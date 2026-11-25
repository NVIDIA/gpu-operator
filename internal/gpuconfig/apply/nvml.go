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

package apply

import (
	"errors"
	"fmt"
)

// An implementation maps NVML return codes to these errors. Any other
// non-success code is returned as another error.
var (
	// ErrNotSupported is NVML_ERROR_NOT_SUPPORTED.
	ErrNotSupported = errors.New("NVML: not supported")
	// ErrInvalidArgument is NVML_ERROR_INVALID_ARGUMENT.
	ErrInvalidArgument = errors.New("NVML: invalid argument")
)

// Library is one NVML session.
type Library interface {
	// Init calls nvmlInitWithFlags(NVML_INIT_FLAG_NO_GPUS).
	Init() error
	Shutdown() error
	DeviceCount() (int, error)
	DeviceByIndex(i int) (Device, error)
}

// Device is the NVML device API the applier uses. Power values are in
// milliwatts and clocks in MHz, as NVML takes them.
type Device interface {
	UUID() (string, error)
	// Name is nvmlDeviceGetName.
	Name() (string, error)
	// PCIDeviceID is nvmlPciInfo_t.pciDeviceId.
	PCIDeviceID() (uint32, error)

	// PowerLimitConstraints is nvmlDeviceGetPowerManagementLimitConstraints.
	PowerLimitConstraints() (minMilliwatts, maxMilliwatts uint32, err error)
	// SetPowerLimit is nvmlDeviceSetPowerManagementLimit.
	SetPowerLimit(milliwatts uint32) error

	// SupportedMemoryClocks is nvmlDeviceGetSupportedMemoryClocks. It returns
	// the whole list; growing the buffer on NVML_ERROR_INSUFFICIENT_SIZE is
	// the implementation's job.
	SupportedMemoryClocks() ([]uint32, error)
	// SupportedGraphicsClocks is nvmlDeviceGetSupportedGraphicsClocks, with
	// the same list contract.
	SupportedGraphicsClocks(memoryClockMHz uint32) ([]uint32, error)
	// SetGPULockedClocks is nvmlDeviceSetGpuLockedClocks.
	SetGPULockedClocks(minMHz, maxMHz uint32) error
	// SetMemoryLockedClocks is nvmlDeviceSetMemoryLockedClocks.
	SetMemoryLockedClocks(minMHz, maxMHz uint32) error

	// ECCModes is nvmlDeviceGetEccMode.
	ECCModes() (current, pending bool, err error)
	// SetECCMode is nvmlDeviceSetEccMode, which sets the pending mode.
	SetECCMode(enabled bool) error

	// VirtualizationMode is nvmlDeviceGetVirtualizationMode.
	VirtualizationMode() (VirtualizationMode, error)
}

// VirtualizationMode is nvmlGpuVirtualizationMode_t.
type VirtualizationMode uint32

const (
	VirtualizationNone        VirtualizationMode = 0
	VirtualizationPassthrough VirtualizationMode = 1
	VirtualizationVGPU        VirtualizationMode = 2
	VirtualizationHostVGPU    VirtualizationMode = 3
	VirtualizationHostVSGA    VirtualizationMode = 4
	// VirtualizationNotReported stands for a GPU whose mode NVML can't
	// report. It isn't an NVML value.
	VirtualizationNotReported VirtualizationMode = 0xFFFFFFFF
)

func (m VirtualizationMode) String() string {
	switch m {
	case VirtualizationNone:
		return "none"
	case VirtualizationPassthrough:
		return "passthrough"
	case VirtualizationVGPU:
		return "vGPU guest"
	case VirtualizationHostVGPU:
		return "vGPU host"
	case VirtualizationHostVSGA:
		return "vSGA host"
	case VirtualizationNotReported:
		return "mode NVML can't report"
	default:
		return fmt.Sprintf("unknown (%d)", uint32(m))
	}
}

// UnsupportedMode reports whether d runs in a mode the applier doesn't write
// settings in: anything other than bare metal or a passthrough guest.
func UnsupportedMode(d Device) (VirtualizationMode, bool, error) {
	m, err := d.VirtualizationMode()
	if errors.Is(err, ErrNotSupported) {
		return VirtualizationNotReported, true, nil
	}
	if err != nil {
		return 0, false, err
	}
	return m, m != VirtualizationNone && m != VirtualizationPassthrough, nil
}
