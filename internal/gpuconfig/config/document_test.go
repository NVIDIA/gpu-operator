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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A document that uses every setting, both selector fields and default.
const exampleDoc = `version: v1
gpus:
  h100-sxm5:
    deviceSelectors:
      - deviceID:
          - "0x233010DE"
          - "0x233910DE"
    settings:
      eccMode: enabled
      powerLimit: 500
      gpuClocks:
        min: 1590
        max: 1980
  l40:
    deviceSelectors:
      - productName:
          - "NVIDIA L40*"
    settings:
      eccMode: disabled
      powerLimit: 250
      memoryClocks:
        min: 9001
        max: 9001
# Settings for every GPU that matches no entry.
default:
  eccMode: enabled
`

func TestParseExample(t *testing.T) {
	d, err := parse([]byte(exampleDoc))
	require.NoError(t, err)
	require.Equal(t, &Document{
		Version: "v1",
		GPUs: map[string]Entry{
			"h100-sxm5": {
				DeviceSelectors: []Selector{{DeviceID: []string{"0x233010DE", "0x233910DE"}}},
				Settings: Settings{
					ECCMode:    new(ECCEnabled),
					PowerLimit: new(uint32(500)),
					GPUClocks:  &ClockRange{Min: 1590, Max: 1980},
				},
			},
			"l40": {
				DeviceSelectors: []Selector{{ProductName: []string{"NVIDIA L40*"}}},
				Settings: Settings{
					ECCMode:      new(ECCDisabled),
					PowerLimit:   new(uint32(250)),
					MemoryClocks: &ClockRange{Min: 9001, Max: 9001},
				},
			},
		},
		Default: &Settings{ECCMode: new(ECCEnabled)},
	}, d)
}

// withEntry adds an entry to exampleDoc's gpus.
func withEntry(entry string) string {
	return strings.Replace(exampleDoc, "# Settings for every GPU", entry+"# Settings for every GPU", 1)
}

// entryDoc wraps settings in one entry with a deviceID selector.
func entryDoc(settings string) string {
	return "version: v1\ngpus:\n  e:\n    deviceSelectors: [{deviceID: [\"0x233010DE\"]}]\n    settings: " + settings + "\n"
}

// selectorDoc wraps selectors in one entry with empty settings.
func selectorDoc(selectors string) string {
	return "version: v1\ngpus:\n  e:\n    deviceSelectors: " + selectors + "\n    settings: {}\n"
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"empty config.yaml":                  "",
		"invalid YAML":                       "version: [v1",
		"top-level list":                     "- version: v1",
		"missing version":                    "gpus: {}",
		"version as YAML integer":            "version: 1",
		"unsupported version":                "version: v2",
		"newer version with a new key":       "version: v2\nnewKey: 1",
		"null key":                           "version: v1\n~: 1",
		"unknown top-level key":              "version: v1\nentries: {}",
		"key in wrong case":                  entryDoc("{PowerLimit: 500}"),
		"unknown key set to null":            entryDoc("{persistenceMode: null}"),
		"duplicate setting key":              entryDoc("{powerLimit: 1, powerLimit: 2}"),
		"duplicate entry name":               "version: v1\ngpus:\n  e: {deviceSelectors: [{deviceID: [\"0x233010DE\"]}]}\n  e: {deviceSelectors: [{productName: [\"*\"]}]}",
		"fractional powerLimit":              entryDoc("{powerLimit: 500.5}"),
		"quoted powerLimit":                  entryDoc("{powerLimit: \"500\"}"),
		"negative powerLimit":                entryDoc("{powerLimit: -1}"),
		"powerLimit overflows milliwatts":    entryDoc("{powerLimit: 4294968}"),
		"clock min above max":                entryDoc("{gpuClocks: {min: 3, max: 2}}"),
		"invalid eccMode":                    entryDoc("{eccMode: on-reboot}"),
		"YAML boolean as eccMode":            entryDoc("{eccMode: yes}"),
		"entry without deviceSelectors":      "version: v1\ngpus:\n  e: {settings: {}}",
		"empty deviceSelectors":              selectorDoc("[]"),
		"selector with no field":             selectorDoc("[{}]"),
		"selector with two fields":           selectorDoc(`[{deviceID: ["0x233010DE"], productName: ["x"]}]`),
		"empty deviceID list":                selectorDoc("[{deviceID: []}]"),
		"empty productName list":             selectorDoc("[{productName: []}]"),
		"null productName item":              selectorDoc("[{productName: [null]}]"),
		"invalid deviceID in a later item":   selectorDoc(`[{productName: ["*"]}, {deviceID: ["0x233010DE", "0x2330"]}]`),
		"schema error in an unmatched entry": withEntry("  unused: {deviceSelectors: [{productName: [\"none\"]}], settings: {powerLimit: x}}\n"),
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parse([]byte(doc))
			require.ErrorIs(t, err, ErrSchema)
		})
	}
}

func TestParseValid(t *testing.T) {
	cases := map[string]struct {
		doc  string
		want *Document
	}{
		"only version": {
			doc:  "version: v1",
			want: &Document{Version: "v1"},
		},
		"only the first YAML document is read": {
			doc:  "version: v1\n---\nversion: v2\nbogus: 1",
			want: &Document{Version: "v1"},
		},
		"null settings are unmanaged": {
			doc:  entryDoc("{powerLimit: null, gpuClocks: null, memoryClocks: null, eccMode: null}"),
			want: oneEntry(Selector{DeviceID: []string{"0x233010DE"}}, Settings{}),
		},
		"null selector field beside a set one": {
			doc:  selectorDoc(`[{deviceID: ["0X233010de"], productName: null}]`),
			want: oneEntry(Selector{DeviceID: []string{"0X233010de"}}, Settings{}),
		},
		"entry without settings": {
			doc:  "version: v1\ngpus:\n  e: {deviceSelectors: [{deviceID: [\"0x233010DE\"]}]}",
			want: oneEntry(Selector{DeviceID: []string{"0x233010DE"}}, Settings{}),
		},
		"missing clock bound is 0": {
			doc:  entryDoc("{gpuClocks: {max: 2}}"),
			want: oneEntry(Selector{DeviceID: []string{"0x233010DE"}}, Settings{GPUClocks: &ClockRange{Max: 2}}),
		},
		"largest powerLimit NVML takes": {
			doc:  entryDoc("{powerLimit: 4294967}"),
			want: oneEntry(Selector{DeviceID: []string{"0x233010DE"}}, Settings{PowerLimit: new(uint32(4294967))}),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			d, err := parse([]byte(tc.doc))
			require.NoError(t, err)
			require.Equal(t, tc.want, d)
		})
	}
}

func oneEntry(sel Selector, settings Settings) *Document {
	return &Document{Version: "v1", GPUs: map[string]Entry{
		"e": {DeviceSelectors: []Selector{sel}, Settings: settings},
	}}
}

func TestParseSource(t *testing.T) {
	cases := map[string]struct {
		data     map[string]string
		wantErr  error
		wantGPUs int
	}{
		"no config.yaml key": {
			data:    map[string]string{"other": "x"},
			wantErr: ErrSchema,
		},
		"valid document": {
			data:     map[string]string{ConfigKey: exampleDoc},
			wantGPUs: 2,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			d, err := ParseSource(&Source{Name: "gpu-config", Data: tc.data})
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			require.Len(t, d.GPUs, tc.wantGPUs)
		})
	}
}

func TestHash(t *testing.T) {
	cases := map[string]struct {
		src  *Source
		want string
	}{
		"no document": {
			src:  nil,
			want: "",
		},
		"no config.yaml key hashes as empty bytes": {
			src:  &Source{Data: map[string]string{"other": "x"}},
			want: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		"document bytes": {
			src:  &Source{Data: map[string]string{ConfigKey: "version: v1\n"}},
			want: "sha256:aefdbe17a1506d3662fc7ea9484fdcb1d20276d28139b0dda28fdab0e162e376",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, Hash(tc.src))
		})
	}

	// Any edit changes the hash, including whitespace.
	a := Hash(&Source{Data: map[string]string{ConfigKey: "version: v1\n"}})
	b := Hash(&Source{Data: map[string]string{ConfigKey: "version: v1\n\n"}})
	require.NotEqual(t, a, b)
}

func TestDocumentNames(t *testing.T) {
	cases := map[string]struct {
		fn     func(string) (string, bool)
		in     string
		want   string
		wantOK bool
	}{
		"node":                    {fn: NodeDocumentName, in: "node-1", want: "gpu-config-node-node-1", wantOK: true},
		"node name of 237 chars":  {fn: NodeDocumentName, in: strings.Repeat("a", 237), want: "gpu-config-node-" + strings.Repeat("a", 237), wantOK: true},
		"node name over 237":      {fn: NodeDocumentName, in: strings.Repeat("a", 238)},
		"pool":                    {fn: PoolDocumentName, in: "train", want: "gpu-config-pool-train", wantOK: true},
		"pool normalized":         {fn: PoolDocumentName, in: "GPU_Train", want: "gpu-config-pool-gpu-train", wantOK: true},
		"pool with dots":          {fn: PoolDocumentName, in: "a.b", want: "gpu-config-pool-a.b", wantOK: true},
		"pool still invalid":      {fn: PoolDocumentName, in: "a..b"},
		"pool with trailing dash": {fn: PoolDocumentName, in: "a_"},
		"pool empty":              {fn: PoolDocumentName, in: ""},
		"pool with space":         {fn: PoolDocumentName, in: "a b"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, ok := tc.fn(tc.in)
			require.Equal(t, tc.wantOK, ok)
			require.Equal(t, tc.want, got)
		})
	}
}
