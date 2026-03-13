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

package utils

import (
	"fmt"
	"hash"
	"hash/fnv"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/davecgh/go-spew/spew"
	"k8s.io/apimachinery/pkg/util/rand"
)

// GetFilesWithSuffix returns all files under a given base directory that have a specific suffix
// The operation is performed recursively on subdirectories as well
func GetFilesWithSuffix(baseDir string, suffixes ...string) ([]string, error) {
	var files []string
	err := filepath.Walk(baseDir, func(path string, info os.FileInfo, err error) error {
		// Error during traversal
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		// Skip non suffix files
		base := info.Name()
		for _, s := range suffixes {
			if strings.HasSuffix(base, s) {
				files = append(files, path)
			}
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("error traversing directory tree: %w", err)
	}
	return files, nil
}

var spewPrinter = spew.ConfigState{
	Indent:         " ",
	SortKeys:       true,
	DisableMethods: true,
	SpewKeys:       true,
}

// GetObjectHash returns an FNV-32a hash of the full object (all fields).
func GetObjectHash(obj interface{}) string {
	hasher := fnv.New32a()
	spewPrinter.Fprintf(hasher, "%#v", obj)
	return fmt.Sprint(hasher.Sum32())
}

// GetObjectHashIgnoreEmptyKeys returns an FNV-32a hash of only the non-zero
// fields of a struct. Adding a new zero-valued field will not change
// the digest. Embedded structs are flattened.
func GetObjectHashIgnoreEmptyKeys(obj interface{}) string {
	hasher := fnv.New32a()
	hashNonZeroFields(hasher, reflect.Indirect(reflect.ValueOf(obj)))
	return fmt.Sprint(hasher.Sum32())
}

// isEffectivelyZero returns true if a field is zero-valued or is an empty
// slice/map. reflect.IsZero treats nil slices as zero but non-nil empty
// slices ([]T{}) as non-zero; we treat both as zero so that the digest
// is not affected by the distinction.
func isEffectivelyZero(fv reflect.Value) bool {
	if fv.IsZero() {
		return true
	}
	k := fv.Kind()
	return (k == reflect.Slice || k == reflect.Map) && fv.Len() == 0
}

// hashNonZeroFields hashes non-zero struct fields in alphabetical order by
// field name, so the digest is independent of field declaration order.
// Embedded (anonymous) structs are flattened into the same sorted pool.
func hashNonZeroFields(h hash.Hash32, v reflect.Value) {
	fields := reflect.VisibleFields(v.Type())
	sort.Slice(fields, func(a, b int) bool {
		return fields[a].Name < fields[b].Name
	})
	for _, f := range fields {
		if f.Anonymous {
			continue
		}
		fv := v.FieldByIndex(f.Index)
		if !isEffectivelyZero(fv) {
			fmt.Fprintf(h, "%s:", f.Name)
			spewPrinter.Fprintf(h, "%#v", fv.Interface())
		}
	}
}

func GetStringHash(s string) string {
	hasher := fnv.New32a()
	if _, err := hasher.Write([]byte(s)); err != nil {
		panic(err)
	}
	return rand.SafeEncodeString(fmt.Sprint(hasher.Sum32()))
}
