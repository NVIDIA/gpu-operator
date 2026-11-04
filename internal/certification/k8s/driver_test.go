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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// newTestClients returns Clients backed by an API server stub that serves one
// NVIDIADriver CR and answers node list requests with nodesStatus/nodesBody.
func newTestClients(t *testing.T, nodesStatus int, nodesBody string) *Clients {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/nvidiadrivers"):
			_, _ = w.Write([]byte(`{"apiVersion":"nvidia.com/v1alpha1","kind":"NVIDIADriverList","items":[` +
				`{"apiVersion":"nvidia.com/v1alpha1","kind":"NVIDIADriver","metadata":{"name":"default"},"spec":{}}]}`))
		case strings.HasSuffix(r.URL.Path, "/nodes"):
			w.WriteHeader(nodesStatus)
			_, _ = w.Write([]byte(nodesBody))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	cfg := &rest.Config{Host: srv.URL}
	k8sClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	dynClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return &Clients{k8s: k8sClient, dynamic: dynClient, restConfig: cfg}
}

func TestGDRCopyEligibleNodesReturnsListNodesError(t *testing.T) {
	c := newTestClients(t, http.StatusInternalServerError,
		`{"kind":"Status","apiVersion":"v1","status":"Failure","message":"boom","reason":"InternalError","code":500}`)

	nodes, err := c.GDRCopyEligibleNodes(context.Background())
	if err == nil {
		t.Fatalf("expected an error when listing nodes fails, got nodes=%v", nodes)
	}
	if nodes != nil {
		t.Fatalf("expected no nodes on error, got %v", nodes)
	}
}

func TestGDRCopyEligibleNodesNVIDIADriverMode(t *testing.T) {
	c := newTestClients(t, http.StatusOK,
		`{"apiVersion":"v1","kind":"NodeList","items":[{"metadata":{"name":"gpu-node-1"}}]}`)

	nodes, err := c.GDRCopyEligibleNodes(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !nodes["gpu-node-1"] || len(nodes) != 1 {
		t.Fatalf("expected only gpu-node-1, got %v", nodes)
	}
}
