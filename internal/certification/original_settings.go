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

package certification

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"reflect"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/NVIDIA/gpu-operator/internal/certification/k8s"
)

// crClient is the part of *k8s.Clients that captures and restores the CRs.
type crClient interface {
	GetClusterPolicyUnstructured(ctx context.Context) (*unstructured.Unstructured, error)
	ListNVIDIADriversUnstructured(ctx context.Context) ([]unstructured.Unstructured, error)
	ModifyClusterPolicy(ctx context.Context, patchJSON []byte) error
	ModifyNVIDIADriver(ctx context.Context, name string, patchJSON []byte) error
}

// OriginalSettings holds the initial ClusterPolicy and NVIDIADriver CRs so they
// can be restored after each TestSet. They are kept as the API server returned
// them: the typed specs would drop explicit zero values from omitempty fields,
// such as upgradePolicy.autoUpgrade: false, and the API server would replace
// those with their CRD defaults on restore.
type OriginalSettings struct {
	clusterPolicy *unstructured.Unstructured
	drivers       []unstructured.Unstructured
}

// CaptureOriginalSettings saves the current ClusterPolicy and NVIDIADriver CR specs.
// It retries on transient API-server errors until the timeout expires, preventing a
// momentary failure from silently disabling restoration for the entire suite.
func CaptureOriginalSettings(ctx context.Context, clients *k8s.Clients, timeout time.Duration) (*OriginalSettings, error) {
	var s *OriginalSettings
	err := clients.WaitForCondition(ctx, timeout, func(ctx context.Context) (bool, error) {
		var captureErr error
		s, captureErr = captureSettings(ctx, clients)
		if captureErr != nil {
			log.Printf("  original settings: capture failed (retrying): %v", captureErr)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return nil, fmt.Errorf("capturing original settings: %w", err)
	}
	return s, nil
}

func captureSettings(ctx context.Context, c crClient) (*OriginalSettings, error) {
	s := &OriginalSettings{}

	cp, err := c.GetClusterPolicyUnstructured(ctx)
	if err != nil {
		return nil, err
	}
	s.clusterPolicy = cp

	drivers, err := c.ListNVIDIADriversUnstructured(ctx)
	if err != nil {
		// NVIDIADriver CRD may not exist — not an error
		log.Printf("  original settings: no NVIDIADriver CRs to capture: %v", err)
	} else {
		s.drivers = drivers
	}
	return s, nil
}

// Restore patches ClusterPolicy and NVIDIADriver CRs back to their original specs.
// Only patches resources whose spec has changed. If anything was restored, restarts
// driver pods (OnDelete strategy) when driver-related fields changed, then polls
// ClusterPolicy status to confirm the operator has fully reconciled.
func (s *OriginalSettings) Restore(ctx context.Context, clients *k8s.Clients, namespace string, timeout time.Duration) {
	driverSpecChanged := s.restoreClusterPolicy(ctx, clients)
	driverCRChanged := s.restoreNVIDIADrivers(ctx, clients)

	// Restart driver pods if driver-related config changed. Driver DaemonSets
	// use OnDelete update strategy so the restored spec won't take effect until
	// old pods are deleted. Without this, the next TestSet inherits stale driver
	// pods (e.g. with RDMA/GDRCopy still enabled, or wrong driver version).
	driverRestart := driverSpecChanged || driverCRChanged
	if driverRestart {
		log.Println("  original settings: deleting driver pods to apply restored config")
		if err := clients.DeletePodsByLabel(ctx, namespace, k8s.DriverLabelSelector); err != nil {
			log.Printf("  original settings: failed to delete driver pods: %v", err)
		}
		if err := clients.WaitForPodsReady(ctx, namespace, k8s.DriverLabelSelector, timeout); err != nil {
			log.Printf("  original settings: driver pods not ready after restore: %v", err)
		}
	}

	// Wait for the operator to finish reconciling before returning.
	// Tests may trigger pod restarts via side-effects (e.g. MIG node label
	// changes) that don't show up as spec diffs, so this runs unconditionally.
	log.Println("  original settings: waiting for operator to begin reconciliation")
	_ = clients.WaitForClusterPolicyNotReady(ctx, 20*time.Second)

	log.Println("  original settings: waiting for cluster to stabilize")
	if err := clients.WaitForClusterPolicyReady(ctx, timeout); err != nil {
		log.Printf("  original settings: ClusterPolicy not ready after restore: %v", err)
	}

	log.Println("  original settings: waiting for GFD labels to be present on all GPU nodes")
	if err := clients.WaitForGFDLabels(ctx, 2*time.Minute); err != nil {
		log.Printf("  original settings: GFD labels not present after restore: %v", err)
	}
}

// restoreClusterPolicy patches the ClusterPolicy back to its original spec.
// Returns whether driver-related fields changed.
func (s *OriginalSettings) restoreClusterPolicy(ctx context.Context, c crClient) (driverRelated bool) {
	if s.clusterPolicy == nil {
		return false
	}

	current, err := c.GetClusterPolicyUnstructured(ctx)
	if err != nil {
		log.Printf("  original settings: failed to get current ClusterPolicy: %v", err)
		return false
	}
	if reflect.DeepEqual(current.Object["spec"], s.clusterPolicy.Object["spec"]) {
		return false
	}

	// Detect whether driver-related fields changed. Driver DaemonSets use
	// OnDelete update strategy, so spec changes only take effect after pods
	// are explicitly deleted.
	driverRelated = specFieldChanged(current, s.clusterPolicy, "driver") ||
		specFieldChanged(current, s.clusterPolicy, "gdrcopy") ||
		specFieldChanged(current, s.clusterPolicy, "gds")

	log.Println("  original settings: restoring ClusterPolicy spec")
	patch, err := specReplacePatch(s.clusterPolicy)
	if err != nil {
		log.Printf("  original settings: failed to marshal ClusterPolicy spec: %v", err)
		return false
	}
	if err := c.ModifyClusterPolicy(ctx, patch); err != nil {
		log.Printf("  original settings: failed to restore ClusterPolicy: %v", err)
		return false
	}
	return driverRelated
}

// restoreNVIDIADrivers patches NVIDIADriver CRs back to their original specs.
// Returns whether any CR was patched. The entire NVIDIADriver spec is driver-related,
// so patched == driverRelated.
func (s *OriginalSettings) restoreNVIDIADrivers(ctx context.Context, c crClient) (patched bool) {
	if len(s.drivers) == 0 {
		return false
	}

	currentDrivers, err := c.ListNVIDIADriversUnstructured(ctx)
	if err != nil {
		log.Printf("  original settings: failed to get current NVIDIADriver CRs: %v", err)
		return false
	}

	currentByName := make(map[string]unstructured.Unstructured, len(currentDrivers))
	for _, d := range currentDrivers {
		currentByName[d.GetName()] = d
	}

	for _, saved := range s.drivers {
		current, exists := currentByName[saved.GetName()]
		if !exists || reflect.DeepEqual(current.Object["spec"], saved.Object["spec"]) {
			continue
		}

		log.Printf("  original settings: restoring NVIDIADriver %q spec", saved.GetName())
		patch, err := specReplacePatch(&saved)
		if err != nil {
			log.Printf("  original settings: failed to marshal NVIDIADriver %q spec: %v", saved.GetName(), err)
			continue
		}

		if err := c.ModifyNVIDIADriver(ctx, saved.GetName(), patch); err != nil {
			log.Printf("  original settings: failed to restore NVIDIADriver %q: %v", saved.GetName(), err)
		} else {
			patched = true
		}
	}
	return patched
}

// specReplacePatch returns a JSON patch that replaces the spec with the one in obj.
func specReplacePatch(obj *unstructured.Unstructured) ([]byte, error) {
	return json.Marshal([]map[string]any{
		{"op": "replace", "path": "/spec", "value": obj.Object["spec"]},
	})
}

// specFieldChanged reports whether the spec field differs between the two objects.
func specFieldChanged(a, b *unstructured.Unstructured, field string) bool {
	aValue, _, _ := unstructured.NestedFieldNoCopy(a.Object, "spec", field)
	bValue, _, _ := unstructured.NestedFieldNoCopy(b.Object, "spec", field)
	return !reflect.DeepEqual(aValue, bValue)
}
