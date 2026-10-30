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

package clusterpolicy

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	v1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
)

const osTagSuffix = "-ubuntu22.04"

const malformedImageRef = "@@bad::ref"

const absentTagSuffix = "-absent"

const (
	driverTag              = "driver"
	toolkitTag             = "toolkit"
	devicePluginTag        = "device-plugin"
	dcgmExporterTag        = "dcgm-exporter"
	dcgmTag                = "dcgm-hostengine"
	gpuFeatureDiscoveryTag = "gfd"
	migManagerTag          = "mig-manager"
	gpuDirectStorageTag    = "gds"
	vfioManagerTag         = "vfio-manager"
	sandboxDevicePluginTag = "sandbox-plugin"
	vgpuDeviceManagerTag   = "vgpu-device-manager"
)

func clearImagePathEnvVars(t *testing.T) {
	t.Helper()

	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasSuffix(name, "_IMAGE") {
			t.Setenv(name, "")
		}
	}
}

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

func TestValidateImages_EmptyDriverSpecImagePathError(t *testing.T) {
	clearImagePathEnvVars(t)

	spec := &v1.ClusterPolicySpec{}

	err := validateImages(context.Background(), spec)
	require.ErrorContains(t, err, "failed to construct the image path")
	require.ErrorContains(t, err, "DRIVER_IMAGE")
}

func TestValidateImages_InvalidDriverImageRefError(t *testing.T) {
	spec := &v1.ClusterPolicySpec{}
	spec.Driver.Image = malformedImageRef

	err := validateImages(context.Background(), spec)
	require.ErrorContains(t, err, "failed to validate image")
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

func newClusterPolicySpecWithResolvableImages(t *testing.T) *v1.ClusterPolicySpec {
	t.Helper()

	layoutRef := newOCILayoutRef(t,
		driverTag+osTagSuffix,
		toolkitTag,
		devicePluginTag,
		dcgmExporterTag,
		dcgmTag,
		gpuFeatureDiscoveryTag,
		migManagerTag,
		gpuDirectStorageTag+osTagSuffix,
		vfioManagerTag,
		sandboxDevicePluginTag,
		vgpuDeviceManagerTag,
	)

	return &v1.ClusterPolicySpec{
		Driver:              v1.DriverSpec{Image: layoutRef + ":" + driverTag},
		Toolkit:             v1.ToolkitSpec{Image: layoutRef + ":" + toolkitTag},
		DevicePlugin:        v1.DevicePluginSpec{Image: layoutRef + ":" + devicePluginTag},
		DCGMExporter:        v1.DCGMExporterSpec{Image: layoutRef + ":" + dcgmExporterTag},
		DCGM:                v1.DCGMSpec{Image: layoutRef + ":" + dcgmTag},
		GPUFeatureDiscovery: v1.GPUFeatureDiscoverySpec{Image: layoutRef + ":" + gpuFeatureDiscoveryTag},
		MIGManager:          v1.MIGManagerSpec{Image: layoutRef + ":" + migManagerTag},
		GPUDirectStorage:    &v1.GPUDirectStorageSpec{Image: layoutRef + ":" + gpuDirectStorageTag},
		VFIOManager:         v1.VFIOManagerSpec{Image: layoutRef + ":" + vfioManagerTag},
		SandboxDevicePlugin: v1.SandboxDevicePluginSpec{Image: layoutRef + ":" + sandboxDevicePluginTag},
		VGPUDeviceManager:   v1.VGPUDeviceManagerSpec{Image: layoutRef + ":" + vgpuDeviceManagerTag},
	}
}

func TestValidateImages_Success(t *testing.T) {
	spec := newClusterPolicySpecWithResolvableImages(t)

	require.NoError(t, validateImages(context.Background(), spec))
}

func TestValidateImages_EachOperandIsValidated(t *testing.T) {
	testCases := []struct {
		description       string
		breakOperandImage func(spec *v1.ClusterPolicySpec)
		expectedTag       string
	}{
		{
			description:       "Driver",
			breakOperandImage: func(spec *v1.ClusterPolicySpec) { spec.Driver.Image += absentTagSuffix },
			expectedTag:       driverTag + absentTagSuffix + osTagSuffix,
		},
		{
			description:       "Toolkit",
			breakOperandImage: func(spec *v1.ClusterPolicySpec) { spec.Toolkit.Image += absentTagSuffix },
			expectedTag:       toolkitTag + absentTagSuffix,
		},
		{
			description:       "DevicePlugin",
			breakOperandImage: func(spec *v1.ClusterPolicySpec) { spec.DevicePlugin.Image += absentTagSuffix },
			expectedTag:       devicePluginTag + absentTagSuffix,
		},
		{
			description:       "DCGMExporter",
			breakOperandImage: func(spec *v1.ClusterPolicySpec) { spec.DCGMExporter.Image += absentTagSuffix },
			expectedTag:       dcgmExporterTag + absentTagSuffix,
		},
		{
			description:       "DCGM",
			breakOperandImage: func(spec *v1.ClusterPolicySpec) { spec.DCGM.Image += absentTagSuffix },
			expectedTag:       dcgmTag + absentTagSuffix,
		},
		{
			description:       "GPUFeatureDiscovery",
			breakOperandImage: func(spec *v1.ClusterPolicySpec) { spec.GPUFeatureDiscovery.Image += absentTagSuffix },
			expectedTag:       gpuFeatureDiscoveryTag + absentTagSuffix,
		},
		{
			description:       "MIGManager",
			breakOperandImage: func(spec *v1.ClusterPolicySpec) { spec.MIGManager.Image += absentTagSuffix },
			expectedTag:       migManagerTag + absentTagSuffix,
		},
		{
			description:       "GPUDirectStorage",
			breakOperandImage: func(spec *v1.ClusterPolicySpec) { spec.GPUDirectStorage.Image += absentTagSuffix },
			expectedTag:       gpuDirectStorageTag + absentTagSuffix + osTagSuffix,
		},
		{
			description:       "VFIOManager",
			breakOperandImage: func(spec *v1.ClusterPolicySpec) { spec.VFIOManager.Image += absentTagSuffix },
			expectedTag:       vfioManagerTag + absentTagSuffix,
		},
		{
			description:       "SandboxDevicePlugin",
			breakOperandImage: func(spec *v1.ClusterPolicySpec) { spec.SandboxDevicePlugin.Image += absentTagSuffix },
			expectedTag:       sandboxDevicePluginTag + absentTagSuffix,
		},
		{
			description:       "VGPUDeviceManager",
			breakOperandImage: func(spec *v1.ClusterPolicySpec) { spec.VGPUDeviceManager.Image += absentTagSuffix },
			expectedTag:       vgpuDeviceManagerTag + absentTagSuffix,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			spec := newClusterPolicySpecWithResolvableImages(t)
			tc.breakOperandImage(spec)

			err := validateImages(context.Background(), spec)
			require.ErrorContains(t, err, "failed to get image manifest")
			require.ErrorContains(t, err, tc.expectedTag)
		})
	}
}

func TestValidateImages_EachOperandImagePathError(t *testing.T) {
	testCases := []struct {
		description        string
		blankOperandImage  func(spec *v1.ClusterPolicySpec)
		expectedEnvVarName string
	}{
		{
			description:        "Driver",
			blankOperandImage:  func(spec *v1.ClusterPolicySpec) { spec.Driver.Image = "" },
			expectedEnvVarName: "DRIVER_IMAGE",
		},
		{
			description:        "Toolkit",
			blankOperandImage:  func(spec *v1.ClusterPolicySpec) { spec.Toolkit.Image = "" },
			expectedEnvVarName: "CONTAINER_TOOLKIT_IMAGE",
		},
		{
			description:        "DevicePlugin",
			blankOperandImage:  func(spec *v1.ClusterPolicySpec) { spec.DevicePlugin.Image = "" },
			expectedEnvVarName: "DEVICE_PLUGIN_IMAGE",
		},
		{
			description:        "DCGMExporter",
			blankOperandImage:  func(spec *v1.ClusterPolicySpec) { spec.DCGMExporter.Image = "" },
			expectedEnvVarName: "DCGM_EXPORTER_IMAGE",
		},
		{
			description:        "DCGM",
			blankOperandImage:  func(spec *v1.ClusterPolicySpec) { spec.DCGM.Image = "" },
			expectedEnvVarName: "DCGM_IMAGE",
		},
		{
			description:        "GPUFeatureDiscovery",
			blankOperandImage:  func(spec *v1.ClusterPolicySpec) { spec.GPUFeatureDiscovery.Image = "" },
			expectedEnvVarName: "GFD_IMAGE",
		},
		{
			description:        "MIGManager",
			blankOperandImage:  func(spec *v1.ClusterPolicySpec) { spec.MIGManager.Image = "" },
			expectedEnvVarName: "MIG_MANAGER_IMAGE",
		},
		{
			description:        "GPUDirectStorage",
			blankOperandImage:  func(spec *v1.ClusterPolicySpec) { spec.GPUDirectStorage.Image = "" },
			expectedEnvVarName: "GDS_IMAGE",
		},
		{
			description:        "VFIOManager",
			blankOperandImage:  func(spec *v1.ClusterPolicySpec) { spec.VFIOManager.Image = "" },
			expectedEnvVarName: "VFIO_MANAGER_IMAGE",
		},
		{
			description:        "SandboxDevicePlugin",
			blankOperandImage:  func(spec *v1.ClusterPolicySpec) { spec.SandboxDevicePlugin.Image = "" },
			expectedEnvVarName: "SANDBOX_DEVICE_PLUGIN_IMAGE",
		},
		{
			description:        "VGPUDeviceManager",
			blankOperandImage:  func(spec *v1.ClusterPolicySpec) { spec.VGPUDeviceManager.Image = "" },
			expectedEnvVarName: "VGPU_DEVICE_MANAGER_IMAGE",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			clearImagePathEnvVars(t)

			spec := newClusterPolicySpecWithResolvableImages(t)
			tc.blankOperandImage(spec)

			err := validateImages(context.Background(), spec)
			require.ErrorContains(t, err, "failed to construct the image path")
			require.ErrorContains(t, err, tc.expectedEnvVarName)
		})
	}
}

func TestValidateImages_DriverImageOSTagAppended(t *testing.T) {
	driverLayoutRef := newOCILayoutRef(t, driverTag)
	spec := &v1.ClusterPolicySpec{Driver: v1.DriverSpec{Image: driverLayoutRef + ":" + driverTag}}

	err := validateImages(context.Background(), spec)
	require.ErrorContains(t, err, "failed to get image manifest")
	require.ErrorContains(t, err, driverLayoutRef+":"+driverTag+osTagSuffix+":")
}

func TestValidateImages_GPUDirectStorageImageOSTagAppended(t *testing.T) {
	spec := newClusterPolicySpecWithResolvableImages(t)
	gpuDirectStorageLayoutRef := newOCILayoutRef(t, gpuDirectStorageTag)
	spec.GPUDirectStorage = &v1.GPUDirectStorageSpec{Image: gpuDirectStorageLayoutRef + ":" + gpuDirectStorageTag}

	err := validateImages(context.Background(), spec)
	require.ErrorContains(t, err, "failed to get image manifest")
	require.ErrorContains(t, err, gpuDirectStorageLayoutRef+":"+gpuDirectStorageTag+osTagSuffix+":")
}

func TestValidateImages_GPUDirectStorage(t *testing.T) {
	testCases := []struct {
		description           string
		gpuDirectStorageSpec  *v1.GPUDirectStorageSpec
		expectedErrorMessages []string
	}{
		{
			description:          "omitted from the spec",
			gpuDirectStorageSpec: nil,
		},
		{
			description:           "image set in neither the spec nor the environment",
			gpuDirectStorageSpec:  &v1.GPUDirectStorageSpec{},
			expectedErrorMessages: []string{"failed to construct the image path", "GDS_IMAGE"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			clearImagePathEnvVars(t)

			spec := newClusterPolicySpecWithResolvableImages(t)
			spec.GPUDirectStorage = tc.gpuDirectStorageSpec

			err := validateImages(context.Background(), spec)
			if len(tc.expectedErrorMessages) == 0 {
				require.NoError(t, err)
				return
			}
			for _, expectedErrorMessage := range tc.expectedErrorMessages {
				require.ErrorContains(t, err, expectedErrorMessage)
			}
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

func TestValidateImage_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := validateImage(ctx, "127.0.0.1:1/nvidia/gpu-operator:v1")
	require.ErrorContains(t, err, "failed to get image manifest")
	require.ErrorContains(t, err, context.Canceled.Error())
}
