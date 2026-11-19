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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Each type decodes itself from sigs.k8s.io/yaml's JSON so that keys match
// exactly, unknown keys fail, and errors describe the YAML the user wrote.

// Document is one parsed configuration document.
type Document struct {
	// Version is the document's version string, such as "v1".
	Version string
	// GPUs maps entry names to entries. It is empty when gpus is absent,
	// null or {}.
	GPUs map[string]Entry
	// Default holds the settings for GPUs that match no entry. It is nil
	// when default is absent or null.
	Default *Settings

	version int
}

// Entry selects GPUs and lists their settings.
type Entry struct {
	// DeviceSelectors match a GPU when every selector matches it.
	DeviceSelectors []Selector
	Settings        Settings
}

// Selector sets exactly one field. A GPU matches it when the GPU matches any
// item of that field.
type Selector struct {
	// DeviceID items are "0x" and 8 hex digits: device ID, then vendor ID.
	DeviceID []string
	// ProductName items are globs whose only wildcard is "*".
	ProductName []string
}

// Settings lists the managed settings. A nil field is unmanaged.
type Settings struct {
	// PowerLimit is in watts.
	PowerLimit   *uint32
	GPUClocks    *ClockRange
	MemoryClocks *ClockRange
	ECCMode      *ECCMode
}

// ClockRange is a locked clock range in MHz.
type ClockRange struct {
	Min uint32
	Max uint32
}

// ECCMode is the pending ECC mode to set.
type ECCMode string

const (
	ECCEnabled  ECCMode = "enabled"
	ECCDisabled ECCMode = "disabled"
)

var (
	versionPattern  = regexp.MustCompile(`^v([1-9][0-9]*)$`)
	deviceIDPattern = regexp.MustCompile(`(?i)^0x[0-9a-f]{8}$`)
)

func (d *Document) UnmarshalJSON(b []byte) error {
	m, err := object(b)
	if err != nil {
		return fmt.Errorf("the document %w", err)
	}
	raw, ok := m["version"]
	if !ok || isNull(raw) {
		return errors.New("missing version")
	}
	if err := decodeString(raw, &d.Version); err != nil {
		return fmt.Errorf(`version: %w such as "v1"`, err)
	}
	if d.version, err = parseVersion(d.Version); err != nil {
		return err
	}
	// A newer schema may have keys this one doesn't know; parse reports the
	// version instead.
	if d.version > MaxVersion {
		return nil
	}
	delete(m, "version")
	if err := onlyKnown(m, "gpus", "default"); err != nil {
		return err
	}

	return eachField(m, func(k string, v json.RawMessage) error {
		switch k {
		case "gpus":
			return d.decodeEntries(v)
		case "default":
			d.Default = &Settings{}
			return d.Default.UnmarshalJSON(v)
		}
		return nil
	})
}

// decodeEntries decodes gpus without dropping null entries, so that an entry
// with no body fails as one without deviceSelectors and settings.
func (d *Document) decodeEntries(b []byte) error {
	m, err := object(b)
	if err != nil {
		return err
	}
	d.GPUs = make(map[string]Entry, len(m))
	return eachField(m, func(name string, v json.RawMessage) error {
		var e Entry
		if err := e.UnmarshalJSON(v); err != nil {
			return err
		}
		d.GPUs[name] = e
		return nil
	})
}

func (e *Entry) UnmarshalJSON(b []byte) error {
	m, err := fields(b, "deviceSelectors", "settings")
	if err != nil {
		return err
	}
	for _, k := range []string{"deviceSelectors", "settings"} {
		if _, ok := m[k]; !ok {
			return fmt.Errorf("missing %s", k)
		}
	}
	return eachField(m, func(k string, v json.RawMessage) error {
		switch k {
		case "deviceSelectors":
			items, err := list(v)
			if err != nil {
				return err
			}
			e.DeviceSelectors = make([]Selector, len(items))
			for i, item := range items {
				if err := e.DeviceSelectors[i].UnmarshalJSON(item); err != nil {
					return fmt.Errorf("[%d]: %w", i, err)
				}
			}
			return nil
		case "settings":
			return e.Settings.UnmarshalJSON(v)
		}
		return nil
	})
}

func (s *Selector) UnmarshalJSON(b []byte) error {
	m, err := fields(b, "deviceID", "productName")
	if err != nil {
		return err
	}
	if len(m) != 1 {
		return fmt.Errorf("must set exactly one of deviceID and productName, found %d", len(m))
	}
	return eachField(m, func(k string, v json.RawMessage) error {
		switch k {
		case "deviceID":
			if err := decodeStrings(v, &s.DeviceID); err != nil {
				return err
			}
			for i, id := range s.DeviceID {
				if !deviceIDPattern.MatchString(id) {
					return fmt.Errorf(`[%d]: %q must be "0x" followed by 8 hex digits`, i, id)
				}
			}
			return nil
		case "productName":
			return decodeStrings(v, &s.ProductName)
		}
		return nil
	})
}

func (s *Settings) UnmarshalJSON(b []byte) error {
	m, err := fields(b, "powerLimit", "gpuClocks", "memoryClocks", "eccMode")
	if err != nil {
		return err
	}
	return eachField(m, func(k string, v json.RawMessage) error {
		switch k {
		case "powerLimit":
			s.PowerLimit = new(uint32)
			if err := decodeUint32(v, s.PowerLimit); err != nil {
				return err
			}
			// NVML takes the limit in milliwatts as an unsigned int.
			if limit := uint32(math.MaxUint32 / 1000); *s.PowerLimit > limit {
				return fmt.Errorf("%d W is above %d W, the largest limit NVML accepts", *s.PowerLimit, limit)
			}
			return nil
		case "gpuClocks":
			s.GPUClocks = &ClockRange{}
			return s.GPUClocks.UnmarshalJSON(v)
		case "memoryClocks":
			s.MemoryClocks = &ClockRange{}
			return s.MemoryClocks.UnmarshalJSON(v)
		case "eccMode":
			var mode string
			if err := decodeString(v, &mode); err != nil || (mode != string(ECCEnabled) && mode != string(ECCDisabled)) {
				return fmt.Errorf("must be %q or %q", ECCEnabled, ECCDisabled)
			}
			s.ECCMode = new(ECCMode(mode))
			return nil
		}
		return nil
	})
}

func (c *ClockRange) UnmarshalJSON(b []byte) error {
	m, err := fields(b, "min", "max")
	if err != nil {
		return err
	}
	for _, k := range []string{"min", "max"} {
		if _, ok := m[k]; !ok {
			return fmt.Errorf("missing %s", k)
		}
	}
	if err := eachField(m, func(k string, v json.RawMessage) error {
		switch k {
		case "min":
			return decodeUint32(v, &c.Min)
		case "max":
			return decodeUint32(v, &c.Max)
		}
		return nil
	}); err != nil {
		return err
	}
	if c.Min > c.Max {
		return fmt.Errorf("min %d is greater than max %d", c.Min, c.Max)
	}
	return nil
}

// object decodes a JSON object, null values included. A null object decodes
// as an empty one.
func object(b []byte) (map[string]json.RawMessage, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, errors.New("must be a map")
	}
	return m, nil
}

// fields decodes a JSON object whose keys are all in known, then drops the
// null values.
func fields(b []byte, known ...string) (map[string]json.RawMessage, error) {
	m, err := object(b)
	if err != nil {
		return nil, err
	}
	return m, onlyKnown(m, known...)
}

// onlyKnown rejects any key not in known, even one whose value is null, then
// drops the null values: the schema reads null as a missing key.
func onlyKnown(m map[string]json.RawMessage, known ...string) error {
	for _, k := range slices.Sorted(maps.Keys(m)) {
		if !slices.Contains(known, k) {
			return fmt.Errorf("%s: unknown key", k)
		}
	}
	maps.DeleteFunc(m, func(_ string, v json.RawMessage) bool { return isNull(v) })
	return nil
}

// eachField calls fn for each key in sorted order, so errors are
// deterministic, and prefixes an error with its key.
func eachField(m map[string]json.RawMessage, fn func(string, json.RawMessage) error) error {
	for _, k := range slices.Sorted(maps.Keys(m)) {
		if err := fn(k, m[k]); err != nil {
			// A list item's error starts with its index, which joins the
			// key directly: deviceSelectors[1].
			if strings.HasPrefix(err.Error(), "[") {
				return fmt.Errorf("%s%w", k, err)
			}
			return fmt.Errorf("%s: %w", k, err)
		}
	}
	return nil
}

// list decodes a non-empty JSON array.
func list(b []byte) ([]json.RawMessage, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(b, &items); err != nil {
		return nil, errors.New("must be a list")
	}
	if len(items) == 0 {
		return nil, errors.New("must not be an empty list")
	}
	return items, nil
}

// decodeStrings decodes a non-empty list of strings.
func decodeStrings(b []byte, out *[]string) error {
	items, err := list(b)
	if err != nil {
		return err
	}
	for i, item := range items {
		var s string
		if err := decodeString(item, &s); err != nil {
			return fmt.Errorf("[%d]: %w", i, err)
		}
		*out = append(*out, s)
	}
	return nil
}

func decodeString(b []byte, out *string) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	s, ok := v.(string)
	if !ok {
		return errors.New("must be a string")
	}
	*out = s
	return nil
}

func decodeUint32(b []byte, out *uint32) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return err
	}
	n, ok := v.(json.Number)
	if !ok {
		return errors.New("must be an integer")
	}
	u, err := strconv.ParseUint(n.String(), 10, 32)
	if err == nil {
		*out = uint32(u)
		return nil
	}
	if f, err := n.Float64(); err == nil && f == math.Trunc(f) {
		return fmt.Errorf("must be an integer from 0 to %d", uint32(math.MaxUint32))
	}
	return errors.New("must be an integer")
}

func isNull(b []byte) bool {
	return bytes.Equal(bytes.TrimSpace(b), []byte("null"))
}

func parseVersion(v string) (int, error) {
	m := versionPattern.FindStringSubmatch(v)
	if m == nil {
		return 0, fmt.Errorf("version %q must be v followed by a positive integer, such as v1", v)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		// Too large for an int, so newer than any version this code knows.
		return math.MaxInt, nil
	}
	return n, nil
}
