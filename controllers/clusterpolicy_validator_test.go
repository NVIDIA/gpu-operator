/*
 * Copyright (c) NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package controllers

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
)

func TestValidateDRA(t *testing.T) {
	tests := []struct {
		description  string
		spec         *gpuv1.ClusterPolicySpec
		draSupported bool
		err          error
	}{
		{
			description: "dra not supported, dra driver not enabled",
			spec:        &gpuv1.ClusterPolicySpec{},
		},
		{
			description: "dra not supported, dra driver enabled",
			spec: &gpuv1.ClusterPolicySpec{
				DRADriver: gpuv1.DRADriverSpec{
					GPUs: gpuv1.DRADriverGPUs{
						Enabled: ptr.To(true),
					},
				},
			},
			err: errors.New("the NVIDIA DRA driver for GPUs is enabled in ClusterPolicy but Dynamic Resource Allocation is not enabled in the Kubernetes cluster"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			err := validateDRA(tc.spec, tc.draSupported)
			if tc.err == nil {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Equal(t, tc.err.Error(), err.Error())
			}
		})
	}
}

func TestValidateNRIPlugin(t *testing.T) {
	tests := []struct {
		description string
		spec        *gpuv1.ClusterPolicySpec
		err         error
	}{
		{
			description: "valid CDI object in spec",
			spec: &gpuv1.ClusterPolicySpec{
				CDI: gpuv1.CDIConfigSpec{
					Enabled:          ptr.To(true),
					NRIPluginEnabled: ptr.To(true),
				},
			},
		},
		{
			description: "invalid CDI object in spec",
			spec: &gpuv1.ClusterPolicySpec{
				CDI: gpuv1.CDIConfigSpec{
					Enabled:          ptr.To(false),
					NRIPluginEnabled: ptr.To(true),
				},
			},
			err: errors.New("the NRI Plugin cannot be enabled when CDI is disabled"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			err := validateNRIPlugin(tc.spec)
			if tc.err == nil {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Equal(t, tc.err.Error(), err.Error())
			}
		})
	}
}
