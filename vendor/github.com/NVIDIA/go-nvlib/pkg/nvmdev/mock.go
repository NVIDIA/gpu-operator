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
	"os"
	"path/filepath"

	"github.com/NVIDIA/go-nvlib/pkg/nvpci"
	"github.com/NVIDIA/go-nvlib/pkg/nvpci/bytes"
)

// MockNvmdev mock mdev device.
type MockNvmdev struct {
	*nvmdev
	pciDevicesRoot string
}

var _ Interface = (*MockNvmdev)(nil)

// NewMock creates new mock mediated (vGPU) and parent PCI devices and removes old devices.
func NewMock() (mock *MockNvmdev, rerr error) {
	mdevParentsRootDir, err := os.MkdirTemp(os.TempDir(), "")
	if err != nil {
		return nil, err
	}
	defer func() {
		if rerr != nil {
			os.RemoveAll(mdevParentsRootDir)
		}
	}()
	mdevDevicesRootDir, err := os.MkdirTemp(os.TempDir(), "")
	if err != nil {
		return nil, err
	}
	defer func() {
		if rerr != nil {
			os.RemoveAll(mdevDevicesRootDir)
		}
	}()

	pciRootDir, err := os.MkdirTemp(os.TempDir(), "")
	if err != nil {
		return nil, err
	}
	defer func() {
		if rerr != nil {
			os.RemoveAll(pciRootDir)
		}
	}()

	nvpciLib := nvpci.New(nvpci.WithPCIDevicesRoot(pciRootDir))
	mock = &MockNvmdev{
		nvmdev: &nvmdev{
			mdevParentsRoot: mdevParentsRootDir,
			mdevDevicesRoot: mdevDevicesRootDir,
			nvpci:           nvpciLib,
		},
		pciDevicesRoot: pciRootDir,
	}

	return mock, nil
}

// Cleanup removes the mocked mediated (vGPU) and parent PCI devices root folders.
func (m *MockNvmdev) Cleanup() {
	os.RemoveAll(m.mdevParentsRoot)
	os.RemoveAll(m.mdevDevicesRoot)
	os.RemoveAll(m.pciDevicesRoot)
}

// AddMockA100Parent creates an A100 like parent GPU mock device.
func (m *MockNvmdev) AddMockA100Parent(address string, numaNode int) error {
	pciDeviceDir := filepath.Join(m.pciDevicesRoot, address)
	err := os.MkdirAll(pciDeviceDir, 0755)
	if err != nil {
		return err
	}

	// /sys/class/mdev_bus/<address> is a symlink to /sys/bus/pci/devices/<address>
	deviceDir := filepath.Join(m.mdevParentsRoot, address)
	err = os.Symlink(pciDeviceDir, deviceDir)
	if err != nil {
		return err
	}

	vendor, err := os.Create(filepath.Join(deviceDir, "vendor"))
	if err != nil {
		return err
	}
	_, err = vendor.WriteString(fmt.Sprintf("0x%x", nvpci.PCINvidiaVendorID))
	if err != nil {
		return err
	}

	class, err := os.Create(filepath.Join(deviceDir, "class"))
	if err != nil {
		return err
	}
	_, err = class.WriteString(fmt.Sprintf("0x%x", nvpci.PCI3dControllerClass))
	if err != nil {
		return err
	}

	device, err := os.Create(filepath.Join(deviceDir, "device"))
	if err != nil {
		return err
	}
	_, err = device.WriteString("0x20bf")
	if err != nil {
		return err
	}

	_, err = os.Create(filepath.Join(deviceDir, "nvidia"))
	if err != nil {
		return err
	}
	err = os.Symlink(filepath.Join(deviceDir, "nvidia"), filepath.Join(deviceDir, "driver"))
	if err != nil {
		return err
	}

	_, err = os.Create(filepath.Join(deviceDir, "20"))
	if err != nil {
		return err
	}
	err = os.Symlink(filepath.Join(deviceDir, "20"), filepath.Join(deviceDir, "iommu_group"))
	if err != nil {
		return err
	}

	numa, err := os.Create(filepath.Join(deviceDir, "numa_node"))
	if err != nil {
		return err
	}
	_, err = numa.WriteString(fmt.Sprintf("%v", numaNode))
	if err != nil {
		return err
	}

	config, err := os.Create(filepath.Join(deviceDir, "config"))
	if err != nil {
		return err
	}
	_data := make([]byte, nvpci.PCICfgSpaceStandardSize)
	data := bytes.New(&_data)
	data.Write16(0, nvpci.PCINvidiaVendorID)
	data.Write16(2, uint16(0x20bf))
	data.Write8(nvpci.PCIStatusBytePosition, nvpci.PCIStatusCapabilityList)
	_, err = config.Write(*data.Raw())
	if err != nil {
		return err
	}

	bar0 := []uint64{0x00000000c2000000, 0x00000000c2ffffff, 0x0000000000040200}
	resource, err := os.Create(filepath.Join(deviceDir, "resource"))
	if err != nil {
		return err
	}
	_, err = resource.WriteString(fmt.Sprintf("0x%x 0x%x 0x%x", bar0[0], bar0[1], bar0[2]))
	if err != nil {
		return err
	}

	pmcID := uint32(0x170000a1)
	resource0, err := os.Create(filepath.Join(deviceDir, "resource0"))
	if err != nil {
		return err
	}
	_data = make([]byte, bar0[1]-bar0[0]+1)
	data = bytes.New(&_data).LittleEndian()
	data.Write32(0, pmcID)
	_, err = resource0.Write(*data.Raw())
	if err != nil {
		return err
	}

	mdevSupportedTypes := []string{"A100-4C", "A100-5C", "A100-8C", "A100-10C",
		"A100-20C", "A100-40C", "A100-1-5CME", "A100-1-5C", "A100-2-10C", "A100-3-20C",
		"A100-4-20C", "A100-7-40C"}
	mdevSupportedTypesDir := filepath.Join(deviceDir, "mdev_supported_types")
	err = os.MkdirAll(mdevSupportedTypesDir, 0755)
	if err != nil {
		return err
	}
	for i, mdevTypeName := range mdevSupportedTypes {
		mdevTypeDir := filepath.Join(mdevSupportedTypesDir, fmt.Sprintf("nvidia-%d", 500+i))
		err := os.MkdirAll(mdevTypeDir, 0755)
		if err != nil {
			return err
		}
		name, err := os.Create(filepath.Join(mdevTypeDir, "name"))
		if err != nil {
			return err
		}
		_, err = name.WriteString(fmt.Sprintf("NVIDIA %s", mdevTypeName))
		if err != nil {
			return err
		}
		availableInstances, err := os.Create(filepath.Join(mdevTypeDir, "available_instances"))
		if err != nil {
			return err
		}
		_, err = availableInstances.WriteString("1")
		if err != nil {
			return err
		}
	}

	return nil
}

// AddMockA100Mdev creates an A100 like MDEV (vGPU) mock device.
// The corresponding mocked parent A100 device must be created beforehand.
func (m *MockNvmdev) AddMockA100Mdev(uuid string, mdevType string, mdevTypeDir string, parentDeviceDir string) error {
	mdevDeviceDir := filepath.Join(parentDeviceDir, uuid)
	err := os.Mkdir(mdevDeviceDir, 0755)
	if err != nil {
		return err
	}

	parentMdevTypeDir := filepath.Join(parentDeviceDir, "mdev_supported_types", mdevTypeDir)
	err = os.Symlink(parentMdevTypeDir, filepath.Join(mdevDeviceDir, "mdev_type"))
	if err != nil {
		return err
	}

	_, err = os.Create(filepath.Join(mdevDeviceDir, "vfio_mdev"))
	if err != nil {
		return err
	}
	err = os.Symlink(filepath.Join(mdevDeviceDir, "vfio_mdev"), filepath.Join(mdevDeviceDir, "driver"))
	if err != nil {
		return err
	}

	_, err = os.Create(filepath.Join(mdevDeviceDir, "200"))
	if err != nil {
		return err
	}
	err = os.Symlink(filepath.Join(mdevDeviceDir, "200"), filepath.Join(mdevDeviceDir, "iommu_group"))
	if err != nil {
		return err
	}

	err = os.Symlink(mdevDeviceDir, filepath.Join(m.mdevDevicesRoot, uuid))
	if err != nil {
		return err
	}

	return nil
}
