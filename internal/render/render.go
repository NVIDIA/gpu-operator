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

package render

/*
 Render package renders k8s API objects from a given set of template .yaml files
 provided in a source directory and a RenderData struct to be used in the rendering process

 The objects are rendered in `Unstructured` format provided by
 k8s.io/apimachinery/pkg/apis/meta/v1/unstructured package.
*/

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	yamlDecoder "k8s.io/apimachinery/pkg/util/yaml"
	yamlConverter "sigs.k8s.io/yaml"
)

const (
	maxBufSizeForYamlDecode = 4096
)

var ManifestFileSuffix = []string{"yaml", "yml", "json"}

// Renderer renders k8s objects from a manifest source dir and TemplatingData used by the templating engine
type Renderer interface {
	// RenderObjects renders kubernetes objects using provided TemplatingData
	RenderObjects(data *TemplatingData) ([]*unstructured.Unstructured, error)
}

// TemplatingData is used by the templating engine to render templates
type TemplatingData struct {
	// Funcs are additional Functions used during the templating process
	Funcs template.FuncMap
	// Data used for the rendering process
	Data interface{}
}

// NewRenderer creates a Renderer object, that will render all template files provided.
// file format needs to be either json or yaml.
func NewRenderer(files []string) Renderer {
	return &textTemplateRenderer{
		files: files,
	}
}

// textTemplateRenderer is an implementation of the Renderer interface using golang builtin text/template package
// as its templating engine
type textTemplateRenderer struct {
	files []string
}

// RenderObjects renders kubernetes objects utilizing the provided TemplatingData.
func (r *textTemplateRenderer) RenderObjects(data *TemplatingData) ([]*unstructured.Unstructured, error) {
	var objs []*unstructured.Unstructured

	for _, file := range r.files {
		out, err := r.renderFile(file, data)
		if err != nil {
			return nil, fmt.Errorf("error rendering file %s: %w", file, err)
		}
		objs = append(objs, out...)
	}
	return objs, nil
}

// renderFile renders a single file to a list of k8s unstructured objects
func (r *textTemplateRenderer) renderFile(filePath string, data *TemplatingData) ([]*unstructured.Unstructured, error) {
	// Read file
	txt, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest file %s: %w", filePath, err)
	}

	// Create a new template
	tmpl := template.New(path.Base(filePath)).Funcs(sprig.FuncMap()).Option("missingkey=error")

	tmpl.Funcs(template.FuncMap{
		"yaml": func(obj interface{}) (string, error) {
			yamlBytes, err := yamlConverter.Marshal(obj)
			return string(yamlBytes), err
		},
		"deref": func(b *bool) bool {
			if b == nil {
				return false
			}
			return *b
		},
	})

	if data.Funcs != nil {
		tmpl.Funcs(data.Funcs)
	}

	if _, err := tmpl.Parse(string(txt)); err != nil {
		return nil, fmt.Errorf("failed to parse manifest file %s: %w", filePath, err)
	}
	rendered := bytes.Buffer{}

	if err := tmpl.Execute(&rendered, data.Data); err != nil {
		return nil, fmt.Errorf("failed to render manifest %s: %w", filePath, err)
	}

	out := []*unstructured.Unstructured{}

	// special case - if the entire file is whitespace, skip
	if strings.TrimSpace(rendered.String()) == "" {
		return out, nil
	}

	decoder := yamlDecoder.NewYAMLOrJSONDecoder(&rendered, maxBufSizeForYamlDecode)
	for {
		u := unstructured.Unstructured{}
		if err := decoder.Decode(&u); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("failed to unmarshal manifest %s: %w", filePath, err)
		}
		// Ensure object is not empty by checking the object kind
		if u.GetKind() == "" {
			continue
		}
		out = append(out, &u)
	}

	return out, nil
}
