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

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/NVIDIA/gpu-operator/internal/utils"
)

type templateData struct {
	Foo string
	Bar string
	Baz string
}

func checkRenderedUnstructured(objs []*unstructured.Unstructured, t *templateData) {
	for idx, obj := range objs {
		Expect(obj.GetKind()).To(Equal(fmt.Sprint("TestObj", idx+1)))
		Expect(obj.Object["metadata"].(map[string]interface{})["name"].(string)).To(Equal(t.Foo))
		Expect(obj.Object["spec"].(map[string]interface{})["attribute"].(string)).To(Equal(t.Bar))
		Expect(obj.Object["spec"].(map[string]interface{})["anotherAttribute"].(string)).To(Equal(t.Baz))
	}
}

func getFilesFromDir(dirPath string) []string {
	files, err := utils.GetFilesWithSuffix(dirPath, "json", "yaml", "yml")
	if err != nil {
		panic(err)
	}
	return files
}

// writeTempManifest writes the provided template content to a file in a
// per-spec temporary directory and returns its path.
func writeTempManifest(name, content string) string {
	dir := GinkgoT().TempDir()
	f := filepath.Join(dir, name)
	Expect(os.WriteFile(f, []byte(content), 0o600)).To(Succeed())
	return f
}

var _ = Describe("Test Renderer via API", func() {
	t := &TemplatingData{
		Funcs: nil,
		Data:  &templateData{"foo", "bar", "baz"},
	}

	cwd, err := os.Getwd()
	if err != nil {
		panic(fmt.Sprintf("Failed to get CWD: %v", err))
	}
	manifestsTestDir := filepath.Join(cwd, "testdata")

	Context("Render objects without files", func() {
		It("Should return no objects", func() {
			r := NewRenderer([]string{})
			objs, err := r.RenderObjects(t)
			Expect(err).ToNot(HaveOccurred())
			Expect(objs).To(BeEmpty())
		})
	})

	Context("Render objects from non-existent files", func() {
		It("Should fail", func() {
			r := NewRenderer([]string{filepath.Join(manifestsTestDir, "doesNotExist.yaml")})
			objs, err := r.RenderObjects(t)
			Expect(err).To(HaveOccurred())
			Expect(objs).To(BeNil())
		})
	})

	Context("Render objects from mal formatted files", func() {
		It("Should fail", func() {
			files := getFilesFromDir(filepath.Join(manifestsTestDir, "badManifests"))
			for _, file := range files {
				r := NewRenderer([]string{file})
				objs, err := r.RenderObjects(t)
				Expect(err).To(HaveOccurred())
				Expect(objs).To(BeNil())
			}
		})
	})

	Context("Render objects from template with invalid template data", func() {
		It("Should fail", func() {
			r := NewRenderer(getFilesFromDir(filepath.Join(manifestsTestDir, "invalidManifests")))
			objs, err := r.RenderObjects(t)
			Expect(err).To(HaveOccurred())
			Expect(objs).To(BeNil())
		})
	})

	Context("Render objects from valid manifests dir", func() {
		It("Should return objects in order as appear in the directory lexicographically", func() {
			r := NewRenderer(getFilesFromDir(filepath.Join(manifestsTestDir, "manifests")))
			objs, err := r.RenderObjects(t)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(objs)).To(Equal(3))
			checkRenderedUnstructured(objs, t.Data.(*templateData))
		})
	})

	Context("Render objects from valid manifests dir with mixed file suffixes", func() {
		It("Should return objects in order as appear in the directory lexicographically", func() {
			r := NewRenderer(getFilesFromDir(filepath.Join(manifestsTestDir, "mixedManifests")))
			objs, err := r.RenderObjects(t)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(objs)).To(Equal(3))
			checkRenderedUnstructured(objs, t.Data.(*templateData))
		})
	})
})

var _ = Describe("Test Renderer builtin template functions and edge cases", func() {
	cwd, err := os.Getwd()
	if err != nil {
		panic(fmt.Sprintf("Failed to get CWD: %v", err))
	}
	manifestsTestDir := filepath.Join(cwd, "testdata")

	Context("Render template using the builtin 'yaml' function", func() {
		It("Should marshal the provided data structure to yaml", func() {
			content := strings.Join([]string{
				"apiVersion: v1",
				"kind: TestObjYaml",
				"metadata:",
				"  name: myname",
				"spec:",
				"{{ .Spec | yaml | indent 2 }}",
			}, "\n")
			file := writeTempManifest("yamlFunc.yaml", content)

			data := &TemplatingData{
				Data: struct {
					Spec map[string]interface{}
				}{
					Spec: map[string]interface{}{"attribute": "value"},
				},
			}

			r := NewRenderer([]string{file})
			objs, err := r.RenderObjects(data)
			Expect(err).ToNot(HaveOccurred())
			Expect(objs).To(HaveLen(1))
			Expect(objs[0].GetKind()).To(Equal("TestObjYaml"))
			spec := objs[0].Object["spec"].(map[string]interface{})
			Expect(spec["attribute"]).To(Equal("value"))
		})
	})

	Context("Render template using the builtin 'deref' function", func() {
		renderDeref := func(ptr *bool) []map[string]interface{} {
			content := strings.Join([]string{
				"apiVersion: v1",
				"kind: TestObjDeref",
				"metadata:",
				"  name: deref",
				"spec:",
				"  enabled: {{ deref .Ptr }}",
			}, "\n")
			file := writeTempManifest("derefFunc.yaml", content)

			data := &TemplatingData{
				Data: struct{ Ptr *bool }{Ptr: ptr},
			}
			r := NewRenderer([]string{file})
			objs, err := r.RenderObjects(data)
			Expect(err).ToNot(HaveOccurred())
			Expect(objs).To(HaveLen(1))
			specs := make([]map[string]interface{}, 0, len(objs))
			for _, o := range objs {
				specs = append(specs, o.Object["spec"].(map[string]interface{}))
			}
			return specs
		}

		It("Should return false for a nil pointer", func() {
			specs := renderDeref(nil)
			Expect(specs[0]["enabled"]).To(BeFalse())
		})

		It("Should return the dereferenced value for a non-nil pointer", func() {
			trueVal := true
			specs := renderDeref(&trueVal)
			Expect(specs[0]["enabled"]).To(BeTrue())
		})
	})

	Context("Render template using additional user-provided Funcs", func() {
		It("Should apply the custom function during rendering", func() {
			content := strings.Join([]string{
				"apiVersion: v1",
				"kind: TestObjFunc",
				"metadata:",
				"  name: {{ myUpper .Name }}",
			}, "\n")
			file := writeTempManifest("customFunc.yaml", content)

			data := &TemplatingData{
				Funcs: template.FuncMap{
					"myUpper": strings.ToUpper,
				},
				Data: struct{ Name string }{Name: "lowered"},
			}
			r := NewRenderer([]string{file})
			objs, err := r.RenderObjects(data)
			Expect(err).ToNot(HaveOccurred())
			Expect(objs).To(HaveLen(1))
			name := objs[0].Object["metadata"].(map[string]interface{})["name"].(string)
			Expect(name).To(Equal("LOWERED"))
		})
	})

	Context("Render template that fails to parse", func() {
		It("Should fail with a parse error", func() {
			// Unclosed template action triggers a parse-time error.
			file := writeTempManifest("parseError.yaml", "kind: {{ .Foo")

			data := &TemplatingData{
				Data: struct{ Foo string }{Foo: "bar"},
			}
			r := NewRenderer([]string{file})
			objs, err := r.RenderObjects(data)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to parse manifest file"))
			Expect(objs).To(BeNil())
		})
	})

	Context("Render manifests that produce no objects", func() {
		It("Should skip files whose rendered content is entirely whitespace", func() {
			// The rendered output is whitespace only and deliberately contains a
			// tab. Without renderFile's explicit whitespace-skip, this content
			// would reach the YAML decoder and fail ("character that cannot start
			// any token"), so this spec genuinely exercises the skip branch and
			// would fail if it were removed.
			file := writeTempManifest("whitespace.yaml", "{{/* renders nothing */}}\n \t \n")
			data := &TemplatingData{Data: struct{}{}}
			r := NewRenderer([]string{file})
			objs, err := r.RenderObjects(data)
			Expect(err).ToNot(HaveOccurred())
			Expect(objs).To(BeEmpty())
		})

		It("Should skip an empty file", func() {
			file := filepath.Join(manifestsTestDir, "emptyManifests", "emptyTemplate.yaml")
			data := &TemplatingData{Data: struct{}{}}
			r := NewRenderer([]string{file})
			objs, err := r.RenderObjects(data)
			Expect(err).ToNot(HaveOccurred())
			Expect(objs).To(BeEmpty())
		})

		It("Should skip documents that decode to an object without a kind", func() {
			file := filepath.Join(manifestsTestDir, "emptyManifests", "zeroObj.yaml")
			data := &TemplatingData{Data: struct{}{}}
			r := NewRenderer([]string{file})
			objs, err := r.RenderObjects(data)
			Expect(err).ToNot(HaveOccurred())
			Expect(objs).To(BeEmpty())
		})
	})
})
