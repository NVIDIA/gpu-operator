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

package environment

import (
	"context"
	"strings"

	appsv1 "k8s.io/api/apps/v1"

	"github.com/NVIDIA/gpu-operator/internal/certification/k8s"
)

type operandCollector struct{}

func (c *operandCollector) Name() string { return "operands" }

func (c *operandCollector) Collect(ctx context.Context, clients *k8s.Clients, namespace string, s *EnvironmentSnapshot) error {
	daemonsets, err := clients.ListDaemonSets(ctx, namespace, "")
	if err != nil {
		return err
	}

	knownLabels := make(map[string]bool)
	for _, label := range AllOperandLabels() {
		knownLabels[label] = true
	}

	for _, ds := range daemonsets.Items {
		appLabel := ds.Labels["app"]
		if !knownLabels[appLabel] {
			continue
		}

		info := c.extractOperandInfo(&ds, appLabel)

		// Collect pod-level status
		pods, err := clients.ListPods(ctx, namespace, "app="+appLabel)
		if err == nil {
			for _, pod := range pods.Items {
				podInfo := OperandPodInfo{
					Name:  pod.Name,
					Node:  pod.Spec.NodeName,
					Phase: string(pod.Status.Phase),
				}

				podInfo.Ready = isPodReady(&pod)
				if !podInfo.Ready {
					podInfo.Reason = getPodNotReadyReason(&pod)
				}

				info.Pods = append(info.Pods, podInfo)
			}
		}

		s.Operands = append(s.Operands, info)
	}

	return nil
}

func (c *operandCollector) extractOperandInfo(ds *appsv1.DaemonSet, appLabel string) OperandInfo {
	info := OperandInfo{
		Name:           operandDisplayName(appLabel),
		AppLabel:       appLabel,
		Enabled:        true,
		DesiredCount:   ds.Status.DesiredNumberScheduled,
		ReadyCount:     ds.Status.NumberReady,
		AvailableCount: ds.Status.NumberAvailable,
		UpdatedCount:   ds.Status.UpdatedNumberScheduled,
	}

	// Get main container image
	for _, container := range ds.Spec.Template.Spec.Containers {
		// Use first container or one matching the app label pattern
		if info.Image == "" || strings.Contains(container.Name, "nvidia") {
			info.Image = container.Image
			info.ImageTag = parseImageTag(container.Image)
		}
	}

	// Get init containers (important for driver-manager)
	for _, init := range ds.Spec.Template.Spec.InitContainers {
		info.InitContainers = append(info.InitContainers, ContainerInfo{
			Name:  init.Name,
			Image: init.Image,
		})
	}

	return info
}

// operandDisplayNames maps app labels to human-readable names.
var operandDisplayNames = map[string]string{
	OperandDriver:              "Driver",
	OperandToolkit:             "Container Toolkit",
	OperandDevicePlugin:        "Device Plugin",
	OperandDCGMExporter:        "DCGM Exporter",
	OperandDCGM:                "DCGM",
	OperandGFD:                 "GPU Feature Discovery",
	OperandMIGManager:          "MIG Manager",
	OperandVFIOManager:         "VFIO Manager",
	OperandSandboxDevicePlugin: "Sandbox Device Plugin",
	OperandVGPUManager:         "vGPU Manager",
	OperandCCManager:           "CC Manager",
	OperandValidator:           "Operator Validator",
	OperandFabricManager:       "Fabric Manager",
	OperandGDS:                 "GPUDirect Storage",
	OperandGDRCopy:             "GDRCopy",
	OperandKataManager:         "Kata Manager",
	OperandNFDWorker:           "NFD Worker",
	OperandMPSControlDaemon:    "MPS Control Daemon",
}

// operandDisplayName converts app label to human-readable name.
func operandDisplayName(appLabel string) string {
	if name, ok := operandDisplayNames[appLabel]; ok {
		return name
	}
	return appLabel
}
