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

package nvmllib

import (
	"testing"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/NVIDIA/go-nvml/pkg/nvml/mock"
	"github.com/stretchr/testify/require"

	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/apply"
)

func TestToErr(t *testing.T) {
	cases := map[string]struct {
		ret     nvml.Return
		wantIs  error
		wantMsg string
	}{
		"success":          {ret: nvml.SUCCESS},
		"not supported":    {ret: nvml.ERROR_NOT_SUPPORTED, wantIs: apply.ErrNotSupported},
		"invalid argument": {ret: nvml.ERROR_INVALID_ARGUMENT, wantIs: apply.ErrInvalidArgument},
		"other error":      {ret: nvml.ERROR_UNKNOWN, wantMsg: nvml.ERROR_UNKNOWN.Error()},
	}
	sentinels := []error{apply.ErrNotSupported, apply.ErrInvalidArgument}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := toErr(tc.ret)
			if tc.ret == nvml.SUCCESS {
				require.NoError(t, err)
				return
			}
			for _, s := range sentinels {
				if s == tc.wantIs {
					require.ErrorIs(t, err, s)
				} else {
					require.NotErrorIs(t, err, s)
				}
			}
			require.ErrorContains(t, err, tc.wantMsg)
		})
	}
}

func TestLibrary(t *testing.T) {
	var initFlags uint32
	dev := &mock.Device{GetUUIDFunc: func() (string, nvml.Return) { return "GPU-0", nvml.SUCCESS }}
	lib := newFromInterface(&mock.Interface{
		InitWithFlagsFunc: func(flags uint32) nvml.Return {
			initFlags = flags
			return nvml.SUCCESS
		},
		ShutdownFunc:       func() nvml.Return { return nvml.SUCCESS },
		DeviceGetCountFunc: func() (int, nvml.Return) { return 1, nvml.SUCCESS },
		DeviceGetHandleByIndexFunc: func(i int) (nvml.Device, nvml.Return) {
			if i != 0 {
				return nil, nvml.ERROR_INVALID_ARGUMENT
			}
			return dev, nvml.SUCCESS
		},
	})
	require.NoError(t, lib.Init())
	require.Equal(t, uint32(nvml.INIT_FLAG_NO_GPUS), initFlags)
	n, err := lib.DeviceCount()
	require.NoError(t, err)
	require.Equal(t, 1, n)
	d, err := lib.DeviceByIndex(0)
	require.NoError(t, err)
	uuid, err := d.UUID()
	require.NoError(t, err)
	require.Equal(t, "GPU-0", uuid)
	_, err = lib.DeviceByIndex(1)
	require.ErrorIs(t, err, apply.ErrInvalidArgument)
	require.NoError(t, lib.Shutdown())
}

func TestDevice(t *testing.T) {
	var setECC []nvml.EnableState
	var gpuClocks, memClocks [2]uint32
	var powerLimit uint32
	d := &device{d: &mock.Device{
		GetUUIDFunc: func() (string, nvml.Return) { return "GPU-1", nvml.SUCCESS },
		GetNameFunc: func() (string, nvml.Return) { return "NVIDIA H100", nvml.SUCCESS },
		GetPciInfoFunc: func() (nvml.PciInfo, nvml.Return) {
			return nvml.PciInfo{PciDeviceId: 0x233010DE}, nvml.SUCCESS
		},
		GetPowerManagementLimitConstraintsFunc: func() (uint32, uint32, nvml.Return) { return 200000, 700000, nvml.SUCCESS },
		SetPowerManagementLimitFunc:            func(mw uint32) nvml.Return { powerLimit = mw; return nvml.SUCCESS },
		SetGpuLockedClocksFunc: func(lo, hi uint32) nvml.Return {
			gpuClocks = [2]uint32{lo, hi}
			return nvml.SUCCESS
		},
		GetSupportedMemoryClocksFunc: func() ([]uint32, nvml.Return) {
			return []uint32{2619, 1593}, nvml.SUCCESS
		},
		GetSupportedGraphicsClocksFunc: func(mem int) ([]uint32, nvml.Return) {
			if mem != 2619 {
				return nil, nvml.ERROR_NOT_SUPPORTED
			}
			return []uint32{1980, 1590}, nvml.SUCCESS
		},
		SetMemoryLockedClocksFunc: func(lo, hi uint32) nvml.Return {
			memClocks = [2]uint32{lo, hi}
			return nvml.SUCCESS
		},
		GetEccModeFunc: func() (nvml.EnableState, nvml.EnableState, nvml.Return) {
			return nvml.FEATURE_DISABLED, nvml.FEATURE_ENABLED, nvml.SUCCESS
		},
		SetEccModeFunc: func(s nvml.EnableState) nvml.Return {
			setECC = append(setECC, s)
			return nvml.SUCCESS
		},
		GetVirtualizationModeFunc: func() (nvml.GpuVirtualizationMode, nvml.Return) {
			return nvml.GPU_VIRTUALIZATION_MODE_VGPU, nvml.SUCCESS
		},
	}}

	uuid, err := d.UUID()
	require.NoError(t, err)
	require.Equal(t, "GPU-1", uuid)
	name, err := d.Name()
	require.NoError(t, err)
	require.Equal(t, "NVIDIA H100", name)
	id, err := d.PCIDeviceID()
	require.NoError(t, err)
	require.Equal(t, uint32(0x233010DE), id)

	lo, hi, err := d.PowerLimitConstraints()
	require.NoError(t, err)
	require.Equal(t, []uint32{200000, 700000}, []uint32{lo, hi})
	require.NoError(t, d.SetPowerLimit(500000))
	require.Equal(t, uint32(500000), powerLimit)
	require.NoError(t, d.SetGPULockedClocks(1590, 1980))
	require.Equal(t, [2]uint32{1590, 1980}, gpuClocks)
	require.NoError(t, d.SetMemoryLockedClocks(2619, 2619))
	require.Equal(t, [2]uint32{2619, 2619}, memClocks)

	mem, err := d.SupportedMemoryClocks()
	require.NoError(t, err)
	require.Equal(t, []uint32{2619, 1593}, mem)
	gr, err := d.SupportedGraphicsClocks(2619)
	require.NoError(t, err)
	require.Equal(t, []uint32{1980, 1590}, gr)
	_, err = d.SupportedGraphicsClocks(1593)
	require.ErrorIs(t, err, apply.ErrNotSupported)

	current, pending, err := d.ECCModes()
	require.NoError(t, err)
	require.False(t, current)
	require.True(t, pending)
	require.NoError(t, d.SetECCMode(true))
	require.NoError(t, d.SetECCMode(false))
	require.Equal(t, []nvml.EnableState{nvml.FEATURE_ENABLED, nvml.FEATURE_DISABLED}, setECC)

	mode, err := d.VirtualizationMode()
	require.NoError(t, err)
	require.Equal(t, apply.VirtualizationVGPU, mode)
}
