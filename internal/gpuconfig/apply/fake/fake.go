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

// Package fake is an in-memory NVML for tests.
package fake

import (
	"fmt"
	"strings"

	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/apply"
)

// Log records device calls across devices, in order, as
// "<uuid> <Method> <args>".
type Log struct {
	// Calls holds every call, failed ones included.
	Calls []string
	// Writes holds the set calls that succeeded.
	Writes []string
}

// Library is a fake NVML session over a fixed device list.
type Library struct {
	Devices []*Device
	// InitErr, if set, is returned by Init.
	InitErr error
	// DeviceCountErr, if set, is returned by DeviceCount.
	DeviceCountErr error
	// DeviceByIndexErr, if set, is returned by DeviceByIndex.
	DeviceByIndexErr error
	// Initialized is true between Init and Shutdown.
	Initialized bool
}

var _ apply.Library = &Library{}

func (l *Library) Init() error {
	if l.InitErr != nil {
		return l.InitErr
	}
	l.Initialized = true
	return nil
}

func (l *Library) Shutdown() error {
	l.Initialized = false
	return nil
}

func (l *Library) DeviceCount() (int, error) {
	if l.DeviceCountErr != nil {
		return 0, l.DeviceCountErr
	}
	return len(l.Devices), nil
}

func (l *Library) DeviceByIndex(i int) (apply.Device, error) {
	if l.DeviceByIndexErr != nil {
		return nil, l.DeviceByIndexErr
	}
	if i < 0 || i >= len(l.Devices) {
		return nil, fmt.Errorf("no device %d", i)
	}
	return l.Devices[i], nil
}

// Device is a fake GPU. Set calls update its state.
type Device struct {
	Log *Log

	UUIDValue        string
	NameValue        string
	PCIDeviceIDValue uint32

	PowerMinMilliwatts uint32
	PowerMaxMilliwatts uint32
	PowerLimit         uint32

	MemoryClocks []uint32
	// GraphicsClocks maps a memory clock to its supported graphics clocks.
	GraphicsClocks map[uint32][]uint32
	// LockedGPUClocks and LockedMemoryClocks are {min, max}.
	LockedGPUClocks    [2]uint32
	LockedMemoryClocks [2]uint32

	ECCCurrent bool
	ECCPending bool

	Virtualization apply.VirtualizationMode

	// Errs maps a method name, such as "SetPowerLimit", to the error it
	// returns. A failing set call doesn't change the device.
	Errs map[string]error
}

var _ apply.Device = &Device{}

func (d *Device) call(method string, args ...any) error {
	err := d.Errs[method]
	if d.Log == nil {
		return err
	}
	var b strings.Builder
	b.WriteString(d.UUIDValue)
	b.WriteString(" ")
	b.WriteString(method)
	for _, a := range args {
		fmt.Fprintf(&b, " %v", a)
	}
	d.Log.Calls = append(d.Log.Calls, b.String())
	if err == nil && strings.HasPrefix(method, "Set") {
		d.Log.Writes = append(d.Log.Writes, b.String())
	}
	return err
}

func (d *Device) UUID() (string, error) {
	return d.UUIDValue, d.call("UUID")
}

func (d *Device) Name() (string, error) {
	return d.NameValue, d.call("Name")
}

func (d *Device) PCIDeviceID() (uint32, error) {
	return d.PCIDeviceIDValue, d.call("PCIDeviceID")
}

func (d *Device) PowerLimitConstraints() (uint32, uint32, error) {
	if err := d.call("PowerLimitConstraints"); err != nil {
		return 0, 0, err
	}
	return d.PowerMinMilliwatts, d.PowerMaxMilliwatts, nil
}

func (d *Device) SetPowerLimit(milliwatts uint32) error {
	if err := d.call("SetPowerLimit", milliwatts); err != nil {
		return err
	}
	d.PowerLimit = milliwatts
	return nil
}

func (d *Device) SupportedMemoryClocks() ([]uint32, error) {
	if err := d.call("SupportedMemoryClocks"); err != nil {
		return nil, err
	}
	return d.MemoryClocks, nil
}

func (d *Device) SupportedGraphicsClocks(memoryClockMHz uint32) ([]uint32, error) {
	if err := d.call("SupportedGraphicsClocks", memoryClockMHz); err != nil {
		return nil, err
	}
	return d.GraphicsClocks[memoryClockMHz], nil
}

func (d *Device) SetGPULockedClocks(minMHz, maxMHz uint32) error {
	if err := d.call("SetGPULockedClocks", minMHz, maxMHz); err != nil {
		return err
	}
	d.LockedGPUClocks = [2]uint32{minMHz, maxMHz}
	return nil
}

func (d *Device) SetMemoryLockedClocks(minMHz, maxMHz uint32) error {
	if err := d.call("SetMemoryLockedClocks", minMHz, maxMHz); err != nil {
		return err
	}
	d.LockedMemoryClocks = [2]uint32{minMHz, maxMHz}
	return nil
}

func (d *Device) ECCModes() (bool, bool, error) {
	if err := d.call("ECCModes"); err != nil {
		return false, false, err
	}
	return d.ECCCurrent, d.ECCPending, nil
}

func (d *Device) SetECCMode(enabled bool) error {
	if err := d.call("SetECCMode", enabled); err != nil {
		return err
	}
	d.ECCPending = enabled
	return nil
}

func (d *Device) VirtualizationMode() (apply.VirtualizationMode, error) {
	if err := d.call("VirtualizationMode"); err != nil {
		return 0, err
	}
	return d.Virtualization, nil
}
