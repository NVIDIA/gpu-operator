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

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/image"
)

// configurableState is a State implementation shared by the GPUCluster operands
// that reconcile with the same shape: skip when disabled, resolve the DRA apiVersion,
// label the namespace for adminAccess, resolve the operand image, build the operand's
// render data, and render. Per-operand behavior is injected through the function fields
// so each operand file only declares what actually differs.
type configurableState struct {
	stateSkel

	// isEnabled reports whether this operand should be deployed for the given CR.
	isEnabled func(cr *nvidiav1alpha1.GPUCluster) bool

	// imageOverride resolves the (repository, image, version) overrides from the CR,
	// which feed image.ImagePath together with imageEnvName. It may be nil for operands
	// that carry no per-CR image overrides (e.g. those running the gpu-operator image).
	imageOverride func(cr *nvidiav1alpha1.GPUCluster) (repository, image, version string)

	// imageEnvName is the fallback env var for the operand image.
	imageEnvName string

	// buildRenderData builds the operand-specific templating data from the resolved
	// image path and DRA apiVersion. It receives ctx and the skeleton so operands that
	// need the client or logging (e.g. dcgm-exporter's ServiceMonitor CRD probe) can use them.
	buildRenderData func(ctx context.Context, s *configurableState, cr *nvidiav1alpha1.GPUCluster, imagePath, apiVersion, openshiftVersion string) (interface{}, error)
}

var _ State = (*configurableState)(nil)

func (s *configurableState) Sync(ctx context.Context, customResource interface{}, infoCatalog InfoCatalog) (SyncState, error) {
	cr, ok := customResource.(*nvidiav1alpha1.GPUCluster)
	if !ok {
		return SyncStateError, fmt.Errorf("GPUCluster CR not provided as input to Sync()")
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

func (s *configurableState) GetWatchSources(mgr ctrlManager) map[string]SyncingSource {
	return gpuClusterDaemonSetSource(mgr)
}

func (s *configurableState) getManifestObjects(ctx context.Context, cr *nvidiav1alpha1.GPUCluster, infoCatalog InfoCatalog) ([]*unstructured.Unstructured, error) {
	if !s.isEnabled(cr) {
		return []*unstructured.Unstructured{}, nil
	}

	apiVersion, err := draResourceAPIVersion(infoCatalog)
	if err != nil {
		return nil, err
	}

	openshiftVersion, err := clusterOpenshiftVersion(infoCatalog)
	if err != nil {
		return nil, fmt.Errorf("failed to get OpenShift version: %w", err)
	}

	if err := ensureAdminAccessLabel(ctx, s.client, s.namespace); err != nil {
		return nil, fmt.Errorf("failed to label namespace for admin access: %w", err)
	}

	var repository, img, version string
	if s.imageOverride != nil {
		repository, img, version = s.imageOverride(cr)
	}
	imagePath, err := image.ImagePath(repository, img, version, s.imageEnvName)
	if err != nil {
		return nil, fmt.Errorf("failed to construct %s image path: %w", s.name, err)
	}

	renderData, err := s.buildRenderData(ctx, s, cr, imagePath, apiVersion, openshiftVersion)
	if err != nil {
		return nil, err
	}

	return s.renderObjects(ctx, renderData)
}
