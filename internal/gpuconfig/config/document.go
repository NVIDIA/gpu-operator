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

// Package config parses and validates GPU configuration documents, matches
// GPUs to their entries, and computes the configuration hash.
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	yamlv2 "go.yaml.in/yaml/v2"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/yaml"
)

const (
	// MaxVersion is the newest schema version this package understands,
	// published as the schema-max label.
	MaxVersion = 1

	// ConfigKey is the ConfigMap key that holds a document.
	ConfigKey = "config.yaml"

	nodeDocumentPrefix = "gpu-config-node-"
	poolDocumentPrefix = "gpu-config-pool-"
)

var (
	// ErrSchema marks a document that breaks the schema.
	ErrSchema = errors.New("schema error")

	errNewerVersion = errors.New("unsupported schema version")
	errOverlap      = errors.New("overlap error")
)

// Source is a snapshot of one ConfigMap that holds a document.
type Source struct {
	Name string
	Data map[string]string
}

// Hash returns the sha256 of the document bytes, or "" when src is nil; a
// missing config.yaml key hashes as empty bytes. The state file stores it, so
// every version must compute the same hash.
func Hash(src *Source) string {
	if src == nil {
		return ""
	}
	sum := sha256.Sum256([]byte(src.Data[ConfigKey]))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ParseSource parses the document under src's config.yaml key.
func ParseSource(src *Source) (*Document, error) {
	data, ok := src.Data[ConfigKey]
	if !ok {
		return nil, fmt.Errorf("%w: ConfigMap %s has no %s key", ErrSchema, src.Name, ConfigKey)
	}
	d, err := parse([]byte(data))
	if err != nil {
		return nil, fmt.Errorf("ConfigMap %s: %w", src.Name, err)
	}
	return d, nil
}

// parse parses and validates one document. Errors wrap ErrSchema or
// errNewerVersion. Only the first YAML document in data is read.
func parse(data []byte) (*Document, error) {
	// The strict conversion rejects duplicate keys.
	j, err := yaml.YAMLToJSONStrict(data)
	if err != nil {
		// A newer version may use YAML this version can't convert.
		if v, ok := newerVersion(data); ok {
			return nil, fmt.Errorf("%w: %s is newer than v%d", errNewerVersion, v, MaxVersion)
		}
		// A null key fails the conversion with an unreadable error.
		if keyErr := checkKeys(data); keyErr != nil {
			return nil, fmt.Errorf("%w: %w", ErrSchema, keyErr)
		}
		return nil, fmt.Errorf("%w: %w", ErrSchema, yamlError(err))
	}
	var d Document
	if err := json.Unmarshal(j, &d); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSchema, err)
	}
	// An empty document never reaches Document.UnmarshalJSON.
	if d.Version == "" {
		return nil, fmt.Errorf("%w: missing version", ErrSchema)
	}
	if d.version > MaxVersion {
		return nil, fmt.Errorf("%w: %s is newer than v%d", errNewerVersion, d.Version, MaxVersion)
	}
	if err := checkKeys(data); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSchema, err)
	}
	if err := checkIntegers(data); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSchema, err)
	}
	return &d, nil
}

// newerVersion returns the document's version when it is a valid version
// newer than MaxVersion.
func newerVersion(data []byte) (string, bool) {
	var doc yamlv2.MapSlice
	if err := yamlv2.Unmarshal(data, &doc); err != nil {
		return "", false
	}
	for _, item := range doc {
		v, ok := item.Value.(string)
		if item.Key != "version" || !ok {
			continue
		}
		n, err := parseVersion(v)
		return v, err == nil && n > MaxVersion
	}
	return "", false
}

// checkKeys rejects entry names that aren't YAML strings: sigs.k8s.io/yaml
// stringifies keys after its duplicate check, so `1:` and `"1":` would merge
// and hide an overlap. Elsewhere it rejects keys sigs.k8s.io/yaml can't
// stringify, such as null, which it reports unreadably.
func checkKeys(data []byte) error {
	var doc yamlv2.MapSlice
	if err := yamlv2.Unmarshal(data, &doc); err != nil {
		return nil
	}
	if err := walkKeys(doc, "", false); err != nil {
		return err
	}
	// MapSlice decoding drops items a `<<` merge key supplies; map decoding keeps them.
	var merged struct {
		GPUs map[any]any `yaml:"gpus"`
	}
	if err := yamlv2.Unmarshal(data, &merged); err != nil {
		return nil
	}
	var bad []string
	for key := range merged.GPUs {
		if _, isString := key.(string); !isString {
			bad = append(bad, describeKey(key))
		}
	}
	if len(bad) == 0 {
		return nil
	}
	slices.Sort(bad)
	return fmt.Errorf("gpus: entry name %s isn't a string; quote it", bad[0])
}

func walkKeys(node any, path string, entryNames bool) error {
	switch n := node.(type) {
	case yamlv2.MapSlice:
		for _, item := range n {
			name, isString := item.Key.(string)
			switch {
			case entryNames && !isString:
				return fmt.Errorf("%sentry name %s isn't a string; quote it", path, describeKey(item.Key))
			case !isString && !stringifiable(item.Key):
				return fmt.Errorf("%skey %s isn't a string; quote it", path, describeKey(item.Key))
			case !isString:
				name = fmt.Sprintf("%v", item.Key)
			}
			if err := walkKeys(item.Value, path+name+": ", path == "" && name == "gpus"); err != nil {
				return err
			}
		}
	case []any:
		prefix := strings.TrimSuffix(path, ": ")
		for i, item := range n {
			if err := walkKeys(item, fmt.Sprintf("%s[%d]: ", prefix, i), false); err != nil {
				return err
			}
		}
	}
	return nil
}

// stringifiable reports whether sigs.k8s.io/yaml converts a key of this type
// to a string.
func stringifiable(key any) bool {
	switch key.(type) {
	case string, bool, int, int64, float64:
		return true
	}
	return false
}

// describeKey names a non-string key's YAML type, since YAML 1.1 reads
// unquoted names such as y, on and no as booleans.
func describeKey(key any) string {
	switch key.(type) {
	case nil:
		return "null (a YAML null)"
	case bool:
		return fmt.Sprintf("%v (a YAML boolean)", key)
	case int, int64, uint64, float64:
		return fmt.Sprintf("%v (a YAML number)", key)
	}
	return fmt.Sprintf("%v", key)
}

// yamlError drops the YAML library's prefixes from a syntax or duplicate-key
// error.
func yamlError(err error) error {
	msg := strings.TrimPrefix(err.Error(), "yaml: ")
	if rest, ok := strings.CutPrefix(msg, "unmarshal errors:\n"); ok {
		lines := strings.Split(rest, "\n")
		for i := range lines {
			lines[i] = strings.TrimSpace(lines[i])
		}
		msg = strings.Join(lines, "; ")
	}
	return fmt.Errorf("invalid YAML: %s", msg)
}

// plainInteger allows the integer-valued float form, such as 500.0, that the
// schema already accepts.
var plainInteger = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.0+)?$`)

// rawNumber holds the source text of a numeric YAML scalar. YAML 1.1 reads
// 0500 as octal 320 and also accepts 0x1F4, 5e2 and 1_000 as numbers, so
// a zero-padded or otherwise unusual value would silently change a setting.
type rawNumber string

func (r *rawNumber) UnmarshalYAML(unmarshal func(any) error) error {
	var v any
	if err := unmarshal(&v); err != nil {
		return err
	}
	switch v.(type) {
	case int, int64, uint64, float64:
		// yaml/v2 decodes a resolved scalar into a string as its source text.
		var s string
		if err := unmarshal(&s); err != nil {
			return err
		}
		*r = rawNumber(s)
	}
	return nil
}

func (r *rawNumber) check() error {
	if r == nil || *r == "" || plainInteger.MatchString(string(*r)) {
		return nil
	}
	return fmt.Errorf("%s isn't a plain decimal integer", string(*r))
}

type rawClockRange struct {
	Min *rawNumber `yaml:"min"`
	Max *rawNumber `yaml:"max"`
}

func (c *rawClockRange) check() error {
	if c == nil {
		return nil
	}
	if err := c.Max.check(); err != nil {
		return fmt.Errorf("max: %w", err)
	}
	if err := c.Min.check(); err != nil {
		return fmt.Errorf("min: %w", err)
	}
	return nil
}

type rawSettings struct {
	PowerLimit   *rawNumber     `yaml:"powerLimit"`
	GPUClocks    *rawClockRange `yaml:"gpuClocks"`
	MemoryClocks *rawClockRange `yaml:"memoryClocks"`
}

// check reports keys in sorted order, as the schema decoders do.
func (s *rawSettings) check() error {
	if s == nil {
		return nil
	}
	if err := s.GPUClocks.check(); err != nil {
		return fmt.Errorf("gpuClocks: %w", err)
	}
	if err := s.MemoryClocks.check(); err != nil {
		return fmt.Errorf("memoryClocks: %w", err)
	}
	if err := s.PowerLimit.check(); err != nil {
		return fmt.Errorf("powerLimit: %w", err)
	}
	return nil
}

// checkIntegers rejects integer settings not written as plain decimal
// integers. It runs after the schema decode, so every value has a valid shape.
func checkIntegers(data []byte) error {
	var doc struct {
		GPUs map[string]struct {
			Settings *rawSettings `yaml:"settings"`
		} `yaml:"gpus"`
		Default *rawSettings `yaml:"default"`
	}
	if err := yamlv2.Unmarshal(data, &doc); err != nil {
		return err
	}
	if err := doc.Default.check(); err != nil {
		return fmt.Errorf("default: %w", err)
	}
	for _, name := range slices.Sorted(maps.Keys(doc.GPUs)) {
		if err := doc.GPUs[name].Settings.check(); err != nil {
			return fmt.Errorf("gpus: %s: settings: %w", name, err)
		}
	}
	return nil
}

// NodeDocumentName returns the name of nodeName's node document, and false
// when that name isn't a valid ConfigMap name.
func NodeDocumentName(nodeName string) (string, bool) {
	return validName(nodeDocumentPrefix + nodeName)
}

// PoolDocumentName returns the name of the pool document for a pool label
// value: uppercase letters become lowercase and each "_" becomes "-". It
// returns false when the result isn't a valid ConfigMap name; such a pool has
// no pool document.
func PoolDocumentName(poolLabelValue string) (string, bool) {
	pool := strings.ReplaceAll(strings.ToLower(poolLabelValue), "_", "-")
	return validName(poolDocumentPrefix + pool)
}

func validName(name string) (string, bool) {
	if len(validation.IsDNS1123Subdomain(name)) > 0 {
		return "", false
	}
	return name, true
}
