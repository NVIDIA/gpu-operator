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

// Package kube implements the Kubernetes reads and writes of the applier and
// the gate on a controller-runtime client.
package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/decision"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/gate"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/loadid"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/publish"
)

// NodeNameField is the field selector key that scopes pods and
// ResourceSlices to a node.
const NodeNameField = "spec.nodeName"

const resourceGroup = "resource.k8s.io"

// resourceVersions are the resource.k8s.io versions the claim and slice reads
// can use, newest first, the order the operator picks one in for the DRA
// stack. The fields read have the same names in all of them.
var resourceVersions = []string{"v1", "v1beta2", "v1beta1"}

// NewScheme registers the types the applier and the gate read. ResourceClaims
// and ResourceSlices are read unstructured, in the version the cluster serves.
func NewScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(s))
	utilruntime.Must(appsv1.AddToScheme(s))
	return s
}

// ServedDRAVersion returns the newest resource.k8s.io version that serves
// both ResourceClaims and ResourceSlices, or "" when the cluster serves none.
func ServedDRAVersion(mapper meta.RESTMapper) (string, error) {
	for _, v := range resourceVersions {
		served := true
		for _, kind := range []string{"ResourceClaim", "ResourceSlice"} {
			_, err := mapper.RESTMapping(schema.GroupKind{Group: resourceGroup, Kind: kind}, v)
			if meta.IsNoMatchError(err) {
				served = false
				break
			}
			if err != nil {
				return "", fmt.Errorf("discovering %s/%s %s: %w", resourceGroup, v, kind, err)
			}
		}
		if served {
			return v, nil
		}
	}
	return "", nil
}

// NewClients returns a direct client with a per-request timeout and a watch
// client without one, since a client timeout would cut watches. They share one
// REST mapper, so discovery still times out.
func NewClients(cfg *rest.Config, timeout time.Duration, opts client.Options) (client.Client, client.WithWatch, error) {
	timed := rest.CopyConfig(cfg)
	timed.Timeout = timeout
	c, err := client.New(timed, opts)
	if err != nil {
		return nil, nil, err
	}
	untimed := rest.CopyConfig(cfg)
	untimed.Timeout = 0
	opts.HTTPClient = nil
	opts.Mapper = c.RESTMapper()
	w, err := client.NewWithWatch(untimed, opts)
	if err != nil {
		return nil, nil, err
	}
	return c, w, nil
}

// Client must be a direct client, not one backed by a cache: a read
// without a resourceVersion is then a quorum read.
type Client struct {
	C client.Client
	// DRAVersion is the resource.k8s.io version the claim and slice reads
	// use, from ServedDRAVersion; "" when the cluster serves none.
	DRAVersion string
}

var (
	_ decision.Cluster       = &Client{}
	_ gate.Cluster           = &Client{}
	_ loadid.DaemonSetLister = &Client{}
	_ publish.NodeClient     = &Client{}
)

// fromCache asks the API server to answer from its watch cache.
var fromCache = &client.GetOptions{Raw: &metav1.GetOptions{ResourceVersion: "0"}}

func (c *Client) GetNode(ctx context.Context, name string) (*corev1.Node, error) {
	node := &corev1.Node{}
	if err := c.C.Get(ctx, client.ObjectKey{Name: name}, node); err != nil {
		return nil, err
	}
	return node, nil
}

// ListNodePods lists the node's pods with a consistent read: the GPU pod check
// must not miss a pod a lagging cache hasn't seen. Terminal phases are dropped
// here rather than by a field selector.
func (c *Client) ListNodePods(ctx context.Context, node string) ([]corev1.Pod, error) {
	list := &corev1.PodList{}
	opts := &client.ListOptions{FieldSelector: fields.OneTermEqualSelector(NodeNameField, node)}
	if err := c.C.List(ctx, list, opts); err != nil {
		return nil, err
	}
	out := make([]corev1.Pod, 0, len(list.Items))
	for _, p := range list.Items {
		if p.Status.Phase != corev1.PodSucceeded && p.Status.Phase != corev1.PodFailed {
			out = append(out, p)
		}
	}
	return out, nil
}

func (c *Client) ClaimDrivers(ctx context.Context, namespace, name string) ([]string, error) {
	// With no claim API served, no claim can exist; a pod that references one
	// is counted the way a missing claim is.
	if c.DRAVersion == "" {
		return nil, decision.ErrClaimNotFound
	}
	claim := &unstructured.Unstructured{}
	claim.SetGroupVersionKind(schema.GroupVersionKind{Group: resourceGroup, Version: c.DRAVersion, Kind: "ResourceClaim"})
	err := c.C.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, claim)
	if apierrors.IsNotFound(err) {
		return nil, decision.ErrClaimNotFound
	}
	if err != nil {
		return nil, err
	}
	results, _, err := unstructured.NestedSlice(claim.Object, "status", "allocation", "devices", "results")
	if err != nil {
		return nil, fmt.Errorf("claim %s/%s: %w", namespace, name, err)
	}
	drivers := make([]string, 0, len(results))
	for _, r := range results {
		result, ok := r.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("claim %s/%s: allocation result isn't an object", namespace, name)
		}
		driver, _, err := unstructured.NestedString(result, "driver")
		if err != nil {
			return nil, fmt.Errorf("claim %s/%s: %w", namespace, name, err)
		}
		drivers = append(drivers, driver)
	}
	return drivers, nil
}

func (c *Client) NodeSliceDrivers(ctx context.Context, node string) ([]string, error) {
	if c.DRAVersion == "" {
		return nil, nil
	}
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{Group: resourceGroup, Version: c.DRAVersion, Kind: "ResourceSliceList"})
	if err := c.C.List(ctx, list, client.MatchingFields{NodeNameField: node}); err != nil {
		return nil, err
	}
	drivers := make([]string, 0, len(list.Items))
	for _, s := range list.Items {
		driver, _, err := unstructured.NestedString(s.Object, "spec", "driver")
		if err != nil {
			return nil, fmt.Errorf("ResourceSlice %s: %w", s.GetName(), err)
		}
		drivers = append(drivers, driver)
	}
	return drivers, nil
}

func (c *Client) DaemonSetExists(ctx context.Context, namespace, name string) (bool, error) {
	err := c.C.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &appsv1.DaemonSet{}, fromCache)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

func (c *Client) NodeLabels(ctx context.Context, name string) (map[string]string, error) {
	node := &corev1.Node{}
	if err := c.C.Get(ctx, client.ObjectKey{Name: name}, node, fromCache); err != nil {
		return nil, err
	}
	return node.Labels, nil
}

func (c *Client) ListDaemonSets(ctx context.Context, namespace, labelSelector string) ([]appsv1.DaemonSet, error) {
	return c.listDaemonSets(ctx, namespace, labelSelector, "")
}

// ListDaemonSetsFromCache lists like ListDaemonSets, but from the API
// server's watch cache rather than with a quorum read.
func (c *Client) ListDaemonSetsFromCache(ctx context.Context, namespace, labelSelector string) ([]appsv1.DaemonSet, error) {
	return c.listDaemonSets(ctx, namespace, labelSelector, "0")
}

func (c *Client) listDaemonSets(ctx context.Context, namespace, labelSelector, resourceVersion string) ([]appsv1.DaemonSet, error) {
	sel, err := labels.Parse(labelSelector)
	if err != nil {
		return nil, fmt.Errorf("label selector %q: %w", labelSelector, err)
	}
	list := &appsv1.DaemonSetList{}
	opts := &client.ListOptions{
		Namespace:     namespace,
		LabelSelector: sel,
		Raw:           &metav1.ListOptions{ResourceVersion: resourceVersion},
	}
	if err := c.C.List(ctx, list, opts); err != nil {
		return nil, err
	}
	return list.Items, nil
}

// PatchNodeLabels merges labels into the node, leaving its other labels as
// they are.
func (c *Client) PatchNodeLabels(ctx context.Context, name string, labels map[string]string) error {
	patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"labels": labels}})
	if err != nil {
		return err
	}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name}}
	return c.C.Patch(ctx, node, client.RawPatch(types.MergePatchType, patch))
}
