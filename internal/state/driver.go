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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/source"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/clusterinfo"
	"github.com/NVIDIA/gpu-operator/internal/image"
	"github.com/NVIDIA/gpu-operator/internal/render"
	"github.com/NVIDIA/gpu-operator/internal/utils"
)

type stateDriver struct {
	stateSkel
}

var _ State = (*stateDriver)(nil)

type driverRuntimeSpec struct {
	Namespace         string
	OpenshiftVersion  string
	KubernetesVersion string
}

type additionalVolumeMounts struct {
	VolumeMounts []corev1.VolumeMount
	Volumes      []corev1.Volume
}

type driverRenderData struct {
	Driver                 *driverSpec
	Operator               *gpuv1.OperatorSpec
	Validator              *validatorSpec
	GDS                    *gdsDriverSpec
	GPUDirectRDMA          *gpuv1.GPUDirectRDMASpec
	RuntimeSpec            driverRuntimeSpec
	AdditionalVolumeMounts additionalVolumeMounts
}

func NewStateDriver(
	k8sClient client.Client,
	scheme *runtime.Scheme,
	manifestDir string) (State, error) {

	files, err := utils.GetFilesWithSuffix(manifestDir, render.ManifestFileSuffix...)
	if err != nil {
		return nil, fmt.Errorf("failed to get files from manifest directory: %v", err)
	}

	renderer := render.NewRenderer(files)
	state := &stateDriver{
		stateSkel: stateSkel{
			name:        "state-driver",
			description: "NVIDIA driver deployed in the cluster",
			client:      k8sClient,
			scheme:      scheme,
			renderer:    renderer,
		},
	}
	return state, nil
}

func (s *stateDriver) Sync(ctx context.Context, customResource interface{}, infoCatalog InfoCatalog) (SyncState, error) {
	// TODO: finish implementing
	// logger := log.FromContext(ctx)
	// cr := customResource.(*nvidiav1alpha1.NVIDIADriver)

	info := infoCatalog.Get(InfoTypeClusterInfo)
	if info == nil {
		return SyncStateNotReady, fmt.Errorf("failed to get cluster info")
	}
	_ = info.(clusterinfo.Interface)

	// objs, err := s.getManifestObjects(ctx, cr)
	return SyncStateNotReady, nil
}

func (s *stateDriver) GetWatchSources(mgr ctrlManager) map[string]SyncingSource {
	wr := make(map[string]SyncingSource)
	wr["DaemonSet"] = source.Kind(
		mgr.GetCache(),
		&appsv1.DaemonSet{},
	)
	return wr
}

func (s *stateDriver) getManifestObjects(ctx context.Context, cr *nvidiav1alpha1.NVIDIADriver, clusterInfo clusterinfo.Interface) ([]*unstructured.Unstructured, error) {
	// TODO: implement this
	return nil, nil
}

func getDriverSpecWrapper(spec *nvidiav1alpha1.NVIDIADriverSpec) (*driverSpec, error) {
	imagePath, err := image.ImagePath(spec.Repository, spec.Image, spec.Version, "DRIVER_IMAGE")
	if err != nil {
		return nil, fmt.Errorf("failed to construct image path: %v", err)
	}

	managerImagePath, err := image.ImagePath(spec.Manager.Repository, spec.Manager.Image, spec.Manager.Version, "DRIVER_MANAGER_IMAGE")
	if err != nil {
		return nil, fmt.Errorf("failed to construct image path for driver manager: %v", err)
	}

	return &driverSpec{
		spec,
		imagePath,
		managerImagePath,
	}, nil
}

func getValidatorSpecWrapper(spec *gpuv1.ValidatorSpec) (*validatorSpec, error) {
	imagePath, err := image.ImagePath(spec.Repository, spec.Image, spec.Version, "DRIVER_IMAGE")
	if err != nil {
		return nil, fmt.Errorf("failed to contruct image path for driver: %v", err)
	}

	return &validatorSpec{
		spec,
		imagePath,
	}, nil
}
