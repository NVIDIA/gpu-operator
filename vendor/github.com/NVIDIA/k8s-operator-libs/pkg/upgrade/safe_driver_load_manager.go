/*
Copyright 2023 NVIDIA CORPORATION & AFFILIATES

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package upgrade

import (
	"context"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"

	"github.com/NVIDIA/k8s-operator-libs/pkg/consts"
)

// SafeDriverLoadManagerImpl default implementation of the SafeDriverLoadManager interface
// Support for safe driver loading is implemented as a part of the upgrade flow.
// When UpgradeStateManager detects a node that is waiting for a safe driver load,
// it will unconditionally transfer it to the UpgradeStateUpgradeRequired state and wait for Cordon
// and Drain operations to complete according to the upgrade policy.
// When the Pod is eventually in the UpgradeStatePodRestartRequired state,
// the UpgradeStateManager will unblock the driver loading (by removing the safe driver load annotation)
// instead of restarting the Pod.
// The default implementation of the SafeDriverLoadManager interface assumes that the driver's safe load
// mechanism is implemented as a two-step procedure.
// As a first step, the driver pod should load the init container,
// which will set "safe driver load annotation" (defined in UpgradeWaitForSafeDriverLoadAnnotationKeyFmt)
// on the node object, then the container blocks until another entity removes the annotation from the node object.
// When the init container completes successfully (when the annotation was removed from the Node object),
// the driver Pod will proceed to the second step and do the driver loading.
// After that, the UpgradeStateManager will wait for the driver to become ready and then Uncordon the node if required.
type SafeDriverLoadManagerImpl struct {
	nodeUpgradeStateProvider NodeUpgradeStateProvider
	log                      logr.Logger
}

// IsWaitingForSafeDriverLoad checks if driver Pod on the node is waiting for a safe load.
// The check is implemented by check that "safe driver loading annotation" is set on the Node object
func (s *SafeDriverLoadManagerImpl) IsWaitingForSafeDriverLoad(_ context.Context, node *corev1.Node) (bool, error) {
	return node.Annotations[GetUpgradeDriverWaitForSafeLoadAnnotationKey()] != "", nil
}

// UnblockLoading unblocks driver loading on the node by remove "safe driver loading annotation"
// from the Node object
func (s *SafeDriverLoadManagerImpl) UnblockLoading(ctx context.Context, node *corev1.Node) error {
	annotationKey := GetUpgradeDriverWaitForSafeLoadAnnotationKey()
	if node.Annotations[annotationKey] == "" {
		return nil
	}
	// driver on the node is waiting for safe load, unblock loading
	err := s.nodeUpgradeStateProvider.ChangeNodeUpgradeAnnotation(ctx, node, annotationKey, "null")
	if err != nil {
		s.log.V(consts.LogLevelError).Error(
			err, "Failed to change node upgrade annotation for node", "node",
			node, "annotation", annotationKey)
		return err
	}
	return nil
}

// SafeDriverLoadManager interface defines handlers to interact with drivers that are waiting for a safe load
type SafeDriverLoadManager interface {
	// IsWaitingForSafeDriverLoad checks if driver Pod on the node is waiting for a safe load
	IsWaitingForSafeDriverLoad(ctx context.Context, node *corev1.Node) (bool, error)
	// UnblockLoading unblocks driver loading on the node
	UnblockLoading(ctx context.Context, node *corev1.Node) error
}

// NewSafeDriverLoadManager returns an instance of SafeDriverLoadManager implementation
func NewSafeDriverLoadManager(
	nodeUpgradeStateProvider NodeUpgradeStateProvider, log logr.Logger) *SafeDriverLoadManagerImpl {
	mgr := &SafeDriverLoadManagerImpl{
		log:                      log,
		nodeUpgradeStateProvider: nodeUpgradeStateProvider,
	}
	return mgr
}
