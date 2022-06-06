/*
 * Copyright (c) 2022, NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package nvmdev

import (
	"fmt"
	"gitlab.com/nvidia/cloud-native/go-nvlib/pkg/nvpci"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	mdevParentsRoot = "/sys/class/mdev_bus"
	mdevDevicesRoot = "/sys/bus/mdev/devices"
)

// Interface allows us to get a list of NVIDIA MDEV (vGPU) and parent devices
type Interface interface {
	GetAllDevices() ([]*Device, error)
	GetAllParentDevices() ([]*ParentDevice, error)
}

type nvmdev struct {
	mdevParentsRoot string
	mdevDevicesRoot string
}

var _ Interface = (*nvmdev)(nil)

// ParentDevice represents an NVIDIA parent PCI device
type ParentDevice struct {
	*nvpci.NvidiaPCIDevice
	mdevPaths map[string]string
}

// Device represents an NVIDIA MDEV (vGPU) device
type Device struct {
	Path     string
	UUID     string
	MDEVType string
	Parent   *ParentDevice
}

// New interface that allows us to get a list of all NVIDIA parent and MDEV (vGPU) devices
func New() Interface {
	return &nvmdev{mdevParentsRoot, mdevDevicesRoot}
}

// GetAllParentDevices returns all NVIDIA Parent PCI devices on the system
func (m *nvmdev) GetAllParentDevices() ([]*ParentDevice, error) {
	deviceDirs, err := ioutil.ReadDir(m.mdevParentsRoot)
	if err != nil {
		return nil, fmt.Errorf("unable to read PCI bus devices: %v", err)
	}

	var nvdevices []*ParentDevice
	for _, deviceDir := range deviceDirs {
		devicePath := path.Join(m.mdevParentsRoot, deviceDir.Name())
		nvdevice, err := NewParentDevice(devicePath)
		if err != nil {
			return nil, fmt.Errorf("error constructing NVIDIA parent device: %v", err)
		}
		if nvdevice == nil {
			continue
		}
		nvdevices = append(nvdevices, nvdevice)
	}

	addressToID := func(address string) uint64 {
		address = strings.ReplaceAll(address, ":", "")
		address = strings.ReplaceAll(address, ".", "")
		id, _ := strconv.ParseUint(address, 16, 64)
		return id
	}

	sort.Slice(nvdevices, func(i, j int) bool {
		return addressToID(nvdevices[i].Address) < addressToID(nvdevices[j].Address)
	})

	return nvdevices, nil
}

// GetAllDevices returns all NVIDIA mdev (vGPU) devices on the system
func (m *nvmdev) GetAllDevices() ([]*Device, error) {
	deviceDirs, err := ioutil.ReadDir(m.mdevDevicesRoot)
	if err != nil {
		return nil, fmt.Errorf("unable to read MDEV devices directory: %v", err)
	}

	var nvdevices []*Device
	for _, deviceDir := range deviceDirs {
		nvdevice, err := NewDevice(m.mdevDevicesRoot, deviceDir.Name())
		if err != nil {
			return nil, fmt.Errorf("error constructing MDEV device: %v", err)
		}
		if nvdevice == nil {
			continue
		}
		nvdevices = append(nvdevices, nvdevice)
	}

	return nvdevices, nil
}

// NewDevice constructs a Device, which represents an NVIDIA mdev (vGPU) device
func NewDevice(root string, uuid string) (*Device, error) {
	path := path.Join(root, uuid)

	m, err := newMdev(path)
	if err != nil {
		return nil, err
	}

	parent, err := NewParentDevice(m.parentDevicePath())
	if err != nil {
		return nil, fmt.Errorf("error constructing NVIDIA PCI device: %v", err)
	}

	if parent == nil {
		return nil, nil
	}

	mdevType, err := m.Type()
	if err != nil {
		return nil, fmt.Errorf("error getting mdev type: %v", err)
	}

	device := Device{
		Path:     path,
		UUID:     uuid,
		MDEVType: mdevType,
		Parent:   parent,
	}

	return &device, nil
}

type mdev string

func newMdev(devicePath string) (mdev, error) {
	mdevTypeDir, err := filepath.EvalSymlinks(path.Join(devicePath, "mdev_type"))
	if err != nil {
		return "", fmt.Errorf("error resolving mdev_type link: %v", err)
	}

	return mdev(mdevTypeDir), nil
}

func (m mdev) String() string {
	return string(m)
}
func (m mdev) parentDevicePath() string {
	// /sys/bus/pci/devices/<addr>/mdev_supported_types/<mdev_type>
	return path.Dir(path.Dir(string(m)))
}

func (m mdev) Type() (string, error) {
	mdevType, err := ioutil.ReadFile(path.Join(string(m), "name"))
	if err != nil {
		return "", fmt.Errorf("unable to read mdev_type name for mdev %s: %v", m, err)
	}
	// file in the format: [NVIDIA|GRID] <vGPU type>
	mdevTypeStr := strings.TrimSpace(string(mdevType))
	mdevTypeSplit := strings.SplitN(mdevTypeStr, " ", 2)
	if len(mdevTypeSplit) != 2 {
		return "", fmt.Errorf("unable to parse mdev_type name %s for mdev %s", mdevTypeStr, m)
	}

	return mdevTypeSplit[1], nil
}

// NewParentDevice constructs a ParentDevice
func NewParentDevice(devicePath string) (*ParentDevice, error) {
	nvdevice, err := nvpci.NewDevice(devicePath)
	if err != nil {
		return nil, fmt.Errorf("failed to construct NVIDIA PCI device: %v", err)
	}
	if nvdevice == nil {
		// not a NVIDIA device
		return nil, err
	}

	paths, err := filepath.Glob(fmt.Sprintf("%s/mdev_supported_types/nvidia-*/name", nvdevice.Path))
	if err != nil {
		return nil, fmt.Errorf("unable to get files in mdev_supported_types directory: %v", err)
	}
	mdevTypesMap := make(map[string]string)
	for _, path := range paths {
		name, err := ioutil.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("unable to read file %s: %v", path, err)
		}
		// file in the format: [NVIDIA|GRID] <vGPU type>
		nameStr := strings.TrimSpace(string(name))
		nameSplit := strings.SplitN(nameStr, " ", 2)
		if len(nameSplit) != 2 {
			return nil, fmt.Errorf("unable to parse mdev_type name %s at path %s", nameStr, path)
		}
		nameStr = nameSplit[len(nameSplit)-1]

		mdevTypesMap[nameStr] = filepath.Dir(path)
	}

	return &ParentDevice{nvdevice, mdevTypesMap}, err
}

// CreateMDEVDevice creates a mediated device (vGPU) on the parent GPU
func (p *ParentDevice) CreateMDEVDevice(mdevType string, id string) error {
	mdevPath, ok := p.mdevPaths[mdevType]
	if !ok {
		return fmt.Errorf("unable to create mdev %s: mdev not supported by parent device %s", mdevType, p.Address)
	}
	f, err := os.OpenFile(filepath.Join(mdevPath, "create"), os.O_WRONLY|os.O_SYNC, 0200)
	if err != nil {
		return fmt.Errorf("unable to open create file: %v", err)
	}
	_, err = f.WriteString(id)
	if err != nil {
		return fmt.Errorf("unable to create mdev: %v", err)
	}
	return nil
}

// DeleteMDEVDevice deletes a mediated device (vGPU)
func (p *ParentDevice) DeleteMDEVDevice(id string) error {
	removeFile, err := os.OpenFile(filepath.Join(p.Path, id, "remove"), os.O_WRONLY|os.O_SYNC, 0200)
	if err != nil {
		return fmt.Errorf("unable to open remove file: %v", err)
	}
	_, err = removeFile.WriteString("1")
	if err != nil {
		return fmt.Errorf("unable to delete mdev: %v", err)
	}

	return nil
}

// Delete deletes a mediated device (vGPU)
func (m *Device) Delete() error {
	removeFile, err := os.OpenFile(filepath.Join(m.Path, "remove"), os.O_WRONLY|os.O_SYNC, 0200)
	if err != nil {
		return fmt.Errorf("unable to open remove file: %v", err)
	}
	_, err = removeFile.WriteString("1")
	if err != nil {
		return fmt.Errorf("unable to delete mdev: %v", err)
	}

	return nil
}

// IsMDEVTypeSupported checks if the mdevType is supported by the GPU
func (p *ParentDevice) IsMDEVTypeSupported(mdevType string) bool {
	_, found := p.mdevPaths[mdevType]
	return found
}

// IsMDEVTypeAvailable checks if a vGPU instance of mdevType can be created on the parent GPU
func (p *ParentDevice) IsMDEVTypeAvailable(mdevType string) (bool, error) {
	availableInstances, err := p.GetAvailableMDEVInstances(mdevType)
	if err != nil {
		return false, fmt.Errorf("failed to get available instances for mdev type %s: %v", mdevType, err)
	}

	return (availableInstances > 0), nil
}

// GetAvailableMDEVInstances returns the available instances for mdevType.
// Return -1 if mdevType is not supported for the device.
func (p *ParentDevice) GetAvailableMDEVInstances(mdevType string) (int, error) {
	mdevPath, ok := p.mdevPaths[mdevType]
	if !ok {
		return -1, nil
	}

	available, err := ioutil.ReadFile(filepath.Join(mdevPath, "available_instances"))
	if err != nil {
		return -1, fmt.Errorf("unable to read available_instances file: %v", err)
	}

	availableInstances, err := strconv.Atoi(strings.TrimSpace(string(available)))
	if err != nil {
		return -1, fmt.Errorf("unable to convert available_instances to an int: %v", err)
	}

	return availableInstances, nil
}
