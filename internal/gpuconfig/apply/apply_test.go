/**
# Copyright (c) NVIDIA CORPORATION.  All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
**/

package apply_test

import (
	"errors"
	"maps"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/apply"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/apply/fake"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/config"
)

var errFault = errors.New("NVML: unknown error")

func newDevice(log *fake.Log, uuid string) *fake.Device {
	return &fake.Device{
		Log:                log,
		UUIDValue:          uuid,
		PowerMinMilliwatts: 200000,
		PowerMaxMilliwatts: 700000,
		MemoryClocks:       []uint32{2619, 1593},
		GraphicsClocks: map[uint32][]uint32{
			2619: {1980, 1590, 345},
			1593: {1755, 210},
		},
		Errs: map[string]error{},
	}
}

func target(d *fake.Device, s config.Settings) apply.Target {
	return apply.Target{Device: d, Assignment: config.Assignment{GPU: config.GPU{UUID: d.UUIDValue}, Settings: s}}
}

func all() config.Settings {
	return config.Settings{
		PowerLimit:   new(uint32(500)),
		GPUClocks:    &config.ClockRange{Min: 1590, Max: 1980},
		MemoryClocks: &config.ClockRange{Min: 2619, Max: 2619},
		ECCMode:      new(config.ECCEnabled),
	}
}

func TestApplyWritesEverySetting(t *testing.T) {
	log := &fake.Log{}
	d := newDevice(log, "GPU-a")
	skips, err := apply.Apply([]apply.Target{target(d, all())})
	require.NoError(t, err)
	require.Empty(t, skips)
	require.Equal(t, uint32(500000), d.PowerLimit)
	require.Equal(t, [2]uint32{1590, 1980}, d.LockedGPUClocks)
	require.Equal(t, [2]uint32{2619, 2619}, d.LockedMemoryClocks)
	require.True(t, d.ECCPending)
}

func TestApplyOrder(t *testing.T) {
	log := &fake.Log{}
	a, b := newDevice(log, "GPU-a"), newDevice(log, "GPU-b")
	_, err := apply.Apply([]apply.Target{target(a, all()), target(b, all())})
	require.NoError(t, err)

	// Every range check runs before the first write.
	firstWrite := -1
	for i, c := range log.Calls {
		if strings.Contains(c, " Set") {
			firstWrite = i
			break
		}
	}
	for _, c := range log.Calls[firstWrite:] {
		require.NotContains(t, c, "Supported")
		require.NotContains(t, c, "Constraints")
	}

	// ECC is staged on every GPU first, then the rest one device at a time.
	require.Equal(t, []string{
		"GPU-a SetECCMode true",
		"GPU-b SetECCMode true",
		"GPU-a SetPowerLimit 500000",
		"GPU-a SetGPULockedClocks 1590 1980",
		"GPU-a SetMemoryLockedClocks 2619 2619",
		"GPU-b SetPowerLimit 500000",
		"GPU-b SetGPULockedClocks 1590 1980",
		"GPU-b SetMemoryLockedClocks 2619 2619",
	}, log.Writes)
}

func TestApplyWritesOnlyManagedSettings(t *testing.T) {
	log := &fake.Log{}
	d := newDevice(log, "GPU-a")
	_, err := apply.Apply([]apply.Target{
		target(d, config.Settings{PowerLimit: new(uint32(300))}),
		target(newDevice(log, "GPU-b"), config.Settings{}),
	})
	require.NoError(t, err)
	require.Equal(t, []string{"GPU-a SetPowerLimit 300000"}, log.Writes)
	require.Equal(t, []string{"GPU-a PowerLimitConstraints", "GPU-a SetPowerLimit 300000"}, log.Calls)
}

func TestApplyErrors(t *testing.T) {
	cases := map[string]struct {
		settings config.Settings
		errs     map[string]error
		setup    func(*fake.Device)
		wantErr  error
		// wantWrites are the writes, on both GPUs, made before the failure.
		wantWrites []string
	}{
		"power limit below range": {
			settings: config.Settings{PowerLimit: new(uint32(199)), ECCMode: new(config.ECCEnabled)},
			wantErr:  apply.ErrRange,
		},
		"power limit above range": {
			settings: config.Settings{PowerLimit: new(uint32(701))},
			wantErr:  apply.ErrRange,
		},
		"power limit constraints read error": {
			settings: config.Settings{PowerLimit: new(uint32(500))},
			errs:     map[string]error{"PowerLimitConstraints": errFault},
			wantErr:  apply.ErrNVML,
		},
		"memory clock outside supported clocks": {
			settings: config.Settings{MemoryClocks: &config.ClockRange{Min: 1500, Max: 2619}},
			wantErr:  apply.ErrRange,
		},
		"memory clocks read error": {
			settings: config.Settings{MemoryClocks: &config.ClockRange{Min: 2619, Max: 2619}},
			errs:     map[string]error{"SupportedMemoryClocks": errFault},
			wantErr:  apply.ErrNVML,
		},
		"GPU clock above every memory clock's range": {
			settings: config.Settings{GPUClocks: &config.ClockRange{Min: 1590, Max: 1981}},
			wantErr:  apply.ErrRange,
		},
		"GPU clock below every memory clock's range": {
			settings: config.Settings{GPUClocks: &config.ClockRange{Min: 209, Max: 1980}},
			wantErr:  apply.ErrRange,
		},
		"graphics clocks read error, such as NOT_FOUND": {
			settings: config.Settings{GPUClocks: &config.ClockRange{Min: 1590, Max: 1980}},
			errs:     map[string]error{"SupportedGraphicsClocks": errFault},
			wantErr:  apply.ErrNVML,
		},
		"no supported clocks reported": {
			settings: config.Settings{MemoryClocks: &config.ClockRange{Min: 2619, Max: 2619}},
			setup:    func(d *fake.Device) { d.MemoryClocks = nil },
			wantErr:  apply.ErrRange,
		},
		"ECC read error": {
			settings: config.Settings{ECCMode: new(config.ECCEnabled)},
			errs:     map[string]error{"ECCModes": errFault},
			wantErr:  apply.ErrNVML,
		},
		"ECC write error": {
			settings: config.Settings{ECCMode: new(config.ECCEnabled)},
			errs:     map[string]error{"SetECCMode": errFault},
			wantErr:  apply.ErrNVML,
		},
		"clock pair rejected at write time": {
			settings:   all(),
			errs:       map[string]error{"SetGPULockedClocks": apply.ErrInvalidArgument},
			wantErr:    apply.ErrRange,
			wantWrites: []string{"GPU-a SetECCMode true", "GPU-b SetECCMode true", "GPU-a SetPowerLimit 500000"},
		},
		"memory clock pair rejected at write time": {
			settings:   config.Settings{MemoryClocks: &config.ClockRange{Min: 2619, Max: 2619}},
			errs:       map[string]error{"SetMemoryLockedClocks": apply.ErrInvalidArgument},
			wantErr:    apply.ErrRange,
			wantWrites: []string{"GPU-b SetECCMode true"},
		},
		"power limit rejected at write time": {
			settings:   config.Settings{PowerLimit: new(uint32(500))},
			errs:       map[string]error{"SetPowerLimit": apply.ErrInvalidArgument},
			wantErr:    apply.ErrRange,
			wantWrites: []string{"GPU-b SetECCMode true"},
		},
		"set error": {
			settings: all(),
			errs:     map[string]error{"SetMemoryLockedClocks": errFault},
			wantErr:  apply.ErrNVML,
			wantWrites: []string{
				"GPU-a SetECCMode true", "GPU-b SetECCMode true",
				"GPU-a SetPowerLimit 500000", "GPU-a SetGPULockedClocks 1590 1980",
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			log := &fake.Log{}
			a := newDevice(log, "GPU-a")
			maps.Copy(a.Errs, tc.errs)
			if tc.setup != nil {
				tc.setup(a)
			}
			b := newDevice(log, "GPU-b")
			_, err := apply.Apply([]apply.Target{target(a, tc.settings), target(b, all())})
			require.ErrorIs(t, err, tc.wantErr)
			require.ErrorContains(t, err, "GPU-a")
			require.Equal(t, tc.wantWrites, log.Writes)
		})
	}
}

func TestApplyRangeErrorOnLaterGPUWritesNothing(t *testing.T) {
	log := &fake.Log{}
	a, b := newDevice(log, "GPU-a"), newDevice(log, "GPU-b")
	_, err := apply.Apply([]apply.Target{
		target(a, all()),
		target(b, config.Settings{PowerLimit: new(uint32(701)), ECCMode: new(config.ECCEnabled)}),
	})
	require.ErrorIs(t, err, apply.ErrRange)
	require.ErrorContains(t, err, "GPU-b")
	require.Empty(t, log.Writes)
}

func TestApplyNotSupported(t *testing.T) {
	cases := map[string]struct {
		settings   config.Settings
		errs       map[string]error
		wantSkips  []apply.Skip
		wantWrites []string
	}{
		"power limit range query not supported skips the setting": {
			settings:   config.Settings{PowerLimit: new(uint32(5000)), GPUClocks: &config.ClockRange{Min: 1590, Max: 1980}},
			errs:       map[string]error{"PowerLimitConstraints": apply.ErrNotSupported},
			wantSkips:  []apply.Skip{{UUID: "GPU-a", Setting: apply.PowerLimit}},
			wantWrites: []string{"GPU-a SetGPULockedClocks 1590 1980"},
		},
		"memory clock range query not supported skips the setting": {
			settings:   config.Settings{PowerLimit: new(uint32(500)), MemoryClocks: &config.ClockRange{Min: 1, Max: 9999}},
			errs:       map[string]error{"SupportedMemoryClocks": apply.ErrNotSupported},
			wantSkips:  []apply.Skip{{UUID: "GPU-a", Setting: apply.MemoryClocks}},
			wantWrites: []string{"GPU-a SetPowerLimit 500000"},
		},
		"memory clock range query not supported skips both clock settings": {
			settings: config.Settings{
				GPUClocks:    &config.ClockRange{Min: 1, Max: 9999},
				MemoryClocks: &config.ClockRange{Min: 1, Max: 9999},
			},
			errs:      map[string]error{"SupportedMemoryClocks": apply.ErrNotSupported},
			wantSkips: []apply.Skip{{UUID: "GPU-a", Setting: apply.GPUClocks}, {UUID: "GPU-a", Setting: apply.MemoryClocks}},
		},
		"graphics clock range query not supported skips the setting": {
			settings: config.Settings{
				GPUClocks:    &config.ClockRange{Min: 1, Max: 9999},
				MemoryClocks: &config.ClockRange{Min: 2619, Max: 2619},
			},
			errs:       map[string]error{"SupportedGraphicsClocks": apply.ErrNotSupported},
			wantSkips:  []apply.Skip{{UUID: "GPU-a", Setting: apply.GPUClocks}},
			wantWrites: []string{"GPU-a SetMemoryLockedClocks 2619 2619"},
		},
		"setter not supported is skipped and the apply continues": {
			settings:   all(),
			errs:       map[string]error{"SetPowerLimit": apply.ErrNotSupported},
			wantSkips:  []apply.Skip{{UUID: "GPU-a", Setting: apply.PowerLimit}},
			wantWrites: []string{"GPU-a SetECCMode true", "GPU-a SetGPULockedClocks 1590 1980", "GPU-a SetMemoryLockedClocks 2619 2619"},
		},
		"ECC read not supported counts as nothing staged": {
			settings:  config.Settings{ECCMode: new(config.ECCEnabled)},
			errs:      map[string]error{"ECCModes": apply.ErrNotSupported},
			wantSkips: []apply.Skip{{UUID: "GPU-a", Setting: apply.ECCMode}},
		},
		"ECC write not supported": {
			settings:  config.Settings{ECCMode: new(config.ECCEnabled)},
			errs:      map[string]error{"SetECCMode": apply.ErrNotSupported},
			wantSkips: []apply.Skip{{UUID: "GPU-a", Setting: apply.ECCMode}},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			log := &fake.Log{}
			d := newDevice(log, "GPU-a")
			d.Errs = tc.errs
			skips, err := apply.Apply([]apply.Target{target(d, tc.settings)})
			require.NoError(t, err)
			require.Equal(t, tc.wantSkips, skips)

			require.Equal(t, tc.wantWrites, log.Writes)
		})
	}
}

// The ECC step writes the pending mode only when it differs from eccMode.
func TestApplyECCStep(t *testing.T) {
	cases := map[string]struct {
		mode             config.ECCMode
		current, pending bool
		wantWrites       []string
	}{
		"enable from disabled, disabled: stage enabled": {
			mode: config.ECCEnabled, wantWrites: []string{"GPU-a SetECCMode true"},
		},
		"enable from disabled, enabled: already staged": {
			mode: config.ECCEnabled, pending: true,
		},
		"enable from enabled, enabled: nothing to do": {
			mode: config.ECCEnabled, current: true, pending: true,
		},
		"enable from enabled, disabled: cancel someone else's staging": {
			mode: config.ECCEnabled, current: true, wantWrites: []string{"GPU-a SetECCMode true"},
		},
		"disable from enabled, enabled: stage disabled": {
			mode: config.ECCDisabled, current: true, pending: true, wantWrites: []string{"GPU-a SetECCMode false"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			log := &fake.Log{}
			d := newDevice(log, "GPU-a")
			d.ECCCurrent, d.ECCPending = tc.current, tc.pending
			_, err := apply.Apply([]apply.Target{target(d, config.Settings{ECCMode: new(tc.mode)})})
			require.NoError(t, err)
			require.Equal(t, tc.mode == config.ECCEnabled, d.ECCPending)
			require.Equal(t, tc.current, d.ECCCurrent)
			require.Equal(t, tc.wantWrites, log.Writes)
		})
	}
}

func TestApplyQueriesMemoryClocksOncePerGPU(t *testing.T) {
	log := &fake.Log{}
	d := newDevice(log, "GPU-a")
	_, err := apply.Apply([]apply.Target{target(d, all())})
	require.NoError(t, err)

	n := 0
	for _, c := range log.Calls {
		if c == "GPU-a SupportedMemoryClocks" {
			n++
		}
	}
	require.Equal(t, 1, n)
}

func TestApplyErrorListsWrittenSettings(t *testing.T) {
	cases := map[string]struct {
		errs     map[string]error
		aPending bool
		wantMsg  string
	}{
		"ECC already pending isn't listed as staged": {
			errs:     map[string]error{"SetMemoryLockedClocks": errFault},
			aPending: true,
			wantMsg:  "written before the failure: GPU-b: eccMode staged, powerLimit, gpuClocks; GPU-a: powerLimit, gpuClocks, memoryClocks",
		},
		"failure on a later GPU names earlier GPUs' writes and ECC staged on all": {
			errs:    map[string]error{"SetMemoryLockedClocks": errFault},
			wantMsg: "written before the failure: GPU-a: eccMode staged, powerLimit, gpuClocks, memoryClocks; GPU-b: eccMode staged, powerLimit, gpuClocks",
		},
		"ECC failure on a later GPU names the ECC staged before it": {
			errs:    map[string]error{"SetECCMode": errFault},
			wantMsg: "written before the failure: GPU-a: eccMode staged",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			log := &fake.Log{}
			a, b := newDevice(log, "GPU-a"), newDevice(log, "GPU-b")
			a.ECCPending = tc.aPending
			b.Errs = tc.errs
			_, err := apply.Apply([]apply.Target{target(a, all()), target(b, all())})
			require.ErrorIs(t, err, apply.ErrNVML)
			require.ErrorIs(t, err, errFault)
			require.ErrorContains(t, err, tc.wantMsg)
		})
	}

	_, err := apply.Apply([]apply.Target{target(newDevice(&fake.Log{}, "GPU-a"), config.Settings{PowerLimit: new(uint32(900))})})
	require.NotContains(t, err.Error(), "written before the failure")
}

func TestApplyPowerRangeErrorStatesBothUnits(t *testing.T) {
	d := newDevice(&fake.Log{}, "GPU-a")
	_, err := apply.Apply([]apply.Target{target(d, config.Settings{PowerLimit: new(uint32(750))})})
	require.ErrorIs(t, err, apply.ErrRange)
	require.ErrorContains(t, err, "powerLimit 750 W (750000 mW) is outside 200000-700000 mW")
}

func TestUnsupportedMode(t *testing.T) {
	cases := map[string]struct {
		mode      apply.VirtualizationMode
		err       error
		wantSkip  bool
		wantErrIs error
	}{
		"bare metal":        {mode: apply.VirtualizationNone},
		"passthrough guest": {mode: apply.VirtualizationPassthrough},
		"vGPU guest":        {mode: apply.VirtualizationVGPU, wantSkip: true},
		"vGPU host":         {mode: apply.VirtualizationHostVGPU, wantSkip: true},
		"vSGA host":         {mode: apply.VirtualizationHostVSGA, wantSkip: true},
		"unknown mode":      {mode: 9, wantSkip: true},
		"mode not reported": {err: apply.ErrNotSupported, wantSkip: true},
		"read error":        {err: errFault, wantErrIs: errFault},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			d := newDevice(&fake.Log{}, "GPU-a")
			d.Virtualization = tc.mode
			if tc.err != nil {
				d.Errs["VirtualizationMode"] = tc.err
			}
			_, skip, err := apply.UnsupportedMode(d)
			if tc.wantErrIs != nil {
				require.ErrorIs(t, err, tc.wantErrIs)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantSkip, skip)
		})
	}
	require.Equal(t, "vGPU guest", apply.VirtualizationVGPU.String())
	require.Equal(t, "unknown (9)", apply.VirtualizationMode(9).String())
}
