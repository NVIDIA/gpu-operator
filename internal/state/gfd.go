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

package state

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/image"
)

const (
	// gfdImageEnvName is the fallback env var for the GFD image when the CR does not
	// specify repository/image/version.
	gfdImageEnvName = "GFD_IMAGE"

	// draAdminNamespaceLabelKey is the label the kube-scheduler requires on a namespace
	// before it allows adminAccess: true in ResourceClaim/ResourceClaimTemplate objects.
	draAdminNamespaceLabelKey = "resource.kubernetes.io/admin-access"
)

type stateGFD struct {
	stateSkel
}

var _ State = (*stateGFD)(nil)

func NewStateGFD(
	k8sClient client.Client,
	namespace string,
	scheme *runtime.Scheme,
	manifestDir string) (State, error) {

	skel, err := newStateSkel(k8sClient, namespace, scheme, manifestDir,
		"state-gfd", "NVIDIA GPU Feature Discovery deployed in the cluster")
	if err != nil {
		return nil, err
	}
	return &stateGFD{stateSkel: skel}, nil
}

func (s *stateGFD) Sync(ctx context.Context, customResource interface{}, infoCatalog InfoCatalog) (SyncState, error) {
	cr, ok := customResource.(*nvidiav1alpha1.GPUClusterConfig)
	if !ok {
		return SyncStateError, fmt.Errorf("GPUClusterConfig CR not provided as input to Sync()")
	}

	objs, err := s.getManifestObjects(ctx, cr, infoCatalog)
	if err != nil {
		return SyncStateNotReady, fmt.Errorf("failed to create k8s objects from manifests: %w", err)
	}

	if len(objs) == 0 {
		return s.handleStateObjectsDeletion(ctx)
	}

	return s.syncObjects(ctx, cr, objs)
}

func (s *stateGFD) GetWatchSources(mgr ctrlManager) map[string]SyncingSource {
	return gpuClusterConfigDaemonSetSource(mgr)
}

func (s *stateGFD) getManifestObjects(ctx context.Context, cr *nvidiav1alpha1.GPUClusterConfig, infoCatalog InfoCatalog) ([]*unstructured.Unstructured, error) {
	if cr.Spec.GFD == nil || !cr.Spec.GFD.IsEnabled() {
		return []*unstructured.Unstructured{}, nil
	}

	apiVersion, err := draResourceAPIVersion(infoCatalog)
	if err != nil {
		return nil, err
	}

	if err := s.ensureAdminAccessLabel(ctx); err != nil {
		return nil, fmt.Errorf("failed to label namespace for admin access: %w", err)
	}

	spec := cr.Spec.GFD
	imagePath, err := image.ImagePath(spec.Repository, spec.Image, spec.Version, gfdImageEnvName)
	if err != nil {
		return nil, fmt.Errorf("failed to construct GFD image path: %w", err)
	}

	daemonsets := cr.Spec.Daemonsets
	renderData := &gfdRenderData{
		GFD:                     &gfdSpec{Spec: spec, ImagePath: imagePath},
		Daemonsets:              &daemonsets,
		Namespace:               s.namespace,
		ResourceClaimAPIVersion: apiVersion,
	}

	return s.renderObjects(ctx, renderData)
}

// ensureAdminAccessLabel patches the operator namespace with the label required by the
// kube-scheduler to allow adminAccess: true in ResourceClaim/ResourceClaimTemplate
// objects. The label is deliberately never removed: it is namespace-level configuration
// that other adminAccess consumers in the namespace may rely on.
func (s *stateGFD) ensureAdminAccessLabel(ctx context.Context) error {
	ns := &corev1.Namespace{}
	if err := s.client.Get(ctx, client.ObjectKey{Name: s.namespace}, ns); err != nil {
		return fmt.Errorf("could not get namespace %s: %w", s.namespace, err)
	}
	if ns.Labels[draAdminNamespaceLabelKey] == "true" {
		return nil
	}
	patch := client.MergeFrom(ns.DeepCopy())
	if ns.Labels == nil {
		ns.Labels = make(map[string]string)
	}
	ns.Labels[draAdminNamespaceLabelKey] = "true"
	return s.client.Patch(ctx, ns, patch)
}
