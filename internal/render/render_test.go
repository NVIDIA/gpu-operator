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

package render_test

import (
	"fmt"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/NVIDIA/gpu-operator/internal/render"
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

var _ = Describe("Test Renderer via API", func() {
	t := &render.TemplatingData{
		Funcs: nil,
		Data:  &templateData{"foo", "bar", "baz"},
	}

	cwd, err := os.Getwd()
	if err != nil {
		panic("Failed to get CWD")
	}
	manifestsTestDir := filepath.Join(cwd, "testdata")

	Context("Render objects without files", func() {
		It("Should return no objects", func() {
			r := render.NewRenderer([]string{})
			objs, err := r.RenderObjects(t)
			Expect(err).ToNot(HaveOccurred())
			Expect(objs).To(BeEmpty())
		})
	})

	Context("Render objects from non-existent files", func() {
		It("Should fail", func() {
			r := render.NewRenderer([]string{filepath.Join(manifestsTestDir, "doesNotExist.yaml")})
			objs, err := r.RenderObjects(t)
			Expect(err).To(HaveOccurred())
			Expect(objs).To(BeNil())
		})
	})

	Context("Render objects from mal formatted files", func() {
		It("Should fail", func() {
			files := getFilesFromDir(filepath.Join(manifestsTestDir, "badManifests"))
			for _, file := range files {
				r := render.NewRenderer([]string{file})
				objs, err := r.RenderObjects(t)
				Expect(err).To(HaveOccurred())
				Expect(objs).To(BeNil())
			}
		})
	})

	Context("Render objects from template with invalid template data", func() {
		It("Should fail", func() {
			r := render.NewRenderer(getFilesFromDir(filepath.Join(manifestsTestDir, "invalidManifests")))
			objs, err := r.RenderObjects(t)
			Expect(err).To(HaveOccurred())
			Expect(objs).To(BeNil())
		})
	})

	Context("Render objects from valid manifests dir", func() {
		It("Should return objects in order as appear in the directory lexicographically", func() {
			r := render.NewRenderer(getFilesFromDir(filepath.Join(manifestsTestDir, "manifests")))
			objs, err := r.RenderObjects(t)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(objs)).To(Equal(3))
			checkRenderedUnstructured(objs, t.Data.(*templateData))
		})
	})

	Context("Render objects from valid manifests dir with mixed file suffixes", func() {
		It("Should return objects in order as appear in the directory lexicographically", func() {
			r := render.NewRenderer(getFilesFromDir(filepath.Join(manifestsTestDir, "mixedManifests")))
			objs, err := r.RenderObjects(t)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(objs)).To(Equal(3))
			checkRenderedUnstructured(objs, t.Data.(*templateData))
		})
	})
})
