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
		version: 1,
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
	cases := map[string]struct {
		doc     string
		wantErr error
	}{
		"missing version":                         {doc: "gpus: {}", wantErr: ErrSchema},
		"version null":                            {doc: "version: null", wantErr: ErrSchema},
		"version without v":                       {doc: "version: \"1\"", wantErr: ErrSchema},
		"version as YAML integer":                 {doc: "version: 1", wantErr: ErrSchema},
		"version v0":                              {doc: "version: v0", wantErr: ErrSchema},
		"version V1":                              {doc: "version: V1", wantErr: ErrSchema},
		"version v01":                             {doc: "version: v01", wantErr: ErrSchema},
		"non-integer powerLimit":                  {doc: entryDoc("{powerLimit: 500.5}"), wantErr: ErrSchema},
		"quoted powerLimit":                       {doc: entryDoc("{powerLimit: \"500\"}"), wantErr: ErrSchema},
		"non-integer clock bound":                 {doc: entryDoc("{gpuClocks: {min: 1.5, max: 2}}"), wantErr: ErrSchema},
		"key in wrong case":                       {doc: entryDoc("{PowerLimit: 500}"), wantErr: ErrSchema},
		"value in wrong case":                     {doc: entryDoc("{eccMode: Enabled}"), wantErr: ErrSchema},
		"YAML boolean as eccMode":                 {doc: entryDoc("{eccMode: yes}"), wantErr: ErrSchema},
		"YAML true as eccMode":                    {doc: entryDoc("{eccMode: true}"), wantErr: ErrSchema},
		"quoted yes as eccMode":                   {doc: entryDoc("{eccMode: \"yes\"}"), wantErr: ErrSchema},
		"empty config.yaml":                       {doc: "", wantErr: ErrSchema},
		"only comments":                           {doc: "# nothing here\n", wantErr: ErrSchema},
		"duplicate top-level key":                 {doc: "version: v1\nversion: v1", wantErr: ErrSchema},
		"duplicate entry name":                    {doc: "version: v1\ngpus:\n  e: {deviceSelectors: [{deviceID: [\"0x233010DE\"]}], settings: {}}\n  e: {deviceSelectors: [{deviceID: [\"0x233010DE\"]}], settings: {}}", wantErr: ErrSchema},
		"duplicate setting key":                   {doc: entryDoc("{powerLimit: 1, powerLimit: 2}"), wantErr: ErrSchema},
		"deviceID with subsystem suffix":          {doc: selectorDoc("[{deviceID: [\"0x233010DE:0x16C110DE\"]}]"), wantErr: ErrSchema},
		"deviceID too short":                      {doc: selectorDoc("[{deviceID: [\"0x2330\"]}]"), wantErr: ErrSchema},
		"deviceID without 0x":                     {doc: selectorDoc("[{deviceID: [\"233010DE\"]}]"), wantErr: ErrSchema},
		"deviceID unquoted":                       {doc: selectorDoc("[{deviceID: [0x233010DE]}]"), wantErr: ErrSchema},
		"productName as number":                   {doc: selectorDoc("[{productName: [100]}]"), wantErr: ErrSchema},
		"empty deviceSelectors":                   {doc: selectorDoc("[]"), wantErr: ErrSchema},
		"empty deviceID list":                     {doc: selectorDoc("[{deviceID: []}]"), wantErr: ErrSchema},
		"empty productName list":                  {doc: selectorDoc("[{productName: []}]"), wantErr: ErrSchema},
		"null deviceID item":                      {doc: selectorDoc("[{deviceID: [null]}]"), wantErr: ErrSchema},
		"null productName item":                   {doc: selectorDoc("[{productName: [null]}]"), wantErr: ErrSchema},
		"negative powerLimit":                     {doc: entryDoc("{powerLimit: -1}"), wantErr: ErrSchema},
		"powerLimit over uint32":                  {doc: entryDoc("{powerLimit: 4294967296}"), wantErr: ErrSchema},
		"powerLimit overflows milliwatts":         {doc: entryDoc("{powerLimit: 4294968}"), wantErr: ErrSchema},
		"clock over uint32":                       {doc: entryDoc("{gpuClocks: {min: 1, max: 4294967296}}"), wantErr: ErrSchema},
		"newer version":                           {doc: "version: v2\nnewKey: 1", wantErr: errNewerVersion},
		"much newer version":                      {doc: "version: v99999999999999999999", wantErr: errNewerVersion},
		"newer version, unconvertible YAML":       {doc: "version: v2\nnewKey: {~: 1}", wantErr: errNewerVersion},
		"newer version, duplicate key":            {doc: "version: v2\nnewKey: 1\nnewKey: 2", wantErr: errNewerVersion},
		"invalid YAML":                            {doc: "version: [v1", wantErr: ErrSchema},
		"top-level list":                          {doc: "- version: v1", wantErr: ErrSchema},
		"unknown top-level key":                   {doc: "version: v1\nentries: {}", wantErr: ErrSchema},
		"unknown setting key":                     {doc: entryDoc("{persistenceMode: enabled}"), wantErr: ErrSchema},
		"unknown key in default":                  {doc: "version: v1\ndefault: {eccmode: enabled}", wantErr: ErrSchema},
		"unknown clock key":                       {doc: entryDoc("{gpuClocks: {min: 1, max: 2, step: 1}}"), wantErr: ErrSchema},
		"clock without max":                       {doc: entryDoc("{gpuClocks: {min: 1}}"), wantErr: ErrSchema},
		"clock with null min":                     {doc: entryDoc("{memoryClocks: {min: null, max: 2}}"), wantErr: ErrSchema},
		"clock min above max":                     {doc: entryDoc("{gpuClocks: {min: 3, max: 2}}"), wantErr: ErrSchema},
		"invalid eccMode":                         {doc: entryDoc("{eccMode: on-reboot}"), wantErr: ErrSchema},
		"entry without deviceSelectors":           {doc: "version: v1\ngpus:\n  e: {settings: {}}", wantErr: ErrSchema},
		"entry without settings":                  {doc: "version: v1\ngpus:\n  e: {deviceSelectors: [{deviceID: [\"0x233010DE\"]}]}", wantErr: ErrSchema},
		"entry with null settings":                {doc: "version: v1\ngpus:\n  e: {deviceSelectors: [{deviceID: [\"0x233010DE\"]}], settings: null}", wantErr: ErrSchema},
		"entry with no body":                      {doc: "version: v1\ngpus:\n  e:", wantErr: ErrSchema},
		"unknown entry key":                       {doc: "version: v1\ngpus:\n  e: {deviceSelectors: [{deviceID: [\"0x233010DE\"]}], settings: {}, priority: 1}", wantErr: ErrSchema},
		"selector with no field":                  {doc: selectorDoc("[{}]"), wantErr: ErrSchema},
		"selector that is null":                   {doc: selectorDoc("[null]"), wantErr: ErrSchema},
		"selector with two fields":                {doc: selectorDoc("[{deviceID: [\"0x233010DE\"], productName: [\"x\"]}]"), wantErr: ErrSchema},
		"selector with unknown field":             {doc: selectorDoc("[{uuid: [\"GPU-1\"]}]"), wantErr: ErrSchema},
		"selector field in wrong case":            {doc: selectorDoc("[{deviceId: [\"0x233010DE\"]}]"), wantErr: ErrSchema},
		"wrong type for gpus":                     {doc: "version: v1\ngpus: [a]", wantErr: ErrSchema},
		"entry name that is a YAML integer":       {doc: "version: v1\ngpus:\n  1: {deviceSelectors: [{deviceID: [\"0x233010DE\"]}], settings: {}}", wantErr: ErrSchema},
		"entry names that become the same string": {doc: "version: v1\ngpus:\n  on: {deviceSelectors: [{deviceID: [\"0x233010DE\"]}], settings: {}}\n  \"true\": {deviceSelectors: [{productName: [\"*\"]}], settings: {}}", wantErr: ErrSchema},
		"null entry name":                         {doc: "version: v1\ngpus:\n  ~: {deviceSelectors: [{deviceID: [\"0x233010DE\"]}], settings: {}}", wantErr: ErrSchema},
		"unknown top-level key set to null":       {doc: "version: v1\nentries: null", wantErr: ErrSchema},
		"unknown setting key set to null":         {doc: entryDoc("{PowerLimit: null}"), wantErr: ErrSchema},
		"unknown clock key set to null":           {doc: entryDoc("{gpuClocks: {min: 1, max: 2, step: null}}"), wantErr: ErrSchema},
		"unknown selector field set to null":      {doc: selectorDoc("[{deviceID: [\"0x233010DE\"], uuid: null}]"), wantErr: ErrSchema},
		"unknown entry key set to null":           {doc: "version: v1\ngpus:\n  e: {deviceSelectors: [{deviceID: [\"0x233010DE\"]}], settings: {}, priority: ~}", wantErr: ErrSchema},
		"wrong type for settings":                 {doc: entryDoc("[powerLimit]"), wantErr: ErrSchema},
		"schema error in an unmatched entry":      {doc: withEntry("  unused: {deviceSelectors: [{productName: [\"none\"]}], settings: {powerLimit: x}}\n"), wantErr: ErrSchema},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parse([]byte(tc.doc))
			require.ErrorIs(t, err, tc.wantErr)
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
			want: &Document{Version: "v1", version: 1},
		},
		"gpus and default null": {
			doc:  "version: v1\ngpus: null\ndefault: null",
			want: &Document{Version: "v1", version: 1},
		},
		"gpus empty": {
			doc:  "version: v1\ngpus: {}",
			want: &Document{Version: "v1", GPUs: map[string]Entry{}, version: 1},
		},
		"default empty": {
			doc:  "version: v1\ndefault: {}",
			want: &Document{Version: "v1", Default: &Settings{}, version: 1},
		},
		"settings empty": {
			doc: selectorDoc(`[{deviceID: ["0x233010de"]}]`),
			want: &Document{Version: "v1", version: 1, GPUs: map[string]Entry{
				"e": {DeviceSelectors: []Selector{{DeviceID: []string{"0x233010de"}}}},
			}},
		},
		"null settings are unmanaged": {
			doc: entryDoc("{powerLimit: null, gpuClocks: null, memoryClocks: null, eccMode: null}"),
			want: &Document{Version: "v1", version: 1, GPUs: map[string]Entry{
				"e": {DeviceSelectors: []Selector{{DeviceID: []string{"0x233010DE"}}}},
			}},
		},
		"null selector field beside a set one": {
			doc: selectorDoc(`[{deviceID: ["0x233010DE"], productName: null}]`),
			want: &Document{Version: "v1", version: 1, GPUs: map[string]Entry{
				"e": {DeviceSelectors: []Selector{{DeviceID: []string{"0x233010DE"}}}},
			}},
		},
		"largest powerLimit NVML takes": {
			doc: entryDoc("{powerLimit: 4294967}"),
			want: &Document{Version: "v1", version: 1, GPUs: map[string]Entry{
				"e": {
					DeviceSelectors: []Selector{{DeviceID: []string{"0x233010DE"}}},
					Settings:        Settings{PowerLimit: new(uint32(4294967))},
				},
			}},
		},
		"integer-valued float is an integer": {
			doc: entryDoc("{powerLimit: 500.0}"),
			want: &Document{Version: "v1", version: 1, GPUs: map[string]Entry{
				"e": {
					DeviceSelectors: []Selector{{DeviceID: []string{"0x233010DE"}}},
					Settings:        Settings{PowerLimit: new(uint32(500))},
				},
			}},
		},
		"only the first YAML document is read": {
			doc:  "version: v1\n---\nversion: v2\nbogus: 1",
			want: &Document{Version: "v1", version: 1},
		},
		"uppercase 0X deviceID prefix": {
			doc: selectorDoc(`[{deviceID: ["0X233010DE"]}]`),
			want: &Document{Version: "v1", version: 1, GPUs: map[string]Entry{
				"e": {DeviceSelectors: []Selector{{DeviceID: []string{"0X233010DE"}}}},
			}},
		},
		"quoted numeric entry name": {
			doc: "version: v1\ngpus:\n  \"1\": {deviceSelectors: [{deviceID: [\"0x233010DE\"]}], settings: {}}",
			want: &Document{Version: "v1", version: 1, GPUs: map[string]Entry{
				"1": {DeviceSelectors: []Selector{{DeviceID: []string{"0x233010DE"}}}},
			}},
		},
		"leading document marker": {
			doc:  "---\nversion: v1",
			want: &Document{Version: "v1", version: 1},
		},
		"anchor and merge key": {
			doc: "version: v1\ndefault: &s {powerLimit: 300}\ngpus:\n  e:\n    deviceSelectors: [{deviceID: [\"0x233010DE\"]}]\n    settings: {<<: *s, eccMode: enabled}\n",
			want: &Document{Version: "v1", version: 1,
				Default: &Settings{PowerLimit: new(uint32(300))},
				GPUs: map[string]Entry{
					"e": {
						DeviceSelectors: []Selector{{DeviceID: []string{"0x233010DE"}}},
						Settings:        Settings{PowerLimit: new(uint32(300)), ECCMode: new(ECCEnabled)},
					},
				}},
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

// The applier logs these messages, so they name the YAML path and describe
// the value the user wrote rather than the Go types it decodes into.
func TestParseErrorMessages(t *testing.T) {
	cases := map[string]struct {
		doc     string
		wantErr string
	}{
		"clock min above max": {
			doc:     entryDoc("{gpuClocks: {min: 3, max: 2}}"),
			wantErr: "schema error: gpus: e: settings: gpuClocks: min 3 is greater than max 2",
		},
		"string powerLimit": {
			doc:     defaultDoc("{powerLimit: x}"),
			wantErr: "schema error: default: powerLimit: must be an integer",
		},
		"fractional powerLimit": {
			doc:     defaultDoc("{powerLimit: 500.5}"),
			wantErr: "schema error: default: powerLimit: must be an integer",
		},
		"negative powerLimit": {
			doc:     defaultDoc("{powerLimit: -1}"),
			wantErr: "schema error: default: powerLimit: must be an integer from 0 to 4294967295",
		},
		"powerLimit over milliwatt limit": {
			doc:     defaultDoc("{powerLimit: 4294968}"),
			wantErr: "schema error: default: powerLimit: 4294968 W is above 4294967 W, the largest limit NVML accepts",
		},
		"list as clock bound": {
			doc:     entryDoc("{memoryClocks: {min: [1], max: 2}}"),
			wantErr: "schema error: gpus: e: settings: memoryClocks: min: must be an integer",
		},
		"YAML boolean as eccMode": {
			doc:     defaultDoc("{eccMode: true}"),
			wantErr: `schema error: default: eccMode: must be "enabled" or "disabled"`,
		},
		"unknown eccMode": {
			doc:     defaultDoc("{eccMode: Enabled}"),
			wantErr: `schema error: default: eccMode: must be "enabled" or "disabled"`,
		},
		"list as settings": {
			doc:     defaultDoc("[1]"),
			wantErr: "schema error: default: must be a map",
		},
		"top-level list": {
			doc:     "- version: v1",
			wantErr: "schema error: the document must be a map",
		},
		"YAML integer as version": {
			doc:     "version: 1",
			wantErr: `schema error: version: must be a string such as "v1"`,
		},
		"version without v": {
			doc:     `version: "1"`,
			wantErr: `schema error: version "1" must be v followed by a positive integer, such as v1`,
		},
		"invalid YAML": {
			doc:     "version: [v1",
			wantErr: "schema error: invalid YAML: line 1: did not find expected ',' or ']'",
		},
		"duplicate key": {
			doc:     "version: v1\nversion: v1",
			wantErr: `schema error: invalid YAML: line 2: key "version" already set in map`,
		},
		"merge key and a duplicate key": {
			doc:     "version: v1\ndefault: &s {powerLimit: 300}\ngpus:\n  e:\n    deviceSelectors: [{deviceID: [\"0x233010DE\"]}]\n    settings: {<<: *s, powerLimit: 200}\n",
			wantErr: `schema error: invalid YAML: line 6: key "powerLimit" already set in map`,
		},
		"bad deviceID names its list index": {
			doc:     selectorDoc(`[{productName: ["*"]}, {deviceID: ["0x233010DE", "0x2330"]}]`),
			wantErr: `schema error: gpus: e: deviceSelectors[1]: deviceID[1]: "0x2330" must be "0x" followed by 8 hex digits`,
		},
		"number in productName": {
			doc:     selectorDoc(`[{productName: [100]}]`),
			wantErr: "schema error: gpus: e: deviceSelectors[0]: productName[0]: must be a string",
		},
		"string as deviceID": {
			doc:     selectorDoc(`[{deviceID: "0x233010DE"}]`),
			wantErr: "schema error: gpus: e: deviceSelectors[0]: deviceID: must be a list",
		},
		"empty selector": {
			doc:     selectorDoc(`[{deviceID: ["0x233010DE"]}, {}]`),
			wantErr: "schema error: gpus: e: deviceSelectors[1]: must set exactly one of deviceID and productName, found 0",
		},
		"empty deviceSelectors": {
			doc:     selectorDoc("[]"),
			wantErr: "schema error: gpus: e: deviceSelectors: must not be an empty list",
		},
		"YAML boolean as entry name": {
			doc:     "version: v1\ngpus:\n  y: {deviceSelectors: [{deviceID: [\"0x233010DE\"]}], settings: {}}",
			wantErr: "schema error: gpus: entry name true (a YAML boolean) isn't a string; quote it",
		},
		"YAML integer as entry name": {
			doc:     "version: v1\ngpus:\n  1: {deviceSelectors: [{deviceID: [\"0x233010DE\"]}], settings: {}}",
			wantErr: "schema error: gpus: entry name 1 (a YAML number) isn't a string; quote it",
		},
		"YAML integer entry name and its quoted twin in a merge": {
			doc:     "version: v1\ngpus:\n  <<:\n    1: {deviceSelectors: [{productName: [\"*\"]}], settings: {powerLimit: 300}}\n    \"1\": {deviceSelectors: [{productName: [\"*\"]}], settings: {powerLimit: 200}}\n",
			wantErr: "schema error: gpus: entry name 1 (a YAML number) isn't a string; quote it",
		},
		"merged YAML integer entry name beside its quoted twin": {
			doc:     "version: v1\ngpus:\n  <<:\n    1: {deviceSelectors: [{productName: [\"*\"]}], settings: {powerLimit: 300}}\n  \"1\": {deviceSelectors: [{productName: [\"*\"]}], settings: {powerLimit: 200}}\n",
			wantErr: "schema error: gpus: entry name 1 (a YAML number) isn't a string; quote it",
		},
		"merged YAML integer entry name": {
			doc:     "version: v1\ngpus:\n  <<:\n    1: {deviceSelectors: [{productName: [\"*\"]}], settings: {powerLimit: 300}}\n",
			wantErr: "schema error: gpus: entry name 1 (a YAML number) isn't a string; quote it",
		},
		"null entry name": {
			doc:     "version: v1\ngpus:\n  ~: {deviceSelectors: [{deviceID: [\"0x233010DE\"]}], settings: {}}",
			wantErr: "schema error: gpus: entry name null (a YAML null) isn't a string; quote it",
		},
		"null key in default": {
			doc:     defaultDoc("{~: 1}"),
			wantErr: "schema error: default: key null (a YAML null) isn't a string; quote it",
		},
		"null key in a selector": {
			doc:     selectorDoc("[{deviceID: [\"0x233010DE\"]}, {~: 1}]"),
			wantErr: "schema error: gpus: e: deviceSelectors[1]: key null (a YAML null) isn't a string; quote it",
		},
		"null top-level key": {
			doc:     "version: v1\n~: 1",
			wantErr: "schema error: key null (a YAML null) isn't a string; quote it",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parse([]byte(tc.doc))
			require.ErrorIs(t, err, ErrSchema)
			require.EqualError(t, err, tc.wantErr)
		})
	}
}

func defaultDoc(settings string) string {
	return "version: v1\ndefault: " + settings + "\n"
}

func TestParseIntegerForms(t *testing.T) {
	forms := map[string]struct {
		literal string
		want    uint32
		wantErr bool
	}{
		"plain":                {literal: "500", want: 500},
		"zero":                 {literal: "0", want: 0},
		"integer-valued float": {literal: "500.0", want: 500},
		"octal":                {literal: "0500", wantErr: true},
		"zero-padded float":    {literal: "0500.0", wantErr: true},
		"hex":                  {literal: "0x1F4", wantErr: true},
		"binary":               {literal: "0b111110100", wantErr: true},
		"exponent":             {literal: "5e2", wantErr: true},
		"float with exponent":  {literal: "5.0e+2", wantErr: true},
		"underscore":           {literal: "1_000", wantErr: true},
		"explicit plus sign":   {literal: "+500", wantErr: true},
	}
	places := map[string]struct {
		doc     func(literal string) string
		wantErr string
		got     func(*Document) uint32
	}{
		"entry powerLimit": {
			doc:     func(l string) string { return entryDoc("{powerLimit: " + l + "}") },
			wantErr: "gpus: e: settings: powerLimit: %s isn't a plain decimal integer",
			got:     func(d *Document) uint32 { return *d.GPUs["e"].Settings.PowerLimit },
		},
		"entry gpuClocks min": {
			doc:     func(l string) string { return entryDoc("{gpuClocks: {min: " + l + ", max: 5000}}") },
			wantErr: "gpus: e: settings: gpuClocks: min: %s isn't a plain decimal integer",
			got:     func(d *Document) uint32 { return d.GPUs["e"].Settings.GPUClocks.Min },
		},
		"default powerLimit": {
			doc:     func(l string) string { return defaultDoc("{powerLimit: " + l + "}") },
			wantErr: "default: powerLimit: %s isn't a plain decimal integer",
			got:     func(d *Document) uint32 { return *d.Default.PowerLimit },
		},
		"default memoryClocks max": {
			doc:     func(l string) string { return defaultDoc("{memoryClocks: {min: 0, max: " + l + "}}") },
			wantErr: "default: memoryClocks: max: %s isn't a plain decimal integer",
			got:     func(d *Document) uint32 { return d.Default.MemoryClocks.Max },
		},
	}
	for placeName, p := range places {
		for formName, f := range forms {
			t.Run(placeName+"/"+formName, func(t *testing.T) {
				d, err := parse([]byte(p.doc(f.literal)))
				if f.wantErr {
					require.ErrorIs(t, err, ErrSchema)
					require.ErrorContains(t, err, fmt.Sprintf(p.wantErr, f.literal))
					return
				}
				require.NoError(t, err)
				require.Equal(t, f.want, p.got(d))
			})
		}
	}
}

func TestParseSource(t *testing.T) {
	cases := map[string]struct {
		data     map[string]string
		wantErr  error
		wantMsg  string
		wantGPUs int
	}{
		"no config.yaml key": {
			data:    map[string]string{"other": "x"},
			wantErr: ErrSchema,
			wantMsg: "schema error: ConfigMap gpu-config has no config.yaml key",
		},
		"newer version names the ConfigMap": {
			data:    map[string]string{ConfigKey: "version: v2"},
			wantErr: errNewerVersion,
			wantMsg: "ConfigMap gpu-config: unsupported schema version: v2 is newer than v1",
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
				require.EqualError(t, err, tc.wantMsg)
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
