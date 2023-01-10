/*
Copyright 2022 NVIDIA CORPORATION & AFFILIATES
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

	"github.com/NVIDIA/k8s-operator-libs/pkg/consts"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
)

// ValidationManagerImpl implements the ValidationManager interface and waits on a validation pod,
// identified via podSelector, to be Ready.
type ValidationManagerImpl struct {
	k8sInterface             kubernetes.Interface
	log                      logr.Logger
	eventRecorder            record.EventRecorder
	nodeUpgradeStateProvider NodeUpgradeStateProvider

	// podSelector indicates the pod performing validation on the node after a driver upgrade
	podSelector string
}

// ValidationManager is an interface for validating driver upgrades
type ValidationManager interface {
	Validate(ctx context.Context, node *corev1.Node) (bool, error)
}

// NewValidationManager returns an instance of ValidationManager implementation
func NewValidationManager(
	k8sInterface kubernetes.Interface,
	log logr.Logger,
	eventRecorder record.EventRecorder,
	nodeUpgradeStateProvider NodeUpgradeStateProvider,
	podSelector string) *ValidationManagerImpl {

	mgr := &ValidationManagerImpl{
		k8sInterface:             k8sInterface,
		log:                      log,
		eventRecorder:            eventRecorder,
		nodeUpgradeStateProvider: nodeUpgradeStateProvider,
		podSelector:              podSelector,
	}

	return mgr
}

// Validate checks if the validation pod(s), identified via podSelector, is Ready
func (m *ValidationManagerImpl) Validate(ctx context.Context, node *corev1.Node) (bool, error) {
	if m.podSelector == "" {
		return true, nil
	}

	// fetch the pods using the label selector provided
	listOptions := metav1.ListOptions{LabelSelector: m.podSelector, FieldSelector: "spec.nodeName=" + node.Name}
	podList, err := m.k8sInterface.CoreV1().Pods("").List(ctx, listOptions)
	if err != nil {
		m.log.V(consts.LogLevelError).Error(err, "Failed to list pods", "selector", m.podSelector, "node", node.Name)
		return false, err
	}

	if len(podList.Items) == 0 {
		m.log.V(consts.LogLevelWarning).Info("No validation pods found on the node", "node", node.Name, "podSelector", m.podSelector)
		return false, nil
	}

	m.log.V(consts.LogLevelDebug).Info("Found validation pods", "selector", m.podSelector, "node", node.Name, "pods", len(podList.Items))

	done := true
	for _, pod := range podList.Items {
		if !m.isPodReady(pod) {
			done = false
			break
		}
	}
	return done, nil
}

func (m *ValidationManagerImpl) isPodReady(pod corev1.Pod) bool {
	if pod.Status.Phase != "Running" {
		m.log.V(consts.LogLevelDebug).Info("Pod not Running", "pod", pod.Name, "podPhase", pod.Status.Phase)
		return false
	}
	if len(pod.Status.ContainerStatuses) == 0 {
		m.log.V(consts.LogLevelDebug).Info("No containers running in pod", "pod", pod.Name)
		return false
	}

	for i := range pod.Status.ContainerStatuses {
		if !pod.Status.ContainerStatuses[i].Ready {
			m.log.V(consts.LogLevelDebug).Info("Not all containers ready in pod", "pod", pod.Name)
			return false
		}
	}

	return true
}
