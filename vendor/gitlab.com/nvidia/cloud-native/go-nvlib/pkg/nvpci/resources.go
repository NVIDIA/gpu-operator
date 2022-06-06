/*
 * Copyright (c) 2021, NVIDIA CORPORATION.  All rights reserved.
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

package nvpci

import (
	"fmt"

	"gitlab.com/nvidia/cloud-native/go-nvlib/pkg/nvpci/mmio"
)

const (
	pmcEndianRegister = 0x4
	pmcLittleEndian   = 0x0
	pmcBigEndian      = 0x01000001
)

// MemoryResource represents a mmio region
type MemoryResource struct {
	Start uintptr
	End   uintptr
	Flags uint64
	Path  string
}

// OpenRW read write mmio region
func (mr *MemoryResource) OpenRW() (mmio.Mmio, error) {
	rw, err := mmio.OpenRW(mr.Path, 0, int(mr.End-mr.Start+1))
	if err != nil {
		return nil, fmt.Errorf("failed to open file for mmio: %v", err)
	}
	switch rw.Read32(pmcEndianRegister) {
	case pmcBigEndian:
		return rw.BigEndian(), nil
	case pmcLittleEndian:
		return rw.LittleEndian(), nil
	}
	return nil, fmt.Errorf("unknown endianness for mmio: %v", err)
}

// OpenRO read only mmio region
func (mr *MemoryResource) OpenRO() (mmio.Mmio, error) {
	ro, err := mmio.OpenRO(mr.Path, 0, int(mr.End-mr.Start+1))
	if err != nil {
		return nil, fmt.Errorf("failed to open file for mmio: %v", err)
	}
	switch ro.Read32(pmcEndianRegister) {
	case pmcBigEndian:
		return ro.BigEndian(), nil
	case pmcLittleEndian:
		return ro.LittleEndian(), nil
	}
	return nil, fmt.Errorf("unknown endianness for mmio: %v", err)
}
