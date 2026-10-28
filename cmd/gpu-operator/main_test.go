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

package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func allocatedClaim(name, driver string) *resourcev1.ResourceClaim {
	claim := &resourcev1.ResourceClaim{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
	}
	if driver != "" {
		claim.Status.Allocation = &resourcev1.AllocationResult{
			Devices: resourcev1.DeviceAllocationResult{
				Results: []resourcev1.DeviceRequestAllocationResult{
					{Request: "gpu", Driver: driver, Pool: "pool", Device: "gpu-0"},
				},
			},
		}
	}
	return claim
}

func claimPod(phase corev1.PodPhase, claims ...corev1.PodResourceClaim) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "default"},
		Spec:       corev1.PodSpec{ResourceClaims: claims},
		Status:     corev1.PodStatus{Phase: phase},
	}
}

func TestGPUPodSpecFilterResourceClaims(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))

	testCases := []struct {
		name     string
		objs     []runtime.Object
		pod      corev1.Pod
		expected bool
	}{
		{
			name:     "pod without GPU resources or claims",
			pod:      claimPod(corev1.PodRunning),
			expected: false,
		},
		{
			name: "pod with a claim allocated by the NVIDIA GPU DRA driver",
			objs: []runtime.Object{allocatedClaim("gpu-claim", "gpu.nvidia.com")},
			pod: claimPod(corev1.PodRunning,
				corev1.PodResourceClaim{Name: "gpu", ResourceClaimName: new("gpu-claim")}),
			expected: true,
		},
		{
			name: "pod with an admin-access claim is not a GPU pod",
			objs: []runtime.Object{func() *resourcev1.ResourceClaim {
				claim := allocatedClaim("admin-claim", "gpu.nvidia.com")
				claim.Status.Allocation.Devices.Results[0].AdminAccess = new(true)
				return claim
			}()},
			pod: claimPod(corev1.PodRunning,
				corev1.PodResourceClaim{Name: "gpu", ResourceClaimName: new("admin-claim")}),
			expected: false,
		},
		{
			name: "pod with a claim allocated by another DRA driver",
			objs: []runtime.Object{allocatedClaim("nic-claim", "net.example.com")},
			pod: claimPod(corev1.PodRunning,
				corev1.PodResourceClaim{Name: "nic", ResourceClaimName: new("nic-claim")}),
			expected: false,
		},
		{
			name: "pod with an unallocated claim",
			objs: []runtime.Object{allocatedClaim("pending-claim", "")},
			pod: claimPod(corev1.PodRunning,
				corev1.PodResourceClaim{Name: "gpu", ResourceClaimName: new("pending-claim")}),
			expected: false,
		},
		{
			name: "pod with a template-generated claim resolved via status",
			objs: []runtime.Object{allocatedClaim("pod-gpu-claim", "gpu.nvidia.com")},
			pod: func() corev1.Pod {
				p := claimPod(corev1.PodRunning,
					corev1.PodResourceClaim{Name: "gpu", ResourceClaimTemplateName: new("single-gpu")})
				p.Status.ResourceClaimStatuses = []corev1.PodResourceClaimStatus{
					{Name: "gpu", ResourceClaimName: new("pod-gpu-claim")},
				}
				return p
			}(),
			expected: true,
		},
		{
			name: "unresolvable claim counts as a GPU pod",
			pod: claimPod(corev1.PodRunning,
				corev1.PodResourceClaim{Name: "gpu", ResourceClaimName: new("missing-claim")}),
			expected: true,
		},
		{
			name: "completed pod with a GPU claim is ignored",
			objs: []runtime.Object{allocatedClaim("gpu-claim", "gpu.nvidia.com")},
			pod: claimPod(corev1.PodSucceeded,
				corev1.PodResourceClaim{Name: "gpu", ResourceClaimName: new("gpu-claim")}),
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(tc.objs...).Build()
			require.Equal(t, tc.expected, gpuPodSpecFilter(t.Context(), c)(tc.pod))
		})
	}
}

func resourcePod(phase corev1.PodPhase, containers ...corev1.Container) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "default"},
		Spec:       corev1.PodSpec{Containers: containers},
		Status:     corev1.PodStatus{Phase: phase},
	}
}

func gpuContainer(limits, requests corev1.ResourceList) corev1.Container {
	return corev1.Container{Resources: corev1.ResourceRequirements{Limits: limits, Requests: requests}}
}

func nativeSidecar(limits, requests corev1.ResourceList) corev1.Container {
	ctr := gpuContainer(limits, requests)
	ctr.RestartPolicy = new(corev1.ContainerRestartPolicyAlways)
	return ctr
}

func podWithInitContainer(phase corev1.PodPhase, initContainer corev1.Container) corev1.Pod {
	pod := resourcePod(phase, gpuContainer(corev1.ResourceList{"cpu": {}}, nil))
	pod.Spec.InitContainers = []corev1.Container{initContainer}
	return pod
}

// TestGPUPodSpecFilterResourceList covers the container resource-list branch of
// gpuPodSpecFilter (nvidia.com/gpu and nvidia.com/mig- in app/native sidecar
// limits/requests, plus the Running/Pending phase gate). The sibling
// TestGPUPodSpecFilterResourceClaims only exercises the DRA/ResourceClaims
// branch, leaving this branch uncovered.
//
// gpuPodSpecFilter returns true when a pod should be treated as a GPU pod, so
// each case's wantGPUPod field states the expected classification.
func TestGPUPodSpecFilterResourceList(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))

	testCases := []struct {
		name       string
		pod        corev1.Pod
		wantGPUPod bool
	}{
		{
			name:       "running pod requesting nvidia.com/gpu in limits -> GPU pod",
			pod:        resourcePod(corev1.PodRunning, gpuContainer(corev1.ResourceList{"nvidia.com/gpu": {}}, nil)),
			wantGPUPod: true,
		},
		{
			name:       "pending pod requesting nvidia.com/gpu in requests -> GPU pod",
			pod:        resourcePod(corev1.PodPending, gpuContainer(nil, corev1.ResourceList{"nvidia.com/gpu": {}})),
			wantGPUPod: true,
		},
		{
			name:       "running pod requesting an nvidia.com/mig- resource -> GPU pod",
			pod:        resourcePod(corev1.PodRunning, gpuContainer(corev1.ResourceList{"nvidia.com/mig-1g.5gb": {}}, nil)),
			wantGPUPod: true,
		},
		{
			name: "running pod where only a later container requests a gpu -> GPU pod",
			pod: resourcePod(corev1.PodRunning,
				gpuContainer(corev1.ResourceList{"cpu": {}}, nil),
				gpuContainer(corev1.ResourceList{"nvidia.com/gpu": {}}, nil)),
			wantGPUPod: true,
		},
		{
			name:       "running pod requesting only cpu/memory -> not a GPU pod",
			pod:        resourcePod(corev1.PodRunning, gpuContainer(corev1.ResourceList{"cpu": {}, "memory": {}}, nil)),
			wantGPUPod: false,
		},
		{
			name:       "succeeded pod requesting a gpu -> not a GPU pod (phase gate rejects it)",
			pod:        resourcePod(corev1.PodSucceeded, gpuContainer(corev1.ResourceList{"nvidia.com/gpu": {}}, nil)),
			wantGPUPod: false,
		},
		{
			name:       "running pod with GPU limit only in native sidecar -> GPU pod",
			pod:        podWithInitContainer(corev1.PodRunning, nativeSidecar(corev1.ResourceList{"nvidia.com/gpu": {}}, nil)),
			wantGPUPod: true,
		},
		{
			name:       "pending pod with GPU request only in native sidecar -> GPU pod",
			pod:        podWithInitContainer(corev1.PodPending, nativeSidecar(nil, corev1.ResourceList{"nvidia.com/gpu": {}})),
			wantGPUPod: true,
		},
		{
			name:       "running pod with MIG request only in native sidecar -> GPU pod",
			pod:        podWithInitContainer(corev1.PodRunning, nativeSidecar(nil, corev1.ResourceList{"nvidia.com/mig-1g.5gb": {}})),
			wantGPUPod: true,
		},
		{
			name:       "running pod with GPU limit only in regular init container -> not a GPU pod",
			pod:        podWithInitContainer(corev1.PodRunning, gpuContainer(corev1.ResourceList{"nvidia.com/gpu": {}}, nil)),
			wantGPUPod: false,
		},
		{
			name:       "running pod with CPU only in native sidecar -> not a GPU pod",
			pod:        podWithInitContainer(corev1.PodRunning, nativeSidecar(corev1.ResourceList{"cpu": {}}, nil)),
			wantGPUPod: false,
		},
		{
			name:       "succeeded pod with GPU limit only in native sidecar -> not a GPU pod",
			pod:        podWithInitContainer(corev1.PodSucceeded, nativeSidecar(corev1.ResourceList{"nvidia.com/gpu": {}}, nil)),
			wantGPUPod: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			c := fake.NewClientBuilder().WithScheme(scheme).Build()

			gotGPUPod := gpuPodSpecFilter(t.Context(), c)(tc.pod)

			require.Equal(t, tc.wantGPUPod, gotGPUPod)
		})
	}
}

// Model the Helm Role: Services can only be listed in the operator namespace.
func TestServiceCacheNamespaceScope(t *testing.T) {
	const namespace = "test-operator"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var mu sync.Mutex
	var paths []string
	opts := operatorCacheOptions(namespace)
	opts.Scheme = scheme
	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{corev1.SchemeGroupVersion})
	mapper.Add(corev1.SchemeGroupVersion.WithKind("Service"), meta.RESTScopeNamespace)
	opts.Mapper = mapper
	opts.HTTPClient = &http.Client{Transport: cacheTestTransport(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		paths = append(paths, req.URL.Path)
		mu.Unlock()
		if req.URL.Path != "/api/v1/namespaces/"+namespace+"/services" {
			return &http.Response{StatusCode: http.StatusForbidden, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Forbidden","code":403}`))}, nil
		}
		if req.URL.Query().Get("watch") == "true" {
			reader, writer := io.Pipe()
			go func() {
				defer writer.Close()
				if req.URL.Query().Get("sendInitialEvents") == "true" {
					_, _ = io.WriteString(writer, `{"type":"BOOKMARK","object":{"kind":"Service","apiVersion":"v1","metadata":{"resourceVersion":"1","annotations":{"k8s.io/initial-events-end":"true"}}}}`+"\n")
				}
				<-req.Context().Done()
			}()
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: reader}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"kind":"ServiceList","apiVersion":"v1","metadata":{"resourceVersion":"1"},"items":[]}`))}, nil
	})}
	serviceCache, err := cache.New(&rest.Config{Host: "https://cache.test"}, opts)
	require.NoError(t, err)
	_, err = serviceCache.GetInformer(ctx, &corev1.Service{}, cache.BlockUntilSynced(false))
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- serviceCache.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-done)
	})
	require.True(t, serviceCache.WaitForCacheSync(ctx), "Service informer must sync with namespace-scoped RBAC")
	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, paths)
	for _, path := range paths {
		require.Equal(t, fmt.Sprintf("/api/v1/namespaces/%s/services", namespace), path)
	}
}

type cacheTestTransport func(*http.Request) (*http.Response, error)

func (f cacheTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
