package certification

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"reflect"
	"time"

	nvidiav1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"

	"github.com/NVIDIA/gpu-operator/internal/certification/k8s"
)

// OriginalSettings holds the initial ClusterPolicy and NVIDIADriver CR specs
// so they can be restored after each TestSet.
type OriginalSettings struct {
	clusterPolicy *nvidiav1.ClusterPolicy
	drivers       []nvidiav1alpha1.NVIDIADriver
}

// CaptureOriginalSettings saves the current ClusterPolicy and NVIDIADriver CR specs.
// It retries on transient API-server errors until the timeout expires, preventing a
// momentary failure from silently disabling restoration for the entire suite.
func CaptureOriginalSettings(ctx context.Context, clients *k8s.Clients, timeout time.Duration) (*OriginalSettings, error) {
	var s *OriginalSettings
	err := clients.WaitForCondition(ctx, timeout, func(ctx context.Context) (bool, error) {
		s = &OriginalSettings{}

		cp, cpErr := clients.GetClusterPolicy(ctx)
		if cpErr != nil {
			log.Printf("  original settings: capture failed (retrying): %v", cpErr)
			return false, nil
		}
		s.clusterPolicy = cp

		drivers, driversErr := clients.ListNVIDIADrivers(ctx)
		if driversErr != nil {
			// NVIDIADriver CRD may not exist — not an error
			log.Printf("  original settings: no NVIDIADriver CRs to capture: %v", driversErr)
		} else {
			s.drivers = drivers
		}

		return true, nil
	})
	if err != nil {
		return nil, fmt.Errorf("capturing original settings: %w", err)
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
func (s *OriginalSettings) restoreClusterPolicy(ctx context.Context, clients *k8s.Clients) (driverRelated bool) {
	if s.clusterPolicy == nil {
		return false
	}

	current, err := clients.GetClusterPolicy(ctx)
	if err != nil {
		log.Printf("  original settings: failed to get current ClusterPolicy: %v", err)
		return false
	}
	if reflect.DeepEqual(current.Spec, s.clusterPolicy.Spec) {
		return false
	}

	// Detect whether driver-related fields changed. Driver DaemonSets use
	// OnDelete update strategy, so spec changes only take effect after pods
	// are explicitly deleted.
	driverRelated = !reflect.DeepEqual(current.Spec.Driver, s.clusterPolicy.Spec.Driver) ||
		!reflect.DeepEqual(current.Spec.GDRCopy, s.clusterPolicy.Spec.GDRCopy) ||
		!reflect.DeepEqual(current.Spec.GPUDirectStorage, s.clusterPolicy.Spec.GPUDirectStorage)

	log.Println("  original settings: restoring ClusterPolicy spec")
	specJSON, err := json.Marshal(s.clusterPolicy.Spec)
	if err != nil {
		log.Printf("  original settings: failed to marshal ClusterPolicy spec: %v", err)
		return false
	}

	patch, _ := json.Marshal([]map[string]interface{}{
		{"op": "replace", "path": "/spec", "value": json.RawMessage(specJSON)},
	})
	if err := clients.ModifyClusterPolicy(ctx, patch); err != nil {
		log.Printf("  original settings: failed to restore ClusterPolicy: %v", err)
		return false
	}
	return driverRelated
}

// restoreNVIDIADrivers patches NVIDIADriver CRs back to their original specs.
// Returns whether any CR was patched. The entire NVIDIADriver spec is driver-related,
// so patched == driverRelated.
func (s *OriginalSettings) restoreNVIDIADrivers(ctx context.Context, clients *k8s.Clients) (patched bool) {
	if len(s.drivers) == 0 {
		return false
	}

	currentDrivers, err := clients.ListNVIDIADrivers(ctx)
	if err != nil {
		log.Printf("  original settings: failed to get current NVIDIADriver CRs: %v", err)
		return false
	}

	currentByName := make(map[string]nvidiav1alpha1.NVIDIADriver, len(currentDrivers))
	for _, d := range currentDrivers {
		currentByName[d.Name] = d
	}

	for _, saved := range s.drivers {
		current, exists := currentByName[saved.Name]
		if !exists || reflect.DeepEqual(current.Spec, saved.Spec) {
			continue
		}

		log.Printf("  original settings: restoring NVIDIADriver %q spec", saved.Name)
		specJSON, err := json.Marshal(saved.Spec)
		if err != nil {
			log.Printf("  original settings: failed to marshal NVIDIADriver %q spec: %v", saved.Name, err)
			continue
		}

		patch, _ := json.Marshal([]map[string]interface{}{
			{"op": "replace", "path": "/spec", "value": json.RawMessage(specJSON)},
		})
		if err := clients.ModifyNVIDIADriver(ctx, saved.Name, patch); err != nil {
			log.Printf("  original settings: failed to restore NVIDIADriver %q: %v", saved.Name, err)
		} else {
			patched = true
		}
	}
	return patched
}
