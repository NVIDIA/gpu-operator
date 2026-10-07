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

package k8s

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const pollInterval = 5 * time.Second

// Clients holds Kubernetes client interfaces for interacting with the cluster.
type Clients struct {
	k8s        kubernetes.Interface
	dynamic    dynamic.Interface
	restConfig *rest.Config
}

// NewClients creates both typed and dynamic Kubernetes clients.
// Uses in-cluster config when running as a K8s Job, or kubeconfig
// (~/.kube/config / KUBECONFIG env) when running outside the cluster.
func NewClients() (*Clients, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig when not running inside a cluster.
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		config, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			loadingRules, &clientcmd.ConfigOverrides{}).ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("getting kubernetes config: %w", err)
		}
	}

	k8sClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("creating kubernetes client: %w", err)
	}

	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("creating dynamic client: %w", err)
	}

	return &Clients{
		k8s:        k8sClient,
		dynamic:    dynClient,
		restConfig: config,
	}, nil
}

// ServerVersion returns the Kubernetes server version.
func (c *Clients) ServerVersion() (*version.Info, error) {
	return c.k8s.Discovery().ServerVersion()
}

// GetNamespace retrieves a namespace by name.
func (c *Clients) GetNamespace(ctx context.Context, name string) (*corev1.Namespace, error) {
	return c.k8s.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
}

// ListDaemonSets lists DaemonSets in a namespace, optionally filtered by a label selector.
func (c *Clients) ListDaemonSets(ctx context.Context, namespace, labelSelector string) (*appsv1.DaemonSetList, error) {
	return c.k8s.AppsV1().DaemonSets(namespace).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
}

// GetConfigMap retrieves a ConfigMap by namespace and name.
func (c *Clients) GetConfigMap(ctx context.Context, namespace, name string) (*corev1.ConfigMap, error) {
	return c.k8s.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
}

// ResourceExists checks if a resource type exists in the cluster.
func (c *Clients) ResourceExists(ctx context.Context, gvr schema.GroupVersionResource) bool {
	_, err := c.k8s.Discovery().ServerResourcesForGroupVersion(gvr.GroupVersion().String())
	return err == nil
}

// GetDynamicResource retrieves an unstructured resource.
func (c *Clients) GetDynamicResource(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string) (*unstructured.Unstructured, error) {
	if namespace == "" {
		return c.dynamic.Resource(gvr).Get(ctx, name, metav1.GetOptions{})
	}
	return c.dynamic.Resource(gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
}

// ListDynamicResource lists unstructured resources.
func (c *Clients) ListDynamicResource(ctx context.Context, gvr schema.GroupVersionResource, namespace string) (*unstructured.UnstructuredList, error) {
	if namespace == "" {
		return c.dynamic.Resource(gvr).List(ctx, metav1.ListOptions{})
	}
	return c.dynamic.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
}

// ListNodeEvents returns events for a specific node.
func (c *Clients) ListNodeEvents(ctx context.Context, nodeName string) (*corev1.EventList, error) {
	return c.k8s.CoreV1().Events("").List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("involvedObject.kind=Node,involvedObject.name=%s", nodeName),
	})
}

// ProxyGetService makes an HTTP GET request to a Service via the API server proxy.
// This works both in-cluster and out-of-cluster, unlike direct Service DNS calls.
func (c *Clients) ProxyGetService(ctx context.Context, namespace, serviceName string, port int, path string) ([]byte, error) {
	return c.k8s.CoreV1().Services(namespace).ProxyGet("", serviceName, fmt.Sprintf("%d", port), path, nil).DoRaw(ctx)
}
