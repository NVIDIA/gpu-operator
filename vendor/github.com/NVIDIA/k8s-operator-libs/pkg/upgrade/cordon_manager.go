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

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubectl/pkg/drain"
)

// CordonManagerImpl implements CordonManager interface and can
// cordon / uncordon k8s nodes
type CordonManagerImpl struct {
	k8sInterface kubernetes.Interface
	log          logr.Logger
}

// CordonManager provides methods for cordoning / uncordoning nodes
type CordonManager interface {
	Cordon(ctx context.Context, node *corev1.Node) error
	Uncordon(ctx context.Context, node *corev1.Node) error
}

// Cordon marks a node as unschedulable
func (m *CordonManagerImpl) Cordon(ctx context.Context, node *corev1.Node) error {
	helper := &drain.Helper{Ctx: ctx, Client: m.k8sInterface}
	return drain.RunCordonOrUncordon(helper, node, true)
}

// Uncordon marks a node as schedulable
func (m *CordonManagerImpl) Uncordon(ctx context.Context, node *corev1.Node) error {
	helper := &drain.Helper{Ctx: ctx, Client: m.k8sInterface}
	return drain.RunCordonOrUncordon(helper, node, false)
}

// NewCordonManager returns a CordonManagerImpl
func NewCordonManager(k8sInterface kubernetes.Interface, log logr.Logger) *CordonManagerImpl {
	return &CordonManagerImpl{
		k8sInterface: k8sInterface,
		log:          log,
	}
}
