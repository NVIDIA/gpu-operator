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

package operatorcache

import (
	"fmt"
	"maps"

	apiimagev1 "github.com/openshift/api/image/v1"
	corev1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	"github.com/NVIDIA/gpu-operator/controllers"
	"github.com/NVIDIA/gpu-operator/internal/consts"
)

// New creates the operator cache while permitting optional OpenShift APIs to be absent.
func New(config *rest.Config, options cache.Options) (cache.Cache, error) {
	// ByObject scopes are resolved eagerly, even if no informer is ever used.
	// The manager supplies its shared scheme and discovery mapper before calling
	// this factory, so optional APIs need not become startup requirements.
	options.ByObject = maps.Clone(options.ByObject)
	for object := range options.ByObject {
		switch object.(type) {
		case *apiimagev1.ImageStream, *resourcev1.ResourceClaim:
		default:
			continue
		}
		_, err := apiutil.IsObjectNamespaced(object, options.Scheme, options.Mapper)
		if meta.IsNoMatchError(err) {
			delete(options.ByObject, object)
		} else if err != nil {
			// An unavailable API server is not evidence that an API is absent.
			return nil, fmt.Errorf("discover optional cache API %T: %w", object, err)
		}
	}
	return cache.New(config, options)
}

func stripUnusedNodeFields(in any) (any, error) {
	node, ok := in.(*corev1.Node)
	if !ok {
		return in, nil
	}

	runtimeVersion := node.Status.NodeInfo.ContainerRuntimeVersion
	node.ManagedFields = nil
	node.Spec = corev1.NodeSpec{}
	node.Status = corev1.NodeStatus{
		NodeInfo: corev1.NodeSystemInfo{ContainerRuntimeVersion: runtimeVersion},
	}
	return node, nil
}

// Options returns cache settings for GPU Operator resources.
func Options(operatorNamespace string) cache.Options {
	stripManagedFields := cache.TransformStripManagedFields()
	cachedNamespaces := map[string]cache.Config{
		operatorNamespace:         {},
		consts.OpenshiftNamespace: {},
	}
	return cache.Options{
		DefaultNamespaces: map[string]cache.Config{operatorNamespace: {}},
		DefaultTransform:  stripManagedFields,
		ByObject: map[client.Object]cache.ByObject{
			&corev1.Node{}: {
				// The operator reads only the container runtime version from node
				// status. Drop all other status data from the cached object.
				Transform: stripUnusedNodeFields,
			},
			&corev1.Pod{}:               {Namespaces: cachedNamespaces},
			&resourcev1.ResourceClaim{}: {Namespaces: cachedNamespaces},
			&apiextensionsv1.CustomResourceDefinition{}: {
				Field: fields.OneTermEqualSelector(
					"metadata.name",
					controllers.ServiceMonitorCRDName,
				),
			},
			&apiimagev1.ImageStream{}: {
				Namespaces: map[string]cache.Config{consts.OpenshiftNamespace: {}},
				Field: fields.OneTermEqualSelector(
					"metadata.name",
					consts.DriverToolkitImageStreamName,
				),
			},
		},
	}
}
