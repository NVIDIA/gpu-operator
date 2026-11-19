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

package config

import (
	"fmt"
	"slices"
	"strings"
)

// GPU is what matching reads from NVML for one GPU.
type GPU struct {
	UUID string
	// Name is nvmlDeviceGetName's product name.
	Name string
	// PCIDeviceID is nvmlPciInfo_t.pciDeviceId: device ID in the upper 16
	// bits, vendor ID in the lower 16.
	PCIDeviceID uint32
}

// Assignment is the settings one GPU takes.
type Assignment struct {
	GPU      GPU
	Settings Settings
}

// Resolve matches every GPU before any setting is written, so an overlap
// fails the apply before its first write. A GPU takes its settings from the
// one entry it matches, else from default, else none.
func (d *Document) Resolve(gpus []GPU) ([]Assignment, error) {
	out := make([]Assignment, 0, len(gpus))
	var overlaps []string
	for _, gpu := range gpus {
		var matched []string
		for name, e := range d.GPUs {
			if e.matches(gpu) {
				matched = append(matched, name)
			}
		}
		switch len(matched) {
		case 0:
			a := Assignment{GPU: gpu}
			if d.Default != nil {
				a.Settings = *d.Default
			}
			out = append(out, a)
		case 1:
			out = append(out, Assignment{GPU: gpu, Settings: d.GPUs[matched[0]].Settings})
		default:
			slices.Sort(matched)
			overlaps = append(overlaps, fmt.Sprintf("GPU %s (%s) matches entries %s",
				gpu.UUID, gpu.Name, strings.Join(matched, ", ")))
		}
	}
	if len(overlaps) > 0 {
		return nil, fmt.Errorf("%w: %s", errOverlap, strings.Join(overlaps, "; "))
	}
	return out, nil
}

func (e *Entry) matches(gpu GPU) bool {
	for _, s := range e.DeviceSelectors {
		if !s.matches(gpu) {
			return false
		}
	}
	return true
}

func (s *Selector) matches(gpu GPU) bool {
	if s.DeviceID != nil {
		id := fmt.Sprintf("0x%08X", gpu.PCIDeviceID)
		return slices.ContainsFunc(s.DeviceID, func(item string) bool {
			return strings.EqualFold(item, id)
		})
	}
	name := strings.ToLower(gpu.Name)
	return slices.ContainsFunc(s.ProductName, func(pattern string) bool {
		return globMatch(strings.ToLower(pattern), name)
	})
}

// globMatch reports whether pattern matches all of name, with "*" matching
// any run of characters and every other character matching itself.
// path.Match isn't used because it also treats "?", "[" and "\" as special.
func globMatch(pattern, name string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == name
	}
	if !strings.HasPrefix(name, parts[0]) {
		return false
	}
	name = name[len(parts[0]):]
	last := parts[len(parts)-1]
	for _, p := range parts[1 : len(parts)-1] {
		i := strings.Index(name, p)
		if i < 0 {
			return false
		}
		name = name[i+len(p):]
	}
	return strings.HasSuffix(name, last)
}
