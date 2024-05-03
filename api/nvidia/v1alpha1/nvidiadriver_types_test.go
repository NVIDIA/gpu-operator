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

package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	testDigest = "10d1df8034373061366d4fb17b364b3b28d766b54d5a0b700c1a5a75378cf125"
)

func TestGetImagePath(t *testing.T) {
	testCases := []struct {
		description   string
		spec          *NVIDIADriverSpec
		osVersion     string
		errorExpected bool
		expectedImage string
	}{
		{
			description: "malformed repository",
			spec: &NVIDIADriverSpec{
				Repository: "malformed?/repo",
			},
			errorExpected: true,
			expectedImage: "",
		},
		{
			description: "malformed image",
			spec: &NVIDIADriverSpec{
				Image: "malformed?image",
			},
			errorExpected: true,
			expectedImage: "",
		},
		{
			description: "only image provided with no tag or digest",
			spec: &NVIDIADriverSpec{
				Image: "nvcr.io/nvidia/driver",
			},
			errorExpected: true,
			expectedImage: "",
		},
		{
			description: "only image provided with tag",
			spec: &NVIDIADriverSpec{
				Image: "nvcr.io/nvidia/driver:525.85.03",
			},
			osVersion:     "ubuntu22.04",
			expectedImage: "nvcr.io/nvidia/driver:525.85.03-ubuntu22.04",
		},
		{
			description: "only image provided with digest",
			spec: &NVIDIADriverSpec{
				Image: "nvcr.io/nvidia/driver@sha256:" + testDigest,
			},
			osVersion:     "ubuntu22.04",
			expectedImage: "nvcr.io/nvidia/driver@sha256:" + testDigest,
		},
		{
			description: "repository, image, and version set but image contains a tag",
			spec: &NVIDIADriverSpec{
				Repository: "nvcr.io/nvidia",
				Image:      "nvcr.io/nvidia/driver:525.85.03",
				Version:    "535.104.05",
			},
			osVersion:     "ubuntu22.04",
			errorExpected: true,
			expectedImage: "",
		},
		{
			description: "repository, image, and version set but image contains a digest",
			spec: &NVIDIADriverSpec{
				Repository: "nvcr.io/nvidia",
				Image:      "nvcr.io/nvidia/driver@sha256:" + testDigest,
				Version:    "535.104.05",
			},
			osVersion:     "ubuntu22.04",
			errorExpected: true,
			expectedImage: "",
		},
		{
			description: "missing version",
			spec: &NVIDIADriverSpec{
				Repository: "nvcr.io/nvidia",
				Image:      "driver",
			},
			osVersion:     "ubuntu22.04",
			errorExpected: true,
			expectedImage: "",
		},
		{
			description: "repository, image, and version set; version is a tag",
			spec: &NVIDIADriverSpec{
				Repository: "nvcr.io/nvidia",
				Image:      "driver",
				Version:    "535.104.05",
			},
			osVersion:     "ubuntu22.04",
			expectedImage: "nvcr.io/nvidia/driver:535.104.05-ubuntu22.04",
		},
		{
			description: "repository, image, and version set; version is a digest",
			spec: &NVIDIADriverSpec{
				Repository: "nvcr.io/nvidia",
				Image:      "driver",
				Version:    "sha256:" + testDigest,
			},
			osVersion:     "ubuntu22.04",
			expectedImage: "nvcr.io/nvidia/driver@sha256:" + testDigest,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			image, err := tc.spec.GetImagePath(tc.osVersion)
			if tc.errorExpected {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, image, tc.expectedImage)
		})
	}
}

func TestGetPrecompiledImagePath(t *testing.T) {
	testCases := []struct {
		description   string
		spec          *NVIDIADriverSpec
		osVersion     string
		kernelVersion string
		errorExpected bool
		expectedImage string
	}{
		{
			description: "malformed repository",
			spec: &NVIDIADriverSpec{
				Repository: "malformed?/repo",
			},
			errorExpected: true,
			expectedImage: "",
		},
		{
			description: "malformed image",
			spec: &NVIDIADriverSpec{
				Image: "malformed?image",
			},
			errorExpected: true,
			expectedImage: "",
		},
		{
			description: "only image provided with no tag or digest",
			spec: &NVIDIADriverSpec{
				Image: "nvcr.io/nvidia/driver",
			},
			errorExpected: true,
			expectedImage: "",
		},
		{
			description: "only image provided with tag",
			spec: &NVIDIADriverSpec{
				Image: "nvcr.io/nvidia/driver:525",
			},
			osVersion:     "ubuntu22.04",
			kernelVersion: "5.4.0-150-generic",
			expectedImage: "nvcr.io/nvidia/driver:525-5.4.0-150-generic-ubuntu22.04",
		},
		{
			description: "only image provided with digest",
			spec: &NVIDIADriverSpec{
				Image: "nvcr.io/nvidia/driver@sha256:" + testDigest,
			},
			osVersion:     "ubuntu22.04",
			kernelVersion: "5.4.0-150-generic",
			errorExpected: true,
			expectedImage: "",
		},
		{
			description: "repository, image, and version set but image contains a tag",
			spec: &NVIDIADriverSpec{
				Repository: "nvcr.io/nvidia",
				Image:      "nvcr.io/nvidia/driver:525.85.03",
				Version:    "535.104.05",
			},
			osVersion:     "ubuntu22.04",
			kernelVersion: "5.4.0-150-generic",
			errorExpected: true,
			expectedImage: "",
		},
		{
			description: "repository, image, and version set but image contains a digest",
			spec: &NVIDIADriverSpec{
				Repository: "nvcr.io/nvidia",
				Image:      "nvcr.io/nvidia/driver@sha256:" + testDigest,
				Version:    "535.104.05",
			},
			osVersion:     "ubuntu22.04",
			kernelVersion: "5.4.0-150-generic",
			errorExpected: true,
			expectedImage: "",
		},
		{
			description: "missing version",
			spec: &NVIDIADriverSpec{
				Repository: "nvcr.io/nvidia",
				Image:      "driver",
			},
			osVersion:     "ubuntu22.04",
			kernelVersion: "5.4.0-150-generic",
			errorExpected: true,
			expectedImage: "",
		},
		{
			description: "repository, image, and version set; version is a tag",
			spec: &NVIDIADriverSpec{
				Repository: "nvcr.io/nvidia",
				Image:      "driver",
				Version:    "535",
			},
			osVersion:     "ubuntu22.04",
			kernelVersion: "5.4.0-150-generic",
			expectedImage: "nvcr.io/nvidia/driver:535-5.4.0-150-generic-ubuntu22.04",
		},
		{
			description: "repository, image, and version set; version is a digest",
			spec: &NVIDIADriverSpec{
				Repository: "nvcr.io/nvidia",
				Image:      "driver",
				Version:    "sha256:" + testDigest,
			},
			osVersion:     "ubuntu22.04",
			kernelVersion: "5.4.0-150-generic",
			errorExpected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			image, err := tc.spec.GetPrecompiledImagePath(tc.osVersion, tc.kernelVersion)
			if tc.errorExpected {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, image, tc.expectedImage)
		})
	}
}

func TestGDSGetImagePath(t *testing.T) {
	testCases := []struct {
		description   string
		spec          *GPUDirectStorageSpec
		osVersion     string
		errorExpected bool
		expectedImage string
	}{
		{
			description: "malformed repository",
			spec: &GPUDirectStorageSpec{
				Repository: "malformed?/repo",
			},
			errorExpected: true,
			expectedImage: "",
		},
		{
			description: "malformed image",
			spec: &GPUDirectStorageSpec{
				Image: "malformed?image",
			},
			errorExpected: true,
			expectedImage: "",
		},
		{
			description: "valid image",
			spec: &GPUDirectStorageSpec{
				Repository: "nvcr.io/nvidia/cloud-native",
				Image:      "nvidia-fs",
				Version:    "2.16.1",
			},
			osVersion:     "ubuntu20.04",
			errorExpected: false,
			expectedImage: "nvcr.io/nvidia/cloud-native/nvidia-fs:2.16.1-ubuntu20.04",
		},
		{
			description: "only image provided with no tag or digest",
			spec: &GPUDirectStorageSpec{
				Image: "nvcr.io/nvidia/cloud-native",
			},
			errorExpected: true,
			expectedImage: "",
		},
		{
			description: "repository, image, and version set; version is a digest",
			spec: &GPUDirectStorageSpec{
				Repository: "nvcr.io/nvidia/cloud-native",
				Image:      "nvidia-fs",
				Version:    "sha256:" + testDigest,
			},
			osVersion:     "ubuntu22.04",
			errorExpected: false,
			expectedImage: "nvcr.io/nvidia/cloud-native/nvidia-fs@sha256:10d1df8034373061366d4fb17b364b3b28d766b54d5a0b700c1a5a75378cf125",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			image, err := tc.spec.GetImagePath(tc.osVersion)
			if tc.errorExpected {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, image, tc.expectedImage)
		})
	}
}

func TestGDRCopyGetImagePath(t *testing.T) {
	testCases := []struct {
		description   string
		spec          *GDRCopySpec
		osVersion     string
		errorExpected bool
		expectedImage string
	}{
		{
			description: "malformed repository",
			spec: &GDRCopySpec{
				Repository: "malformed?/repo",
			},
			errorExpected: true,
			expectedImage: "",
		},
		{
			description: "malformed image",
			spec: &GDRCopySpec{
				Image: "malformed?image",
			},
			errorExpected: true,
			expectedImage: "",
		},
		{
			description: "valid image",
			spec: &GDRCopySpec{
				Repository: "nvcr.io/nvidia/cloud-native",
				Image:      "gdrdrv",
				Version:    "v2.4.1",
			},
			osVersion:     "ubuntu20.04",
			errorExpected: false,
			expectedImage: "nvcr.io/nvidia/cloud-native/gdrdrv:v2.4.1-ubuntu20.04",
		},
		{
			description: "only image provided with no tag or digest",
			spec: &GDRCopySpec{
				Image: "nvcr.io/nvidia/cloud-native",
			},
			errorExpected: true,
			expectedImage: "",
		},
		{
			description: "repository, image, and version set; version is a digest",
			spec: &GDRCopySpec{
				Repository: "nvcr.io/nvidia/cloud-native",
				Image:      "gdrdrv",
				Version:    "sha256:" + testDigest,
			},
			osVersion:     "ubuntu22.04",
			errorExpected: false,
			expectedImage: "nvcr.io/nvidia/cloud-native/gdrdrv@sha256:10d1df8034373061366d4fb17b364b3b28d766b54d5a0b700c1a5a75378cf125",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			image, err := tc.spec.GetImagePath(tc.osVersion)
			if tc.errorExpected {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, image, tc.expectedImage)
		})
	}
}
