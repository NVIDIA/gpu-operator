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

package csv

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

const malformedImageRef = "@@bad::ref"

func TestValidateImage_InvalidReference(t *testing.T) {
	testCases := []struct {
		description string
		imagePath   string
	}{
		{
			description: "empty reference",
			imagePath:   "",
		},
		{
			description: "malformed reference",
			imagePath:   malformedImageRef,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			err := validateImage(context.Background(), tc.imagePath)
			require.ErrorContains(t, err, "failed to construct an image reference")
		})
	}
}

func TestValidateImages_InvalidRelatedImage(t *testing.T) {
	operatorImage := newOCILayoutRef(t, "operator") + ":operator"

	csv := newCSVWithOperatorContainer(operatorImage, nil)
	csv.Spec.RelatedImages = []v1alpha1.RelatedImage{
		{Name: "bad-image", Image: malformedImageRef},
	}

	err := validateImages(context.Background(), csv)
	require.ErrorContains(t, err, "failed to validate image bad-image")
	require.ErrorContains(t, err, "failed to construct an image reference")
}

func newOCILayoutRef(t *testing.T, tags ...string) string {
	t.Helper()

	layoutDir := filepath.Join(t.TempDir(), "layout")
	require.NoError(t, os.MkdirAll(filepath.Join(layoutDir, "blobs", "sha256"), 0o700))

	manifest := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"sha256:0000000000000000000000000000000000000000000000000000000000000000","size":0},"layers":[]}`)
	manifestDigest := fmt.Sprintf("%x", sha256.Sum256(manifest))

	descriptors := make([]map[string]any, 0, len(tags))
	for _, tag := range tags {
		descriptors = append(descriptors, map[string]any{
			"mediaType":   "application/vnd.oci.image.manifest.v1+json",
			"digest":      "sha256:" + manifestDigest,
			"size":        len(manifest),
			"annotations": map[string]string{"org.opencontainers.image.ref.name": tag},
		})
	}
	imageIndex, err := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.index.v1+json",
		"manifests":     descriptors,
	})
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(layoutDir, "oci-layout"), []byte(`{"imageLayoutVersion":"1.0.0"}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(layoutDir, "index.json"), imageIndex, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(layoutDir, "blobs", "sha256", manifestDigest), manifest, 0o600))

	return "ocidir://" + layoutDir
}

func newCSVWithOperatorContainer(operatorImage string, envVars []corev1.EnvVar) *v1alpha1.ClusterServiceVersion {
	csv := &v1alpha1.ClusterServiceVersion{}
	csv.Spec.InstallStrategy.StrategySpec.DeploymentSpecs = []v1alpha1.StrategyDeploymentSpec{
		{
			Name: "gpu-operator",
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "gpu-operator",
								Image: operatorImage,
								Env:   envVars,
							},
						},
					},
				},
			},
		},
	}
	return csv
}

func TestValidateImages_AllImagesResolve(t *testing.T) {
	firstRelatedImage := newOCILayoutRef(t, "related-one") + ":related-one"
	secondRelatedImage := newOCILayoutRef(t, "related-two") + ":related-two"
	operatorImage := newOCILayoutRef(t, "operator") + ":operator"
	driverImage := newOCILayoutRef(t, "driver") + ":driver"
	toolkitImage := newOCILayoutRef(t, "toolkit") + ":toolkit"

	csv := newCSVWithOperatorContainer(operatorImage, []corev1.EnvVar{
		{Name: "DRIVER_IMAGE", Value: driverImage},
		{Name: "TOOLKIT_IMAGE", Value: toolkitImage},
		{Name: "DRIVER_VERSION", Value: malformedImageRef},
	})
	csv.Spec.RelatedImages = []v1alpha1.RelatedImage{
		{Name: "related-one", Image: firstRelatedImage},
		{Name: "related-two", Image: secondRelatedImage},
	}

	err := validateImages(context.Background(), csv)
	require.NoError(t, err)
}

func TestValidateImages_UnresolvableRelatedImageAfterAResolvableOne(t *testing.T) {
	operatorImage := newOCILayoutRef(t, "operator") + ":operator"
	firstRelatedImage := newOCILayoutRef(t, "related-one") + ":related-one"

	csv := newCSVWithOperatorContainer(operatorImage, nil)
	csv.Spec.RelatedImages = []v1alpha1.RelatedImage{
		{Name: "related-one", Image: firstRelatedImage},
		{Name: "related-two", Image: malformedImageRef},
	}

	err := validateImages(context.Background(), csv)
	require.ErrorContains(t, err, "failed to validate image related-two")
}

func TestValidateImages_UnresolvableOperatorImage(t *testing.T) {
	relatedImage := newOCILayoutRef(t, "related") + ":related"

	csv := newCSVWithOperatorContainer(malformedImageRef, nil)
	csv.Spec.RelatedImages = []v1alpha1.RelatedImage{
		{Name: "gpu-operator-image", Image: relatedImage},
	}

	err := validateImages(context.Background(), csv)
	require.ErrorContains(t, err, "failed to validate image "+malformedImageRef)
}

func TestValidateImages_SkipsEnvVarsWithoutImageSuffix(t *testing.T) {
	operatorImage := newOCILayoutRef(t, "operator") + ":operator"

	csv := newCSVWithOperatorContainer(operatorImage, []corev1.EnvVar{
		{Name: "DRIVER_VERSION", Value: malformedImageRef},
		{Name: "DRIVER_IMAGE-580", Value: malformedImageRef},
	})

	err := validateImages(context.Background(), csv)
	require.NoError(t, err)
}

func TestValidateImages_InvalidOperandImageEnvVar(t *testing.T) {
	operatorImage := newOCILayoutRef(t, "operator") + ":operator"
	driverImage := newOCILayoutRef(t, "driver") + ":driver"

	testCases := []struct {
		description          string
		envVars              []corev1.EnvVar
		expectedErrorMessage string
	}{
		{
			description: "an unresolvable _IMAGE env var follows a skipped one",
			envVars: []corev1.EnvVar{
				{Name: "DRIVER_VERSION", Value: malformedImageRef},
				{Name: "DRIVER_IMAGE", Value: malformedImageRef},
			},
			expectedErrorMessage: "failed to validate image DRIVER_IMAGE",
		},
		{
			description: "a later, differently named _IMAGE env var is unresolvable",
			envVars: []corev1.EnvVar{
				{Name: "DRIVER_IMAGE", Value: driverImage},
				{Name: "TOOLKIT_IMAGE", Value: malformedImageRef},
			},
			expectedErrorMessage: "failed to validate image TOOLKIT_IMAGE",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			csv := newCSVWithOperatorContainer(operatorImage, tc.envVars)

			err := validateImages(context.Background(), csv)
			require.ErrorContains(t, err, tc.expectedErrorMessage)
		})
	}
}

func TestValidateImage_TagResolution(t *testing.T) {
	layoutRef := newOCILayoutRef(t, "v1")

	testCases := []struct {
		description          string
		imagePath            string
		expectedErrorMessage string
	}{
		{
			description: "tag present in the layout",
			imagePath:   layoutRef + ":v1",
		},
		{
			description:          "tag absent from the layout",
			imagePath:            layoutRef + ":v2",
			expectedErrorMessage: "failed to get image manifest",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			err := validateImage(context.Background(), tc.imagePath)
			if tc.expectedErrorMessage == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.expectedErrorMessage)
		})
	}
}
