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

// Code generated by client-gen. DO NOT EDIT.

package v1alpha1

import (
	context "context"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	scheme "github.com/NVIDIA/gpu-operator/api/versioned/scheme"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	gentype "k8s.io/client-go/gentype"
)

// NVIDIADriversGetter has a method to return a NVIDIADriverInterface.
// A group's client should implement this interface.
type NVIDIADriversGetter interface {
	NVIDIADrivers() NVIDIADriverInterface
}

// NVIDIADriverInterface has methods to work with NVIDIADriver resources.
type NVIDIADriverInterface interface {
	Create(ctx context.Context, nVIDIADriver *nvidiav1alpha1.NVIDIADriver, opts v1.CreateOptions) (*nvidiav1alpha1.NVIDIADriver, error)
	Update(ctx context.Context, nVIDIADriver *nvidiav1alpha1.NVIDIADriver, opts v1.UpdateOptions) (*nvidiav1alpha1.NVIDIADriver, error)
	// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
	UpdateStatus(ctx context.Context, nVIDIADriver *nvidiav1alpha1.NVIDIADriver, opts v1.UpdateOptions) (*nvidiav1alpha1.NVIDIADriver, error)
	Delete(ctx context.Context, name string, opts v1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error
	Get(ctx context.Context, name string, opts v1.GetOptions) (*nvidiav1alpha1.NVIDIADriver, error)
	List(ctx context.Context, opts v1.ListOptions) (*nvidiav1alpha1.NVIDIADriverList, error)
	Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *nvidiav1alpha1.NVIDIADriver, err error)
	NVIDIADriverExpansion
}

// nVIDIADrivers implements NVIDIADriverInterface
type nVIDIADrivers struct {
	*gentype.ClientWithList[*nvidiav1alpha1.NVIDIADriver, *nvidiav1alpha1.NVIDIADriverList]
}

// newNVIDIADrivers returns a NVIDIADrivers
func newNVIDIADrivers(c *NvidiaV1alpha1Client) *nVIDIADrivers {
	return &nVIDIADrivers{
		gentype.NewClientWithList[*nvidiav1alpha1.NVIDIADriver, *nvidiav1alpha1.NVIDIADriverList](
			"nvidiadrivers",
			c.RESTClient(),
			scheme.ParameterCodec,
			"",
			func() *nvidiav1alpha1.NVIDIADriver { return &nvidiav1alpha1.NVIDIADriver{} },
			func() *nvidiav1alpha1.NVIDIADriverList { return &nvidiav1alpha1.NVIDIADriverList{} },
		),
	}
}
