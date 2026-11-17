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

// Package statefile implements the node-local GPU configuration state file:
// its encoding, the lock, atomic writes and the predicates read from it. The
// applier, which configures the GPUs, records its decision there; the gate,
// which holds GPU allocators until that decision exists, records its passes.
package statefile

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// stateVersion is the only state file version. parse rejects a file with any
// other version.
const stateVersion = 1

// Decision is the value the applier recorded for a driver load. Only Applied
// and Skipped count as a decision; a missing or unknown value reads as not
// decided.
type Decision string

const (
	Applied Decision = "applied"
	Skipped Decision = "skipped"
)

// ErrParse marks a state file that exists but doesn't parse. Neither side
// writes over such a file.
var ErrParse = errors.New("state file parse error")

// State is the decoded state file. The uint64 and time.Time field types do the
// schema check while decoding; fields have no omitempty because a writer
// writes every field.
type State struct {
	Version uint64    `json:"version"`
	Created time.Time `json:"created"`
	// Apply and Gate are nil when absent or null.
	Apply *Apply `json:"apply"`
	Gate  *Gate  `json:"gate"`
}

// Apply is the applier's object.
type Apply struct {
	Module uint64    `json:"module"`
	Value  Decision  `json:"value"`
	Config string    `json:"config"`
	Time   time.Time `json:"time"`
}

// Gate is the gate's object.
type Gate struct {
	Module uint64    `json:"module"`
	Time   time.Time `json:"time"`
}

// File is one read of the state file: the decoded State, plus the raw
// top-level keys that a write carries through unchanged.
type File struct {
	State
	// raw is nil when the file doesn't exist.
	raw map[string]json.RawMessage
}

// Exists reports whether the state file existed at the read.
func (f *File) Exists() bool {
	return f.raw != nil
}

// parse decodes the contents of an existing state file. Every failure wraps
// ErrParse. Unknown keys are ignored, never an error, so that fields added
// later read cleanly in older binaries.
func parse(data []byte) (*File, error) {
	f := &File{}
	if err := json.Unmarshal(data, &f.State); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrParse, err)
	}
	// version is the only required field, so this also rejects a top-level
	// {} or null, which leave it at 0.
	if f.Version != stateVersion {
		return nil, fmt.Errorf("%w: version %d, want %d", ErrParse, f.Version, stateVersion)
	}
	// raw keeps every top-level key, unknown ones included, so a field a
	// newer version added survives a write by this one.
	if err := json.Unmarshal(data, &f.raw); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrParse, err)
	}
	return f, nil
}

// The predicates below take l, the load ID of the current driver load, which
// is at least 1.

// ApplyAt reports whether the apply object was written for this driver load.
func (s *State) ApplyAt(l uint64) bool {
	return s.Apply != nil && s.Apply.Module == l
}

// Decided reports whether the applier recorded applied or skipped for this
// driver load.
func (s *State) Decided(l uint64) bool {
	return s.ApplyAt(l) && (s.Apply.Value == Applied || s.Apply.Value == Skipped)
}

// AppliedAt reports whether the applier recorded applied for this driver load.
func (s *State) AppliedAt(l uint64) bool {
	return s.ApplyAt(l) && s.Apply.Value == Applied
}

// Undecided reports whether the apply object is for this driver load but holds
// a missing or unknown value.
func (s *State) Undecided(l uint64) bool {
	return s.ApplyAt(l) && !s.Decided(l)
}

// Passed reports whether the gate last passed at this driver load.
func (s *State) Passed(l uint64) bool {
	return s.Gate != nil && s.Gate.Module == l
}

// GatePassedWithoutDecision reports whether the gate last passed with no
// applier decision to rely on; the gate records such a pass at load ID 0.
func (s *State) GatePassedWithoutDecision() bool {
	return s.Gate != nil && s.Gate.Module == 0
}

// GatePassedThisBoot reports whether a gate has passed at any driver load since
// boot, so pods released at an earlier load may still be on the node.
func (s *State) GatePassedThisBoot() bool {
	return s.Gate != nil
}

// ConfigAt returns the stored config hash when the apply object is for this
// driver load, otherwise "".
func (s *State) ConfigAt(l uint64) string {
	if !s.ApplyAt(l) {
		return ""
	}
	return s.Apply.Config
}
