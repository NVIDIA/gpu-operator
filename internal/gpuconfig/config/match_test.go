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
	"testing"

	"github.com/stretchr/testify/require"
)

var (
	h100 = GPU{UUID: "GPU-h100", Name: "NVIDIA H100 80GB HBM3", PCIDeviceID: 0x233010DE}
	l40s = GPU{UUID: "GPU-l40s", Name: "NVIDIA L40S", PCIDeviceID: 0x26B910DE}
	a100 = GPU{UUID: "GPU-a100", Name: "NVIDIA A100-SXM4-80GB", PCIDeviceID: 0x20B210DE}
)

func mustParse(t *testing.T, doc string) *Document {
	d, err := parse([]byte(doc))
	require.NoError(t, err)
	return d
}

func TestResolveExample(t *testing.T) {
	got, err := mustParse(t, exampleDoc).Resolve([]GPU{h100, l40s, a100})
	require.NoError(t, err)
	require.Equal(t, []Assignment{
		{GPU: h100, Settings: Settings{
			ECCMode: new(ECCEnabled), PowerLimit: new(uint32(500)), GPUClocks: &ClockRange{Min: 1590, Max: 1980},
		}},
		{GPU: l40s, Settings: Settings{
			ECCMode: new(ECCDisabled), PowerLimit: new(uint32(250)), MemoryClocks: &ClockRange{Min: 9001, Max: 9001},
		}},
		{GPU: a100, Settings: Settings{ECCMode: new(ECCEnabled)}},
	}, got)
}

func TestResolve(t *testing.T) {
	// Each entry's powerLimit tells which entry a GPU took its settings from.
	entry := func(powerLimit, name, selectors string) string {
		return "  " + name + ":\n    deviceSelectors: " + selectors + "\n    settings: {powerLimit: " + powerLimit + "}\n"
	}
	cases := map[string]struct {
		doc            string
		gpu            GPU
		wantPowerLimit *uint32
		wantErr        error
	}{
		"deviceID matches without regard to case": {
			doc:            entry("1", "e", `[{deviceID: ["0x233010de"]}]`),
			gpu:            h100,
			wantPowerLimit: new(uint32(1)),
		},
		"deviceID with an uppercase 0X prefix": {
			doc:            entry("1", "e", `[{deviceID: ["0X233010DE"]}]`),
			gpu:            h100,
			wantPowerLimit: new(uint32(1)),
		},
		"deviceID is OR within the list": {
			doc:            entry("1", "e", `[{deviceID: ["0x233910DE", "0x233010DE"]}]`),
			gpu:            h100,
			wantPowerLimit: new(uint32(1)),
		},
		"deviceID compares vendor too": {
			doc: entry("1", "e", `[{deviceID: ["0x23301234"]}]`),
			gpu: h100,
		},
		"productName glob": {
			doc:            entry("1", "e", `[{productName: ["NVIDIA H100*"]}]`),
			gpu:            h100,
			wantPowerLimit: new(uint32(1)),
		},
		"productName without regard to case": {
			doc:            entry("1", "e", `[{productName: ["nvidia h100 80gb hbm3"]}]`),
			gpu:            h100,
			wantPowerLimit: new(uint32(1)),
		},
		"productName must match the whole name": {
			doc: entry("1", "e", `[{productName: ["NVIDIA H100"]}]`),
			gpu: h100,
		},
		"selectors AND across the list": {
			doc:            entry("1", "e", `[{productName: ["*H100*"]}, {deviceID: ["0x233010DE"]}]`),
			gpu:            h100,
			wantPowerLimit: new(uint32(1)),
		},
		"selectors AND fails when one doesn't match": {
			doc: entry("1", "e", `[{productName: ["*H100*"]}, {deviceID: ["0x233910DE"]}]`),
			gpu: h100,
		},
		"overlap between two deviceID entries": {
			doc:     entry("1", "a", `[{deviceID: ["0x233010DE"]}]`) + entry("2", "b", `[{deviceID: ["0x233010DE"]}]`),
			gpu:     h100,
			wantErr: errOverlap,
		},
		"overlap between deviceID and productName": {
			doc:     entry("1", "a", `[{deviceID: ["0x233010DE"]}]`) + entry("2", "b", `[{productName: ["NVIDIA H100*"]}]`),
			gpu:     h100,
			wantErr: errOverlap,
		},
		"no overlap when only one entry matches this GPU": {
			doc:            entry("1", "a", `[{deviceID: ["0x233010DE"]}]`) + entry("2", "b", `[{productName: ["NVIDIA L40*"]}]`),
			gpu:            h100,
			wantPowerLimit: new(uint32(1)),
		},
		"entry that matches no GPU leaves the GPU to default": {
			doc:            entry("1", "e", `[{productName: ["NVIDIA L40*"]}]`) + "default: {powerLimit: 9}\n",
			gpu:            h100,
			wantPowerLimit: new(uint32(9)),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			d := mustParse(t, "version: v1\ngpus:\n"+tc.doc)
			got, err := d.Resolve([]GPU{tc.gpu})
			require.ErrorIs(t, err, tc.wantErr)
			if tc.wantErr != nil {
				return
			}
			require.Equal(t, tc.wantPowerLimit, got[0].Settings.PowerLimit)
		})
	}
}

func TestResolveNoDefault(t *testing.T) {
	got, err := mustParse(t, "version: v1").Resolve([]GPU{h100})
	require.NoError(t, err)
	require.Equal(t, []Assignment{{GPU: h100}}, got)
}

func TestResolveEntryDoesNotInheritDefault(t *testing.T) {
	d := mustParse(t, entryDoc("{}")+"default: {eccMode: enabled}\n")
	got, err := d.Resolve([]GPU{h100})
	require.NoError(t, err)
	require.Equal(t, Settings{}, got[0].Settings)
}

func TestGlobMatch(t *testing.T) {
	cases := map[string]struct {
		pattern, name string
		want          bool
	}{
		"literal matches itself":            {pattern: "abc", name: "abc", want: true},
		"literal must match the whole name": {pattern: "abc", name: "abcd"},
		"star matches empty":                {pattern: "*", name: "", want: true},
		"star matches anything":             {pattern: "*", name: "anything", want: true},
		"empty matches empty":               {pattern: "", name: "", want: true},
		"empty matches nothing else":        {pattern: "", name: "a"},
		"trailing star matches empty":       {pattern: "a*", name: "a", want: true},
		"trailing star":                     {pattern: "a*", name: "ab", want: true},
		"leading star":                      {pattern: "*c", name: "abc", want: true},
		"leading star needs the suffix":     {pattern: "*c", name: "cb"},
		"inner star matches empty":          {pattern: "a*c", name: "ac", want: true},
		"inner star":                        {pattern: "a*c", name: "abbc", want: true},
		"two stars":                         {pattern: "a*b*c", name: "abc", want: true},
		"two stars keep order":              {pattern: "a*b*c", name: "acb"},
		"prefix and suffix don't share":     {pattern: "a*a", name: "a"},
		"prefix and suffix overlap":         {pattern: "ab*ba", name: "aba"},
		"double star":                       {pattern: "a**b", name: "ab", want: true},
		"question mark isn't a wildcard":    {pattern: "a?c", name: "abc"},
		"question mark matches itself":      {pattern: "a?c", name: "a?c", want: true},
		"bracket isn't a class":             {pattern: "[ab]", name: "a"},
		"bracket matches itself":            {pattern: "[ab]", name: "[ab]", want: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, globMatch(tc.pattern, tc.name))
		})
	}
}
