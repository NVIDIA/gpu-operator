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

package k8s

import (
	"context"
	"fmt"
	"strings"

	"sigs.k8s.io/yaml"
)

const defaultMIGPartedConfigMapName = "default-mig-parted-config"

type migPartedConfig struct {
	MIGConfigs map[string][]migPartedEntry `json:"mig-configs"`
}

type migPartedEntry struct {
	DeviceFilter interface{}    `json:"device-filter"`
	MIGEnabled   bool           `json:"mig-enabled"`
	MIGDevices   map[string]int `json:"mig-devices"`
}

// SelectSingleMIGProfile reads the mig-parted ConfigMap and finds the correct
// single-strategy profile for the given PCI device ID. It uses the "all-balanced"
// profile as a lookup table: each entry has device-filters for a GPU family and
// lists the valid MIG slice types. The 1g slice type is extracted and returned
// as a profile name (e.g. "all-1g.10gb").
func (c *Clients) SelectSingleMIGProfile(ctx context.Context, namespace, pciDeviceID string) (string, error) {
	configMapName := c.getMIGPartedConfigMapName(ctx)

	config, err := c.parseMIGPartedConfigMap(ctx, namespace, configMapName)
	if err != nil {
		return "", err
	}

	deviceID := normalizeHexID(pciDeviceID)

	entries, ok := config.MIGConfigs["all-balanced"]
	if !ok {
		return "", fmt.Errorf("configmap has no all-balanced profile")
	}

	for _, entry := range entries {
		if !entry.MIGEnabled || !filterMatchesDevice(entry.DeviceFilter, deviceID) {
			continue
		}
		for sliceType := range entry.MIGDevices {
			if strings.HasPrefix(sliceType, "1g.") {
				return "all-" + sliceType, nil
			}
		}
		return "", fmt.Errorf("all-balanced entry has no 1g slice type")
	}

	return "", fmt.Errorf("no all-balanced entry matches device ID %s", pciDeviceID)
}

// SelectSingleMIGProfileFromNode reads the auto-generated per-node MIG ConfigMap
// ({nodeName}-mig-config) and selects an "all-1g.*" profile that produces more
// than one MIG instance. Single strategy verification requires gpu count > 1,
// so profiles yielding only 1 slice (e.g. memory-extended variants) are skipped.
// When multiple profiles qualify, the first one found is returned (map iteration order).
func (c *Clients) SelectSingleMIGProfileFromNode(ctx context.Context, namespace, nodeName string) (string, error) {
	configMapName := nodeName + "-mig-config"

	config, err := c.parseMIGPartedConfigMap(ctx, namespace, configMapName)
	if err != nil {
		return "", err
	}

	for profileName, entries := range config.MIGConfigs {
		if strings.HasPrefix(profileName, "all-1g.") && migDeviceCount(entries) > 1 {
			return profileName, nil
		}
	}

	return "", fmt.Errorf("configmap %s has no all-1g.* profile with more than 1 instance", configMapName)
}


func migDeviceCount(entries []migPartedEntry) int {
	var total int
	for _, entry := range entries {
		for _, count := range entry.MIGDevices {
			total += count
		}
	}
	return total
}

// parseMIGPartedConfigMap fetches a mig-parted ConfigMap by name and parses its config.yaml key.
func (c *Clients) parseMIGPartedConfigMap(ctx context.Context, namespace, configMapName string) (*migPartedConfig, error) {
	cm, err := c.GetConfigMap(ctx, namespace, configMapName)
	if err != nil {
		return nil, fmt.Errorf("getting configmap %s: %w", configMapName, err)
	}

	configData, ok := cm.Data["config.yaml"]
	if !ok {
		return nil, fmt.Errorf("configmap %s has no config.yaml key", configMapName)
	}

	var config migPartedConfig
	if err := yaml.Unmarshal([]byte(configData), &config); err != nil {
		return nil, fmt.Errorf("parsing config.yaml: %w", err)
	}

	return &config, nil
}

// getMIGPartedConfigMapName reads the ClusterPolicy to find the mig-parted
// ConfigMap name. Falls back to the default if not configured.
func (c *Clients) getMIGPartedConfigMapName(ctx context.Context) string {
	cp, err := c.GetClusterPolicy(ctx)
	if err != nil {
		return defaultMIGPartedConfigMapName
	}
	if cp.Spec.MIGManager.Config != nil && cp.Spec.MIGManager.Config.Name != "" {
		return cp.Spec.MIGManager.Config.Name
	}
	return defaultMIGPartedConfigMapName
}

// normalizeHexID strips the "0x" prefix and uppercases a PCI device ID.
func normalizeHexID(id string) string {
	return strings.ToUpper(strings.TrimPrefix(id, "0x"))
}

// filterMatchesDevice checks whether a device ID matches an entry's device-filter.
// A nil/omitted device-filter matches every device.
func filterMatchesDevice(filter interface{}, deviceID string) bool {
	if filter == nil {
		return true
	}
	for _, f := range parseDeviceFilter(filter) {
		if normalizeHexID(f) == deviceID {
			return true
		}
	}
	return false
}

// parseDeviceFilter handles both string and []string YAML forms.
func parseDeviceFilter(v interface{}) []string {
	switch val := v.(type) {
	case string:
		return []string{val}
	case []interface{}:
		out := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
