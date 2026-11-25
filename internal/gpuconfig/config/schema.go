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
	"errors"
	"fmt"
	"maps"
	"math"
	"regexp"
	"slices"
)

// Document is one parsed configuration document.
type Document struct {
	// Version is the document's version string, such as "v1".
	Version string `json:"version"`
	// GPUs maps entry names to entries. It is empty when gpus is absent,
	// null or {}.
	GPUs map[string]Entry `json:"gpus"`
	// Default holds the settings for GPUs that match no entry. It is nil
	// when default is absent or null.
	Default *Settings `json:"default"`
}

// Entry selects GPUs and lists their settings.
type Entry struct {
	// DeviceSelectors match a GPU when every selector matches it.
	DeviceSelectors []Selector `json:"deviceSelectors"`
	Settings        Settings   `json:"settings"`
}

// Selector sets exactly one field. A GPU matches it when the GPU matches any
// item of that field.
type Selector struct {
	// DeviceID items are "0x" and 8 hex digits: device ID, then vendor ID.
	DeviceID []string `json:"deviceID"`
	// ProductName items are globs whose only wildcard is "*".
	ProductName []string `json:"productName"`
}

// Settings lists the managed settings. A nil field is unmanaged.
type Settings struct {
	// PowerLimit is in watts.
	PowerLimit   *uint32     `json:"powerLimit"`
	GPUClocks    *ClockRange `json:"gpuClocks"`
	MemoryClocks *ClockRange `json:"memoryClocks"`
	ECCMode      *ECCMode    `json:"eccMode"`
}

// ClockRange is a locked clock range in MHz. A missing bound is 0.
type ClockRange struct {
	Min uint32 `json:"min"`
	Max uint32 `json:"max"`
}

// ECCMode is the pending ECC mode to set.
type ECCMode string

const (
	ECCEnabled  ECCMode = "enabled"
	ECCDisabled ECCMode = "disabled"
)

var deviceIDPattern = regexp.MustCompile(`(?i)^0x[0-9a-f]{8}$`)

func (d *Document) validate() error {
	if want := fmt.Sprintf("v%d", MaxVersion); d.Version != want {
		return fmt.Errorf("version %q isn't supported, want %s", d.Version, want)
	}
	for _, name := range slices.Sorted(maps.Keys(d.GPUs)) {
		e := d.GPUs[name]
		if err := e.validate(); err != nil {
			return fmt.Errorf("gpus: %s: %w", name, err)
		}
	}
	if d.Default != nil {
		if err := d.Default.validate(); err != nil {
			return fmt.Errorf("default: %w", err)
		}
	}
	return nil
}

func (e *Entry) validate() error {
	if len(e.DeviceSelectors) == 0 {
		return errors.New("deviceSelectors must not be empty")
	}
	for i, s := range e.DeviceSelectors {
		if err := s.validate(); err != nil {
			return fmt.Errorf("deviceSelectors[%d]: %w", i, err)
		}
	}
	if err := e.Settings.validate(); err != nil {
		return fmt.Errorf("settings: %w", err)
	}
	return nil
}

func (s *Selector) validate() error {
	switch {
	case (s.DeviceID == nil) == (s.ProductName == nil):
		return errors.New("must set exactly one of deviceID and productName")
	case s.DeviceID != nil:
		if len(s.DeviceID) == 0 {
			return errors.New("deviceID must not be empty")
		}
		for i, id := range s.DeviceID {
			if !deviceIDPattern.MatchString(id) {
				return fmt.Errorf(`deviceID[%d]: %q must be "0x" followed by 8 hex digits`, i, id)
			}
		}
	default:
		if len(s.ProductName) == 0 {
			return errors.New("productName must not be empty")
		}
		// A null item decodes as "".
		if slices.Contains(s.ProductName, "") {
			return errors.New("productName items must not be empty")
		}
	}
	return nil
}

func (s *Settings) validate() error {
	// NVML takes the limit in milliwatts as an unsigned int.
	if limit := uint32(math.MaxUint32 / 1000); s.PowerLimit != nil && *s.PowerLimit > limit {
		return fmt.Errorf("powerLimit %d W is above %d W, the largest limit NVML accepts", *s.PowerLimit, limit)
	}
	if err := s.GPUClocks.validate(); err != nil {
		return fmt.Errorf("gpuClocks: %w", err)
	}
	if err := s.MemoryClocks.validate(); err != nil {
		return fmt.Errorf("memoryClocks: %w", err)
	}
	if s.ECCMode != nil && *s.ECCMode != ECCEnabled && *s.ECCMode != ECCDisabled {
		return fmt.Errorf("eccMode must be %q or %q", ECCEnabled, ECCDisabled)
	}
	return nil
}

func (c *ClockRange) validate() error {
	if c != nil && c.Min > c.Max {
		return fmt.Errorf("min %d is greater than max %d", c.Min, c.Max)
	}
	return nil
}
