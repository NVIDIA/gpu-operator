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
	"errors"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/json"
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

	errOverlap = errors.New("overlap error")
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

// parse parses and validates one document. Errors wrap ErrSchema. Only the
// first YAML document in data is read.
func parse(data []byte) (*Document, error) {
	// The strict conversion rejects duplicate keys.
	j, err := yaml.YAMLToJSONStrict(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSchema, err)
	}
	// Keys match case-sensitively, and unknown keys are errors.
	var d Document
	strictErrs, err := json.UnmarshalStrict(j, &d)
	if err == nil {
		err = errors.Join(strictErrs...)
	}
	if err == nil {
		err = d.validate()
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSchema, err)
	}
	return &d, nil
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
