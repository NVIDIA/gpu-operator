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

// Package apply writes resolved GPU settings through NVML: range checks, the
// ECC step and the setters.
package apply

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/config"
)

// Every error Apply returns wraps one of these.
var (
	// ErrRange marks a value outside the range the GPU reports, or a value
	// NVML rejects at write time with NVML_ERROR_INVALID_ARGUMENT.
	ErrRange = errors.New("range error")
	// ErrNVML marks an NVML read or set error other than not supported.
	ErrNVML = errors.New("NVML error")
)

// Setting names, as the configuration spells them.
const (
	PowerLimit   = "powerLimit"
	GPUClocks    = "gpuClocks"
	MemoryClocks = "memoryClocks"
	ECCMode      = "eccMode"
)

// Target is one GPU and the settings it takes.
type Target struct {
	Device Device
	config.Assignment
}

// Skip is a managed setting that wasn't written because NVML reported it, or
// the query of its range, not supported on that GPU. It isn't an error; the
// caller logs it.
type Skip struct {
	UUID    string
	Setting string
}

// Apply checks every value against its GPU's range before any write, then
// stages ECC modes, then writes the other settings one device at a time. A
// setting whose range can't be queried is skipped, so no unchecked value is
// written. Apply stops at the first error; settings already written stay
// written, and the error lists them.
func Apply(targets []Target) ([]Skip, error) {
	var skips []Skip
	plans := make([]plan, 0, len(targets))
	for _, t := range targets {
		p, err := checkRanges(t)
		if err != nil {
			return skips, err
		}
		for _, setting := range p.skip {
			skips = append(skips, Skip{t.GPU.UUID, setting})
		}
		plans = append(plans, p)
	}

	w := &written{}
	for _, t := range targets {
		if t.Settings.ECCMode == nil {
			continue
		}
		supported, wrote, err := stageECC(t.Device, *t.Settings.ECCMode == config.ECCEnabled)
		if err != nil {
			return skips, w.wrap(nvmlError(t, ECCMode, err))
		}
		if !supported {
			skips = append(skips, Skip{t.GPU.UUID, ECCMode})
			continue
		}
		if wrote {
			w.add(t.GPU.UUID, ECCMode+" staged")
		}
	}

	for _, p := range plans {
		s, err := p.write(w)
		skips = append(skips, s...)
		if err != nil {
			return skips, w.wrap(err)
		}
	}
	return skips, nil
}

// written records the settings Apply has written, per GPU in write order.
type written struct {
	uuids    []string
	settings map[string][]string
}

func (w *written) add(uuid, setting string) {
	if w.settings == nil {
		w.settings = map[string][]string{}
	}
	if _, ok := w.settings[uuid]; !ok {
		w.uuids = append(w.uuids, uuid)
	}
	w.settings[uuid] = append(w.settings[uuid], setting)
}

func (w *written) wrap(err error) error {
	if len(w.uuids) == 0 {
		return err
	}
	parts := make([]string, 0, len(w.uuids))
	for _, uuid := range w.uuids {
		parts = append(parts, uuid+": "+strings.Join(w.settings[uuid], ", "))
	}
	return fmt.Errorf("%w; written before the failure: %s", err, strings.Join(parts, "; "))
}

type plan struct {
	Target
	// skip lists settings whose range query isn't supported; they aren't written.
	skip []string
}

func checkRanges(t Target) (plan, error) {
	p := plan{Target: t}
	s := t.Settings

	if s.PowerLimit != nil {
		lo, hi, err := t.Device.PowerLimitConstraints()
		switch {
		case errors.Is(err, ErrNotSupported):
			p.skip = append(p.skip, PowerLimit)
		case err != nil:
			return p, nvmlError(t, PowerLimit, err)
		case milliwatts(*s.PowerLimit) < lo || milliwatts(*s.PowerLimit) > hi:
			return p, fmt.Errorf("%w: GPU %s: %s %d W (%d mW) is outside %d-%d mW",
				ErrRange, t.GPU.UUID, PowerLimit, *s.PowerLimit, milliwatts(*s.PowerLimit), lo, hi)
		}
	}

	if s.MemoryClocks == nil && s.GPUClocks == nil {
		return p, nil
	}
	// Not supported skips both clock settings, because the graphics-clock
	// query takes a memory clock.
	mem, err := t.Device.SupportedMemoryClocks()
	if errors.Is(err, ErrNotSupported) {
		if s.GPUClocks != nil {
			p.skip = append(p.skip, GPUClocks)
		}
		if s.MemoryClocks != nil {
			p.skip = append(p.skip, MemoryClocks)
		}
		return p, nil
	}
	if err != nil {
		setting := MemoryClocks
		if s.MemoryClocks == nil {
			setting = GPUClocks
		}
		return p, nvmlError(t, setting, err)
	}

	if s.MemoryClocks != nil {
		if err := checkClocks(t, MemoryClocks, *s.MemoryClocks, mem); err != nil {
			return p, err
		}
	}

	if s.GPUClocks != nil {
		clocks, err := graphicsClocks(t.Device, mem)
		switch {
		case errors.Is(err, ErrNotSupported):
			p.skip = append(p.skip, GPUClocks)
		case err != nil:
			return p, nvmlError(t, GPUClocks, err)
		default:
			if err := checkClocks(t, GPUClocks, *s.GPUClocks, clocks); err != nil {
				return p, err
			}
		}
	}
	return p, nil
}

func graphicsClocks(d Device, mem []uint32) ([]uint32, error) {
	var out []uint32
	for _, m := range mem {
		g, err := d.SupportedGraphicsClocks(m)
		if err != nil {
			return nil, err
		}
		out = append(out, g...)
	}
	return out, nil
}

func checkClocks(t Target, setting string, r config.ClockRange, supported []uint32) error {
	if len(supported) == 0 {
		return fmt.Errorf("%w: GPU %s: %s: the GPU reports no supported clocks", ErrRange, t.GPU.UUID, setting)
	}
	lo, hi := slices.Min(supported), slices.Max(supported)
	if r.Min < lo || r.Max > hi {
		return fmt.Errorf("%w: GPU %s: %s %d-%d MHz is outside %d-%d MHz",
			ErrRange, t.GPU.UUID, setting, r.Min, r.Max, lo, hi)
	}
	return nil
}

// stageECC writes the pending ECC mode only where it differs, to avoid an
// InfoROM write on every apply. A GPU that doesn't support ECC counts as
// nothing staged.
func stageECC(d Device, enabled bool) (supported, wrote bool, err error) {
	_, pending, err := d.ECCModes()
	if errors.Is(err, ErrNotSupported) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	if pending == enabled {
		return true, false, nil
	}
	err = d.SetECCMode(enabled)
	if errors.Is(err, ErrNotSupported) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return true, true, nil
}

// write sets power limit and clocks on one device. Unlike ECC, these are
// written unconditionally, because a value read back can reflect what a
// workload did.
func (p plan) write(w *written) ([]Skip, error) {
	var skips []Skip
	s := p.Settings
	set := func(setting string, err error) error {
		switch {
		case err == nil:
			w.add(p.GPU.UUID, setting)
			return nil
		case errors.Is(err, ErrNotSupported):
			skips = append(skips, Skip{p.GPU.UUID, setting})
			return nil
		case errors.Is(err, ErrInvalidArgument):
			return fmt.Errorf("%w: GPU %s: %s rejected by NVML: %w", ErrRange, p.GPU.UUID, setting, err)
		default:
			return nvmlError(p.Target, setting, err)
		}
	}

	if s.PowerLimit != nil && !slices.Contains(p.skip, PowerLimit) {
		if err := set(PowerLimit, p.Device.SetPowerLimit(milliwatts(*s.PowerLimit))); err != nil {
			return skips, err
		}
	}
	if s.GPUClocks != nil && !slices.Contains(p.skip, GPUClocks) {
		if err := set(GPUClocks, p.Device.SetGPULockedClocks(s.GPUClocks.Min, s.GPUClocks.Max)); err != nil {
			return skips, err
		}
	}
	if s.MemoryClocks != nil && !slices.Contains(p.skip, MemoryClocks) {
		if err := set(MemoryClocks, p.Device.SetMemoryLockedClocks(s.MemoryClocks.Min, s.MemoryClocks.Max)); err != nil {
			return skips, err
		}
	}
	return skips, nil
}

// milliwatts converts a configured power limit; config.ParseSource
// guarantees the product fits a uint32.
func milliwatts(watts uint32) uint32 {
	return watts * 1000
}

func nvmlError(t Target, setting string, err error) error {
	return fmt.Errorf("%w: GPU %s: %s: %w", ErrNVML, t.GPU.UUID, setting, err)
}
