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

package kube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	resourcev1beta1 "k8s.io/api/resource/v1beta1"
	resourcev1beta2 "k8s.io/api/resource/v1beta2"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/decision"
)

// recorded holds the options of the calls the client made.
type recorded struct {
	gets  []*metav1.GetOptions
	lists []*metav1.ListOptions
}

func newClient(t *testing.T, objs ...client.Object) (*Client, *recorded) {
	t.Helper()
	rec := &recorded{}
	// The resource.k8s.io types are registered only so the fake client can
	// store them; the client reads them unstructured.
	scheme := NewScheme()
	utilruntime.Must(resourcev1.AddToScheme(scheme))
	utilruntime.Must(resourcev1beta2.AddToScheme(scheme))
	utilruntime.Must(resourcev1beta1.AddToScheme(scheme))
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithIndex(&corev1.Pod{}, NodeNameField, func(o client.Object) []string {
			return []string{o.(*corev1.Pod).Spec.NodeName}
		}).
		WithIndex(&resourcev1.ResourceSlice{}, NodeNameField, sliceNodeName).
		WithIndex(&resourcev1beta2.ResourceSlice{}, NodeNameField, sliceNodeName).
		WithIndex(&resourcev1beta1.ResourceSlice{}, NodeNameField, sliceNodeName).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				o := &client.GetOptions{}
				o.ApplyOptions(opts)
				rec.gets = append(rec.gets, o.AsGetOptions())
				return c.Get(ctx, key, obj, opts...)
			},
			List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
				o := &client.ListOptions{}
				o.ApplyOptions(opts)
				rec.lists = append(rec.lists, o.AsListOptions().DeepCopy())
				return c.List(ctx, list, opts...)
			},
		}).
		Build()
	return &Client{C: c, DRAVersion: "v1"}, rec
}

// sliceNodeName indexes a ResourceSlice of any version by node. The fake
// client passes unstructured objects when the list is unstructured.
func sliceNodeName(o client.Object) []string {
	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(o)
	if err != nil {
		return nil
	}
	if n, _, _ := unstructured.NestedString(u, "spec", "nodeName"); n != "" {
		return []string{n}
	}
	return nil
}

func pod(name, node string, phase corev1.PodPhase) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec:       corev1.PodSpec{NodeName: node},
		Status:     corev1.PodStatus{Phase: phase},
	}
}

func TestGetNodeIsAQuorumRead(t *testing.T) {
	c, rec := newClient(t, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}})
	node, err := c.GetNode(context.Background(), "node-1")
	require.NoError(t, err)
	require.Equal(t, "node-1", node.Name)
	require.Empty(t, rec.gets[0].ResourceVersion)

	_, err = c.GetNode(context.Background(), "missing")
	require.Error(t, err)
}

func TestListNodePods(t *testing.T) {
	c, rec := newClient(t,
		pod("running", "node-1", corev1.PodRunning),
		pod("pending", "node-1", corev1.PodPending),
		pod("done", "node-1", corev1.PodSucceeded),
		pod("failed", "node-1", corev1.PodFailed),
		pod("elsewhere", "node-2", corev1.PodRunning),
	)
	pods, err := c.ListNodePods(context.Background(), "node-1")
	require.NoError(t, err)
	var names []string
	for _, p := range pods {
		names = append(names, p.Name)
	}
	require.ElementsMatch(t, []string{"running", "pending"}, names)

	opts := rec.lists[0]
	require.Equal(t, "spec.nodeName=node-1", opts.FieldSelector)
	require.Empty(t, opts.ResourceVersion, "a consistent read")
	require.Empty(t, opts.ResourceVersionMatch)
	require.Zero(t, opts.Limit)
}

// claims returns an allocated and a pending claim in version v.
func claims(v string) []client.Object {
	meta := func(name string) metav1.ObjectMeta { return metav1.ObjectMeta{Name: name, Namespace: "default"} }
	switch v {
	case "v1beta2":
		return []client.Object{
			&resourcev1beta2.ResourceClaim{ObjectMeta: meta("gpu"), Status: resourcev1beta2.ResourceClaimStatus{Allocation: &resourcev1beta2.AllocationResult{
				Devices: resourcev1beta2.DeviceAllocationResult{Results: []resourcev1beta2.DeviceRequestAllocationResult{
					{Driver: "gpu.nvidia.com"}, {Driver: "compute-domain.nvidia.com"},
				}},
			}}},
			&resourcev1beta2.ResourceClaim{ObjectMeta: meta("pending")},
		}
	case "v1beta1":
		return []client.Object{
			&resourcev1beta1.ResourceClaim{ObjectMeta: meta("gpu"), Status: resourcev1beta1.ResourceClaimStatus{Allocation: &resourcev1beta1.AllocationResult{
				Devices: resourcev1beta1.DeviceAllocationResult{Results: []resourcev1beta1.DeviceRequestAllocationResult{
					{Driver: "gpu.nvidia.com"}, {Driver: "compute-domain.nvidia.com"},
				}},
			}}},
			&resourcev1beta1.ResourceClaim{ObjectMeta: meta("pending")},
		}
	default:
		return []client.Object{
			&resourcev1.ResourceClaim{ObjectMeta: meta("gpu"), Status: resourcev1.ResourceClaimStatus{Allocation: &resourcev1.AllocationResult{
				Devices: resourcev1.DeviceAllocationResult{Results: []resourcev1.DeviceRequestAllocationResult{
					{Driver: "gpu.nvidia.com"}, {Driver: "compute-domain.nvidia.com"},
				}},
			}}},
			&resourcev1.ResourceClaim{ObjectMeta: meta("pending")},
		}
	}
}

func TestClaimDrivers(t *testing.T) {
	cases := map[string]struct {
		want    []string
		wantErr error
	}{
		"gpu":     {want: []string{"gpu.nvidia.com", "compute-domain.nvidia.com"}},
		"pending": {want: []string{}},
		"missing": {wantErr: decision.ErrClaimNotFound},
	}
	for _, v := range resourceVersions {
		for name, tc := range cases {
			t.Run(v+"/"+name, func(t *testing.T) {
				c, _ := newClient(t, claims(v)...)
				c.DRAVersion = v
				drivers, err := c.ClaimDrivers(context.Background(), "default", name)
				if tc.wantErr != nil {
					require.ErrorIs(t, err, tc.wantErr)
					return
				}
				require.NoError(t, err)
				require.Equal(t, tc.want, drivers)
			})
		}
	}
	t.Run("no claim API", func(t *testing.T) {
		c, rec := newClient(t, claims("v1")...)
		c.DRAVersion = ""
		_, err := c.ClaimDrivers(context.Background(), "default", "gpu")
		require.ErrorIs(t, err, decision.ErrClaimNotFound)
		require.Empty(t, rec.gets)
	})
}

// slices returns a slice on node-1 and one on node-2 in version v.
func slices(v string) []client.Object {
	node1, node2 := "node-1", "node-2"
	meta := func(name string) metav1.ObjectMeta { return metav1.ObjectMeta{Name: name} }
	switch v {
	case "v1beta2":
		return []client.Object{
			&resourcev1beta2.ResourceSlice{ObjectMeta: meta("a"), Spec: resourcev1beta2.ResourceSliceSpec{Driver: "gpu.nvidia.com", NodeName: &node1}},
			&resourcev1beta2.ResourceSlice{ObjectMeta: meta("b"), Spec: resourcev1beta2.ResourceSliceSpec{Driver: "other.example.com", NodeName: &node2}},
		}
	case "v1beta1":
		return []client.Object{
			&resourcev1beta1.ResourceSlice{ObjectMeta: meta("a"), Spec: resourcev1beta1.ResourceSliceSpec{Driver: "gpu.nvidia.com", NodeName: node1}},
			&resourcev1beta1.ResourceSlice{ObjectMeta: meta("b"), Spec: resourcev1beta1.ResourceSliceSpec{Driver: "other.example.com", NodeName: node2}},
		}
	default:
		return []client.Object{
			&resourcev1.ResourceSlice{ObjectMeta: meta("a"), Spec: resourcev1.ResourceSliceSpec{Driver: "gpu.nvidia.com", NodeName: &node1}},
			&resourcev1.ResourceSlice{ObjectMeta: meta("b"), Spec: resourcev1.ResourceSliceSpec{Driver: "other.example.com", NodeName: &node2}},
		}
	}
}

func TestNodeSliceDrivers(t *testing.T) {
	cases := map[string]struct {
		version string
		want    []string
	}{
		"v1":           {version: "v1", want: []string{"gpu.nvidia.com"}},
		"v1beta2":      {version: "v1beta2", want: []string{"gpu.nvidia.com"}},
		"v1beta1":      {version: "v1beta1", want: []string{"gpu.nvidia.com"}},
		"no slice API": {version: ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c, rec := newClient(t, slices(tc.version)...)
			c.DRAVersion = tc.version
			drivers, err := c.NodeSliceDrivers(context.Background(), "node-1")
			require.NoError(t, err)
			require.Equal(t, tc.want, drivers)
			if tc.version == "" {
				require.Empty(t, rec.lists)
			}
		})
	}
}

// failingMapper fails every mapping with err.
type failingMapper struct {
	meta.RESTMapper
	err error
}

func (m failingMapper) RESTMapping(schema.GroupKind, ...string) (*meta.RESTMapping, error) {
	return nil, m.err
}

func TestServedDRAVersion(t *testing.T) {
	served := func(gvs map[string][]string) meta.RESTMapper {
		m := meta.NewDefaultRESTMapper(nil)
		for v, kinds := range gvs {
			for _, k := range kinds {
				m.Add(schema.GroupVersionKind{Group: resourceGroup, Version: v, Kind: k}, meta.RESTScopeNamespace)
			}
		}
		return m
	}
	both := []string{"ResourceClaim", "ResourceSlice"}
	cases := map[string]struct {
		mapper  meta.RESTMapper
		want    string
		wantErr bool
	}{
		"all served picks v1":   {mapper: served(map[string][]string{"v1": both, "v1beta2": both, "v1beta1": both}), want: "v1"},
		"v1beta2 and v1beta1":   {mapper: served(map[string][]string{"v1beta2": both, "v1beta1": both}), want: "v1beta2"},
		"only v1beta1":          {mapper: served(map[string][]string{"v1beta1": both}), want: "v1beta1"},
		"v1 serves only claims": {mapper: served(map[string][]string{"v1": {"ResourceClaim"}, "v1beta2": both}), want: "v1beta2"},
		"none served":           {mapper: served(nil), want: ""},
		"discovery fails":       {mapper: failingMapper{err: errors.New("connection refused")}, wantErr: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := ServedDRAVersion(tc.mapper)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

// The mapper the applier uses discovers lazily and reports a version that
// isn't served as a NoMatch error only after a 404 from the API server.
func TestServedDRAVersionWithLazyDiscovery(t *testing.T) {
	cases := map[string]struct {
		served []string
		want   string
	}{
		"resource.k8s.io not served": {want: ""},
		"only v1beta1 served":        {served: []string{"v1beta1"}, want: "v1beta1"},
		"all served":                 {served: []string{"v1", "v1beta2", "v1beta1"}, want: "v1"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(discoveryHandler(tc.served))
			defer srv.Close()
			mapper, err := apiutil.NewDynamicRESTMapper(&rest.Config{Host: srv.URL}, srv.Client())
			require.NoError(t, err)

			got, err := ServedDRAVersion(mapper)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

// discoveryHandler serves legacy discovery for apps/v1 and the given
// resource.k8s.io versions.
func discoveryHandler(draVersions []string) http.Handler {
	groups := &metav1.APIGroupList{Groups: []metav1.APIGroup{{
		Name:     "apps",
		Versions: []metav1.GroupVersionForDiscovery{{GroupVersion: "apps/v1", Version: "v1"}},
	}}}
	resources := map[string]*metav1.APIResourceList{}
	if len(draVersions) > 0 {
		g := metav1.APIGroup{Name: resourceGroup}
		for _, v := range draVersions {
			gv := resourceGroup + "/" + v
			g.Versions = append(g.Versions, metav1.GroupVersionForDiscovery{GroupVersion: gv, Version: v})
			resources["/apis/"+gv] = &metav1.APIResourceList{GroupVersion: gv, APIResources: []metav1.APIResource{
				{Name: "resourceclaims", Namespaced: true, Kind: "ResourceClaim", Verbs: metav1.Verbs{"get"}},
				{Name: "resourceslices", Kind: "ResourceSlice", Verbs: metav1.Verbs{"list"}},
			}}
		}
		g.PreferredVersion = g.Versions[0]
		groups.Groups = append(groups.Groups, g)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body any
		switch r.URL.Path {
		case "/api":
			body = &metav1.APIVersions{Versions: []string{"v1"}}
		case "/apis":
			body = groups
		default:
			list, ok := resources[r.URL.Path]
			if !ok {
				http.NotFound(w, r)
				return
			}
			body = list
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	})
}

func TestGateReadsComeFromTheCache(t *testing.T) {
	c, rec := newClient(t,
		&appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "applier", Namespace: "gpu-operator"}},
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"a": "b"}}},
	)
	ctx := context.Background()

	exists, err := c.DaemonSetExists(ctx, "gpu-operator", "applier")
	require.NoError(t, err)
	require.True(t, exists)
	exists, err = c.DaemonSetExists(ctx, "gpu-operator", "missing")
	require.NoError(t, err)
	require.False(t, exists)

	labels, err := c.NodeLabels(ctx, "node-1")
	require.NoError(t, err)
	require.Equal(t, map[string]string{"a": "b"}, labels)

	for _, o := range rec.gets {
		require.Equal(t, "0", o.ResourceVersion)
	}
}

func TestDaemonSetExistsError(t *testing.T) {
	c := &Client{C: fake.NewClientBuilder().WithScheme(NewScheme()).WithInterceptorFuncs(interceptor.Funcs{
		Get: func(context.Context, client.WithWatch, client.ObjectKey, client.Object, ...client.GetOption) error {
			return errors.New("forbidden")
		},
	}).Build()}
	_, err := c.DaemonSetExists(context.Background(), "gpu-operator", "applier")
	require.ErrorContains(t, err, "forbidden")
}

func TestListDaemonSets(t *testing.T) {
	ds := func(name, ns, component string) *appsv1.DaemonSet {
		return &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns,
			Labels: map[string]string{"app.kubernetes.io/component": component}}}
	}
	cases := map[string]struct {
		list   func(*Client, context.Context, string, string) ([]appsv1.DaemonSet, error)
		wantRV string
	}{
		"quorum":     {list: (*Client).ListDaemonSets},
		"from cache": {list: (*Client).ListDaemonSetsFromCache, wantRV: "0"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c, rec := newClient(t,
				ds("driver", "gpu-operator", "nvidia-driver"),
				ds("plugin", "gpu-operator", "nvidia-device-plugin"),
				ds("driver-elsewhere", "other", "nvidia-driver"),
			)
			got, err := tc.list(c, context.Background(), "gpu-operator", "app.kubernetes.io/component=nvidia-driver")
			require.NoError(t, err)
			require.Len(t, got, 1)
			require.Equal(t, "driver", got[0].Name)
			require.Len(t, rec.lists, 1)
			require.Equal(t, tc.wantRV, rec.lists[0].ResourceVersion)

			_, err = tc.list(c, context.Background(), "gpu-operator", "a in (")
			require.Error(t, err)
		})
	}
}

func TestPatchNodeLabels(t *testing.T) {
	c, _ := newClient(t, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"keep": "me"}}})
	require.NoError(t, c.PatchNodeLabels(context.Background(), "node-1", map[string]string{"nvidia.com/gpu-config.sync": "in-sync"}))
	node, err := c.GetNode(context.Background(), "node-1")
	require.NoError(t, err)
	require.Equal(t, map[string]string{"keep": "me", "nvidia.com/gpu-config.sync": "in-sync"}, node.Labels)
}

func TestNewClients(t *testing.T) {
	const timeout = 200 * time.Millisecond
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/nodes/stalled":
			<-r.Context().Done()
		case r.URL.Path == "/api/v1/namespaces/gpu-operator/configmaps" && r.URL.Query().Get("watch") == "true":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(3 * timeout):
			}
			_, _ = fmt.Fprintln(w, `{"type":"ADDED","object":{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"gpu-config","namespace":"gpu-operator","resourceVersion":"2"}}}`)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	mapper := meta.NewDefaultRESTMapper(nil)
	mapper.Add(corev1.SchemeGroupVersion.WithKind("Node"), meta.RESTScopeRoot)
	mapper.Add(corev1.SchemeGroupVersion.WithKind("ConfigMap"), meta.RESTScopeNamespace)
	c, wc, err := NewClients(&rest.Config{Host: srv.URL}, timeout, client.Options{Scheme: NewScheme(), Mapper: mapper})
	require.NoError(t, err)

	start := time.Now()
	_, err = (&Client{C: c}).GetNode(context.Background(), "stalled")
	require.Error(t, err)
	require.Less(t, time.Since(start), 5*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wi, err := wc.Watch(ctx, &corev1.ConfigMapList{}, client.InNamespace(ns))
	require.NoError(t, err)
	defer wi.Stop()
	select {
	case ev, ok := <-wi.ResultChan():
		require.True(t, ok, "the watch ended before its first event")
		require.Equal(t, watch.Added, ev.Type)
		require.Equal(t, "gpu-config", ev.Object.(*corev1.ConfigMap).Name)
	case <-time.After(5 * time.Second):
		t.Fatal("no watch event")
	}
}
