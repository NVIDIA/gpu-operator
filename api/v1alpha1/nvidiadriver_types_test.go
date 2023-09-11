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
		errorExpected bool
		expectedImage string
	}{
		{
			description: "malformed image",
			spec: &NVIDIADriverSpec{
				Image: "malformed?image",
			},
			errorExpected: true,
			expectedImage: "",
		},
		{
			description: "only image provided",
			spec: &NVIDIADriverSpec{
				Image: "nvcr.io/nvidia/driver",
			},
			errorExpected: true,
			expectedImage: "",
		},
		{
			description: "only image provided with tag",
			spec: &NVIDIADriverSpec{
				Image: "nvcr.io/nvidia/driver:525.85.03-ubuntu22.04",
			},
			expectedImage: "nvcr.io/nvidia/driver:525.85.03-ubuntu22.04",
		},
		{
			description: "only image provided with digest",
			spec: &NVIDIADriverSpec{
				Image: "nvcr.io/nvidia/driver@sha256:" + testDigest,
			},
			expectedImage: "nvcr.io/nvidia/driver@sha256:" + testDigest,
		},
		{
			description: "image provided with tag, version and osVersion ignored",
			spec: &NVIDIADriverSpec{
				Image:     "nvcr.io/nvidia/driver:525.85.03-ubuntu22.04",
				Version:   "535.104.05",
				OSVersion: "ubuntu20.04",
			},
			expectedImage: "nvcr.io/nvidia/driver:525.85.03-ubuntu22.04",
		},
		{
			description: "image provided with digest, version and osVersion ignored",
			spec: &NVIDIADriverSpec{
				Image:     "nvcr.io/nvidia/driver@sha256:" + testDigest,
				Version:   "535.104.05",
				OSVersion: "ubuntu20.04",
			},
			expectedImage: "nvcr.io/nvidia/driver@sha256:" + testDigest,
		},
		{
			description: "missing version",
			spec: &NVIDIADriverSpec{
				Image:     "nvcr.io/nvidia/driver",
				OSVersion: "ubuntu22.04",
			},
			errorExpected: true,
			expectedImage: "",
		},
		{
			description: "missing OS version",
			spec: &NVIDIADriverSpec{
				Image:   "nvcr.io/nvidia/driver",
				Version: "535.104.05",
			},
			errorExpected: true,
			expectedImage: "",
		},
		{
			description: "image with version and OS version specified",
			spec: &NVIDIADriverSpec{
				Image:     "nvcr.io/nvidia/driver",
				Version:   "535.104.05",
				OSVersion: "ubuntu22.04",
			},
			expectedImage: "nvcr.io/nvidia/driver:535.104.05-ubuntu22.04",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			image, err := tc.spec.GetImagePath()
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
		kernelVersion string
		errorExpected bool
		expectedImage string
	}{
		{
			description: "malformed image",
			spec: &NVIDIADriverSpec{
				Image: "malformed?image",
			},
			errorExpected: true,
			expectedImage: "",
		},
		{
			description: "only image provided",
			spec: &NVIDIADriverSpec{
				Image: "nvcr.io/nvidia/driver",
			},
			errorExpected: true,
			expectedImage: "",
		},
		{
			description: "image provided with tag",
			spec: &NVIDIADriverSpec{
				Image: "nvcr.io/nvidia/driver:525.85.03-ubuntu22.04",
			},
			errorExpected: true,
			expectedImage: "",
		},
		{
			description: "image provided with digest",
			spec: &NVIDIADriverSpec{
				Image: "nvcr.io/nvidia/driver@sha256:" + testDigest,
			},
			errorExpected: true,
			expectedImage: "",
		},
		{
			description: "missing driver branch",
			spec: &NVIDIADriverSpec{
				Image:     "nvcr.io/nvidia/driver",
				OSVersion: "ubuntu22.04",
			},
			errorExpected: true,
			expectedImage: "",
		},
		{
			description: "missing OS version",
			spec: &NVIDIADriverSpec{
				Image:   "nvcr.io/nvidia/driver",
				Version: "535",
			},
			errorExpected: true,
			expectedImage: "",
		},
		{
			description: "image with driver branch and OS version specified",
			spec: &NVIDIADriverSpec{
				Image:     "nvcr.io/nvidia/driver",
				Version:   "535",
				OSVersion: "ubuntu22.04",
			},
			kernelVersion: "5.4.0-150-generic",
			expectedImage: "nvcr.io/nvidia/driver:535-5.4.0-150-generic-ubuntu22.04",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			image, err := tc.spec.GetPrecompiledImagePath(tc.kernelVersion)
			if tc.errorExpected {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, image, tc.expectedImage)
		})
	}
}

func TestParseOSString(t *testing.T) {
	testCases := []struct {
		description       string
		input             string
		errorExpected     bool
		expectedOS        string
		expectedOSVersion string
	}{
		{
			description:   "empty string",
			input:         "",
			errorExpected: true,
		},
		{
			description:   "no number in os string",
			input:         "ubuntu",
			errorExpected: true,
		},
		{
			description:       "ubuntu20.04",
			input:             "ubuntu20.04",
			expectedOS:        "ubuntu",
			expectedOSVersion: "20.04",
		},
		{
			description:       "rhcos4.13",
			input:             "rhcos4.13",
			expectedOS:        "rhcos",
			expectedOSVersion: "4.13",
		},
		{
			description:       "centos7",
			input:             "centos7",
			expectedOS:        "centos",
			expectedOSVersion: "7",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			os, osVersion, err := parseOSString(tc.input)
			if tc.errorExpected {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.expectedOS, os)
			require.Equal(t, tc.expectedOSVersion, osVersion)
		})
	}
}
