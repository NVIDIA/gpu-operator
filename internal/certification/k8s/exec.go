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
	"bytes"
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

// ExecInPod runs a command in a container and returns its stdout.
func (c *Clients) ExecInPod(ctx context.Context, namespace, podName, container string, command []string) (string, error) {
	req := c.k8s.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   command,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(c.restConfig, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("creating executor: %w", err)
	}

	var stdout, stderr bytes.Buffer
	if err := exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	}); err != nil {
		return "", fmt.Errorf("exec failed: %w (stderr: %s)", err, stderr.String())
	}

	return strings.TrimSpace(stdout.String()), nil
}

// GetGPUPCIDeviceID execs nvidia-smi in a driver pod on the specified node to
// get the PCI device ID of the first MIG-capable GPU. On multi-GPU nodes this
// queries all GPUs and returns the one where mig.mode.current is "Enabled" or
// "Disabled" (not "[Not Supported]"). Returns an error if no MIG-capable GPU
// is found.
func (c *Clients) GetGPUPCIDeviceID(ctx context.Context, namespace, nodeName string) (string, error) {
	pods, err := c.ListPods(ctx, namespace, DriverLabelSelector)
	if err != nil {
		return "", fmt.Errorf("listing driver pods: %w", err)
	}

	pod, err := FindRunningPod(pods.Items, nodeName)
	if err != nil {
		return "", err
	}

	// Query all GPUs: each line is "0x232110DE, Disabled" or "0x1EB810DE, [Not Supported]".
	output, err := c.ExecInPod(ctx, namespace, pod.Name, DriverContainerName,
		[]string{"nvidia-smi", "--query-gpu=pci.device_id,mig.mode.current", "--format=csv,noheader"})
	if err != nil {
		return "", fmt.Errorf("querying GPU device IDs: %w", err)
	}

	for _, line := range strings.Split(output, "\n") {
		if id, mode, ok := strings.Cut(line, ","); ok && !strings.Contains(mode, "Not Supported") {
			return strings.TrimSpace(id), nil
		}
	}

	return "", fmt.Errorf("no MIG-capable GPU found, nvidia-smi output:\n%s", output)
}

// FindRunningPod returns the first running pod, optionally filtered to a specific node.
// Pass an empty nodeName to match any node.
func FindRunningPod(pods []corev1.Pod, nodeName string) (*corev1.Pod, error) {
	if len(pods) == 0 {
		return nil, fmt.Errorf("no driver pods found with selector %s", DriverLabelSelector)
	}
	for i := range pods {
		if pods[i].Status.Phase != corev1.PodRunning {
			continue
		}
		if nodeName != "" && pods[i].Spec.NodeName != nodeName {
			continue
		}
		return &pods[i], nil
	}
	if nodeName != "" {
		return nil, fmt.Errorf("no running driver pod found on node %s", nodeName)
	}
	return nil, fmt.Errorf("no running driver pods found with selector %s", DriverLabelSelector)
}
