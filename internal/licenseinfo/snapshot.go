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

package licenseinfo

import (
	"encoding/json"
	"fmt"
	"time"
)

const (
	// AnnotationKey is the node annotation that stores serialized vGPU license data.
	AnnotationKey = "nvidia.com/vgpu-license-statuses"
)

// DeviceStatus represents the licensing status for a single GPU/vGPU device.
type DeviceStatus struct {
	ID       string     `json:"id"`
	Product  string     `json:"product,omitempty"`
	Licensed bool       `json:"licensed"`
	Status   string     `json:"status,omitempty"`
	Expiry   *time.Time `json:"expiry,omitempty"`
	Message  string     `json:"message,omitempty"`
}

// Snapshot captures the licensing state observed on a node at a specific time.
type Snapshot struct {
	CollectedAt time.Time      `json:"collectedAt"`
	Source      string         `json:"source,omitempty"`
	Error       string         `json:"error,omitempty"`
	Devices     []DeviceStatus `json:"devices,omitempty"`
}

// NewSnapshot initializes a Snapshot with the provided metadata. CollectedAt is normalized to UTC.
func NewSnapshot(devices []DeviceStatus, source string, collectedAt time.Time) Snapshot {
	return Snapshot{
		Devices:     devices,
		Source:      source,
		CollectedAt: collectedAt.UTC(),
	}
}

// Marshal converts the snapshot into a JSON string for use in annotations.
func (s Snapshot) Marshal() (string, error) {
	out, err := json.Marshal(s)
	if err != nil {
		return "", fmt.Errorf("failed to marshal license snapshot: %w", err)
	}
	return string(out), nil
}

// Parse converts the serialized snapshot back into a Snapshot struct.
func Parse(value string) (Snapshot, error) {
	if value == "" {
		return Snapshot{}, fmt.Errorf("empty license snapshot")
	}
	var snap Snapshot
	if err := json.Unmarshal([]byte(value), &snap); err != nil {
		return Snapshot{}, fmt.Errorf("unable to parse license snapshot: %w", err)
	}
	return snap, nil
}
