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

// Package nvmllib implements apply.Library and apply.Device with go-nvml.
package nvmllib

import (
	"errors"

	"github.com/NVIDIA/go-nvml/pkg/nvml"

	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/apply"
)

// Library is an NVML session.
type Library struct {
	lib nvml.Interface
}

var _ apply.Library = &Library{}

// New returns a Library that loads NVML from libraryPath at Init.
func New(libraryPath string) *Library {
	return &Library{lib: nvml.New(nvml.WithLibraryPath(libraryPath))}
}

func newFromInterface(lib nvml.Interface) *Library {
	return &Library{lib: lib}
}

// Init initializes NVML so that it succeeds even when no GPU is found. Callers
// must not call Shutdown after a failed Init: it returns
// NVML_ERROR_UNINITIALIZED, and go-nvml keeps the library loaded anyway.
func (l *Library) Init() error {
	return toErr(l.lib.InitWithFlags(nvml.INIT_FLAG_NO_GPUS))
}

func (l *Library) Shutdown() error {
	return toErr(l.lib.Shutdown())
}

func (l *Library) DeviceCount() (int, error) {
	n, ret := l.lib.DeviceGetCount()
	return n, toErr(ret)
}

func (l *Library) DeviceByIndex(i int) (apply.Device, error) {
	d, ret := l.lib.DeviceGetHandleByIndex(i)
	if err := toErr(ret); err != nil {
		return nil, err
	}
	return &device{d: d}, nil
}

type device struct {
	d nvml.Device
}

var _ apply.Device = &device{}

func (d *device) UUID() (string, error) {
	s, ret := d.d.GetUUID()
	return s, toErr(ret)
}

func (d *device) Name() (string, error) {
	s, ret := d.d.GetName()
	return s, toErr(ret)
}

func (d *device) PCIDeviceID() (uint32, error) {
	info, ret := d.d.GetPciInfo()
	return info.PciDeviceId, toErr(ret)
}

func (d *device) PowerLimitConstraints() (uint32, uint32, error) {
	lo, hi, ret := d.d.GetPowerManagementLimitConstraints()
	return lo, hi, toErr(ret)
}

func (d *device) SetPowerLimit(milliwatts uint32) error {
	return toErr(d.d.SetPowerManagementLimit(milliwatts))
}

func (d *device) SupportedMemoryClocks() ([]uint32, error) {
	clocks, ret := d.d.GetSupportedMemoryClocks()
	return clocks, toErr(ret)
}

func (d *device) SupportedGraphicsClocks(memoryClockMHz uint32) ([]uint32, error) {
	clocks, ret := d.d.GetSupportedGraphicsClocks(int(memoryClockMHz))
	return clocks, toErr(ret)
}

func (d *device) SetGPULockedClocks(minMHz, maxMHz uint32) error {
	return toErr(d.d.SetGpuLockedClocks(minMHz, maxMHz))
}

func (d *device) SetMemoryLockedClocks(minMHz, maxMHz uint32) error {
	return toErr(d.d.SetMemoryLockedClocks(minMHz, maxMHz))
}

func (d *device) ECCModes() (bool, bool, error) {
	current, pending, ret := d.d.GetEccMode()
	return current == nvml.FEATURE_ENABLED, pending == nvml.FEATURE_ENABLED, toErr(ret)
}

func (d *device) SetECCMode(enabled bool) error {
	state := nvml.FEATURE_DISABLED
	if enabled {
		state = nvml.FEATURE_ENABLED
	}
	return toErr(d.d.SetEccMode(state))
}

func (d *device) VirtualizationMode() (apply.VirtualizationMode, error) {
	m, ret := d.d.GetVirtualizationMode()
	return apply.VirtualizationMode(m), toErr(ret)
}

// toErr maps an NVML return code to the errors apply classifies.
func toErr(ret nvml.Return) error {
	switch ret {
	case nvml.SUCCESS:
		return nil
	case nvml.ERROR_NOT_SUPPORTED:
		return apply.ErrNotSupported
	case nvml.ERROR_INVALID_ARGUMENT:
		return apply.ErrInvalidArgument
	default:
		return errors.New("NVML: " + ret.Error())
	}
}
