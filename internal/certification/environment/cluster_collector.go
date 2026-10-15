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
	"log"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/NVIDIA/gpu-operator/internal/certification/k8s"
)

type clusterCollector struct{}

func (c *clusterCollector) Name() string { return "cluster" }

func (c *clusterCollector) Collect(ctx context.Context, clients *k8s.Clients, namespace string, s *EnvironmentSnapshot) error {
	// Get Kubernetes version
	version, err := clients.ServerVersion()
	if err != nil {
		return err
	}
	s.Cluster.KubernetesVersion = version.GitVersion
	s.Cluster.KubernetesGitCommit = version.GitCommit

	// Count total nodes
	allNodes, err := clients.ListNodes(ctx, "")
	if err != nil {
		return err
	}
	s.Cluster.TotalNodes = len(allNodes.Items)

	// Count GPU nodes (post-GFD)
	gpuNodes, err := clients.ListNodes(ctx, k8s.GPUNodeSelector)
	if err != nil {
		return err
	}
	s.Cluster.GPUNodeCount = len(gpuNodes.Items)

	// Find nodes with NVIDIA PCI labels (pre-GFD, detected by NFD)
	s.Cluster.GPUPCINodes, s.Cluster.GPUPCINodeCount = c.findPCINodes(allNodes.Items)

	// Detect platform
	s.Cluster.Platform, s.Cluster.PlatformVersion = c.detectPlatform(ctx, clients, allNodes.Items)

	// Check Pod Security Admission on operator namespace
	ns, err := clients.GetNamespace(ctx, namespace)
	if err == nil && ns != nil {
		if level, ok := ns.Labels["pod-security.kubernetes.io/enforce"]; ok {
			s.Cluster.PodSecurityAdmission = level
		}
	}

	// Capture nvidia-smi output from a driver pod
	c.collectNvidiaSmi(ctx, clients, namespace, s)

	return nil
}

func (c *clusterCollector) collectNvidiaSmi(ctx context.Context, clients *k8s.Clients, namespace string, s *EnvironmentSnapshot) {
	pods, err := clients.ListPods(ctx, namespace, k8s.DriverLabelSelector)
	if err != nil {
		msg := "nvidia-smi output unavailable: could not list driver pods"
		log.Printf("Warning: %s: %v", msg, err)
		s.Warnings = append(s.Warnings, msg)
		return
	}

	pod, err := k8s.FindRunningPod(pods.Items, "")
	if err != nil {
		msg := "nvidia-smi output unavailable: no running driver pod found — this is expected when using pre-installed or host-managed drivers"
		log.Printf("Warning: %s", msg)
		s.Warnings = append(s.Warnings, msg)
		return
	}

	output, err := clients.ExecInPod(ctx, namespace, pod.Name, k8s.DriverContainerName, []string{"nvidia-smi"})
	if err != nil {
		msg := "nvidia-smi output unavailable: exec failed"
		log.Printf("Warning: %s: %v", msg, err)
		s.Warnings = append(s.Warnings, msg)
		return
	}

	s.Cluster.NvidiaSmiOutput = strings.Split(output, "\n")
}

func (c *clusterCollector) detectPlatform(ctx context.Context, clients *k8s.Clients, nodes []corev1.Node) (platform, version string) {
	// Check for OpenShift first (most specific)
	if clients.ResourceExists(ctx, clusterVersionGVR) {
		cv, err := clients.GetDynamicResource(ctx, clusterVersionGVR, "", "version")
		if err == nil && cv != nil {
			version, _, _ = unstructured.NestedString(cv.Object, "status", "desired", "version")
			return "openshift", version
		}
		return "openshift", ""
	}

	// Check node labels for cloud providers
	for _, node := range nodes {
		labels := node.Labels

		// GKE
		if _, ok := labels["cloud.google.com/gke-nodepool"]; ok {
			return "gke", ""
		}

		// EKS
		if _, ok := labels["eks.amazonaws.com/nodegroup"]; ok {
			return "eks", ""
		}

		// AKS
		if _, ok := labels["kubernetes.azure.com/agentpool"]; ok {
			return "aks", ""
		}
	}

	return "vanilla", ""
}

// GPU PCI labels set by NFD before GFD runs.
var gpuPCILabels = []string{
	"feature.node.kubernetes.io/pci-10de.present",
	"feature.node.kubernetes.io/pci-0302_10de.present",
	"feature.node.kubernetes.io/pci-0300_10de.present",
}

func (c *clusterCollector) findPCINodes(nodes []corev1.Node) ([]string, int) {
	seen := make(map[string]bool)
	for _, node := range nodes {
		for _, label := range gpuPCILabels {
			if node.Labels[label] == "true" {
				seen[node.Name] = true
				break
			}
		}
	}

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	return names, len(names)
}
