/*
 * Copyright (c) 2024, NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package controllers

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	"github.com/NVIDIA/gpu-operator/internal/conditions"
	"github.com/NVIDIA/gpu-operator/internal/licenseinfo"
)

const (
	licenseExpiryWarningWindow = 14 * 24 * time.Hour
	licenseIssueLimit          = 3
)

type nodeLicenseSnapshot struct {
	node              string
	annotationPresent bool
	snapshot          *licenseinfo.Snapshot
	err               error
}

func (r *ClusterPolicyReconciler) syncLicenseCondition(ctx context.Context, instance *gpuv1.ClusterPolicy) error {
	condition, err := r.buildLicenseCondition(ctx, instance)
	if condition == nil {
		return err
	}
	if setErr := r.setCustomCondition(ctx, instance, *condition); setErr != nil {
		return setErr
	}
	return err
}

func (r *ClusterPolicyReconciler) buildLicenseCondition(ctx context.Context, instance *gpuv1.ClusterPolicy) (*metav1.Condition, error) {
	nodes := &corev1.NodeList{}
	opts := []client.ListOption{
		client.MatchingLabels{
			commonGPULabelKey:         commonGPULabelValue,
			gpuWorkloadConfigLabelKey: gpuWorkloadConfigVMVgpu,
		},
	}

	if err := r.Client.List(ctx, nodes, opts...); err != nil {
		cond := &metav1.Condition{
			Type:    conditions.Licensed,
			Status:  metav1.ConditionUnknown,
			Reason:  conditions.LicenseCollectionFailed,
			Message: fmt.Sprintf("failed to list vGPU nodes: %v", err),
		}
		return cond, err
	}

	if len(nodes.Items) == 0 {
		return &metav1.Condition{
			Type:    conditions.Licensed,
			Status:  metav1.ConditionTrue,
			Reason:  conditions.LicenseNotRequired,
			Message: "No vm-vgpu nodes detected; licensing not required",
		}, nil
	}

	snapshots := make([]nodeLicenseSnapshot, 0, len(nodes.Items))
	for _, node := range nodes.Items {
		annotation := ""
		if node.Annotations != nil {
			annotation = node.Annotations[licenseinfo.AnnotationKey]
		}
		snap := nodeLicenseSnapshot{
			node:              node.Name,
			annotationPresent: annotation != "",
		}
		if annotation == "" {
			snap.err = fmt.Errorf("license annotation missing")
		} else {
			parsed, err := licenseinfo.Parse(annotation)
			if err != nil {
				snap.err = fmt.Errorf("%w", err)
			} else {
				if parsed.Error != "" {
					snap.err = fmt.Errorf("%s", parsed.Error)
				}
				snap.snapshot = &parsed
			}
		}
		snapshots = append(snapshots, snap)
	}

	cond := summarizeLicenseSnapshots(snapshots, time.Now())
	cond.Type = conditions.Licensed
	return &cond, nil
}

func (r *ClusterPolicyReconciler) setCustomCondition(ctx context.Context, instance *gpuv1.ClusterPolicy, condition metav1.Condition) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &gpuv1.ClusterPolicy{}
		if err := r.Get(ctx, types.NamespacedName{Name: instance.Name}, current); err != nil {
			return err
		}
		condition.ObservedGeneration = current.Generation
		meta.SetStatusCondition(&current.Status.Conditions, condition)
		return r.Client.Status().Update(ctx, current)
	})
}

func summarizeLicenseSnapshots(snapshots []nodeLicenseSnapshot, now time.Time) metav1.Condition {
	var (
		unlicensed     []string
		expiringSoon   []string
		dataIssues     []string
		licensedCount  int
		reportedNodes  int
		earliestExpiry *time.Time
	)

	for _, snap := range snapshots {
		if !snap.annotationPresent {
			dataIssues = append(dataIssues, fmt.Sprintf("%s: annotation missing", snap.node))
			continue
		}
		if snap.snapshot == nil {
			dataIssues = append(dataIssues, fmt.Sprintf("%s: %v", snap.node, snap.err))
			continue
		}
		reportedNodes++

		if snap.err != nil {
			dataIssues = append(dataIssues, fmt.Sprintf("%s: %v", snap.node, snap.err))
		}

		if len(snap.snapshot.Devices) == 0 {
			dataIssues = append(dataIssues, fmt.Sprintf("%s: no vGPU devices reported", snap.node))
			continue
		}

		for _, device := range snap.snapshot.Devices {
			descriptor := formatDeviceDescriptor(snap.node, device)
			if !device.Licensed {
				unlicensed = append(unlicensed, fmt.Sprintf("%s (%s)", descriptor, safeStatus(device.Status)))
				continue
			}
			if device.Expiry != nil {
				expiry := device.Expiry.UTC()
				if earliestExpiry == nil || expiry.Before(*earliestExpiry) {
					exp := expiry
					earliestExpiry = &exp
				}
				if expiry.Before(now) {
					unlicensed = append(unlicensed, fmt.Sprintf("%s (expired %s)", descriptor, expiry.Format(time.RFC3339)))
					continue
				}
				if expiry.Sub(now) <= licenseExpiryWarningWindow {
					expiringSoon = append(expiringSoon, fmt.Sprintf("%s (expires %s)", descriptor, expiry.Format(time.RFC3339)))
					continue
				}
			}
			licensedCount++
		}
	}

	switch {
	case len(unlicensed) > 0:
		return metav1.Condition{
			Status:  metav1.ConditionFalse,
			Reason:  conditions.LicenseNotReady,
			Message: summarizeIssues("Unlicensed or expired vGPU devices", unlicensed),
		}
	case len(expiringSoon) > 0:
		return metav1.Condition{
			Status:  metav1.ConditionFalse,
			Reason:  conditions.LicenseExpiringSoon,
			Message: summarizeIssues("Licenses expiring soon", expiringSoon),
		}
	case len(dataIssues) > 0:
		return metav1.Condition{
			Status:  metav1.ConditionUnknown,
			Reason:  conditions.LicenseInfoMissing,
			Message: summarizeIssues("Incomplete license information", dataIssues),
		}
	default:
		message := fmt.Sprintf("All %d vGPU device(s) on %d node(s) are licensed", licensedCount, reportedNodes)
		if earliestExpiry != nil {
			message = fmt.Sprintf("%s (next expiry %s)", message, earliestExpiry.Format(time.RFC3339))
		}
		return metav1.Condition{
			Status:  metav1.ConditionTrue,
			Reason:  conditions.LicenseOK,
			Message: message,
		}
	}
}

func formatDeviceDescriptor(node string, device licenseinfo.DeviceStatus) string {
	if device.Product != "" {
		return fmt.Sprintf("%s/%s (%s)", node, device.ID, device.Product)
	}
	return fmt.Sprintf("%s/%s", node, device.ID)
}

func safeStatus(status string) string {
	if status == "" {
		return "status unknown"
	}
	return status
}

func summarizeIssues(prefix string, entries []string) string {
	if len(entries) == 0 {
		return prefix
	}
	if len(entries) > licenseIssueLimit {
		shown := entries[:licenseIssueLimit]
		return fmt.Sprintf("%s: %s (and %d more)", prefix, strings.Join(shown, "; "), len(entries)-licenseIssueLimit)
	}
	return fmt.Sprintf("%s: %s", prefix, strings.Join(entries, "; "))
}
