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

package decision

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	corev1 "k8s.io/api/core/v1"

	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/statefile"
)

// ToolkitReadyFile is written by the validator's toolkit step, which runs
// after the gate. Every container-stack allocator waits for it.
const ToolkitReadyFile = "toolkit-ready"

// evidence returns why an allocator may have been released at this driver load
// or earlier this boot and still on the node, or "", and the pod list it read.
func (d *decision) evidence(ctx context.Context, st *statefile.State, loadID uint64, node *corev1.Node) (string, []corev1.Pod, error) {
	if st.Passed(loadID) {
		return "a gate passed at this driver load", nil, nil
	}
	if st.GatePassedWithoutDecision() {
		return "a gate passed with no decision to rely on", nil, nil
	}

	if d.Config.Stack == ContainerStack {
		_, err := os.Stat(filepath.Join(d.Config.ValidationsDir, ToolkitReadyFile))
		if err == nil {
			return ToolkitReadyFile + " exists", nil, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", nil, fmt.Errorf("%w: stat %s: %w", ErrTransient, ToolkitReadyFile, err)
		}
		for name, q := range node.Status.Allocatable {
			if isGPUResource(name) && q.Sign() > 0 {
				return fmt.Sprintf("node allocatable %s is %s", name, q.String()), nil, nil
			}
		}
	}

	if d.Config.Stack == DRAStack {
		reason, err := d.sliceEvidence(ctx)
		if reason != "" || err != nil {
			return reason, nil, err
		}
	}

	// The pod rules catch allocators and gate pods left from an earlier
	// driver load.
	if !st.GatePassedThisBoot() {
		return "", nil, nil
	}
	pods, err := d.listPods(ctx)
	if err != nil {
		return "", nil, err
	}
	for i := range pods {
		pod := &pods[i]
		if d.Config.Stack == ContainerStack && d.allocatorPastWait(pod) {
			return fmt.Sprintf("allocator pod %s/%s got past its wait", pod.Namespace, pod.Name), pods, nil
		}
		if d.gatePassed(pod) {
			return fmt.Sprintf("gate pod %s/%s passed its gate", pod.Namespace, pod.Name), pods, nil
		}
	}
	return "", pods, nil
}

// sliceEvidence counts only a slice still listed SliceRecheck later, since a
// stale slice can outlive its plugin by 30 s; any slice in the second list
// counts.
func (d *decision) sliceEvidence(ctx context.Context) (string, error) {
	has := func() (bool, error) {
		drivers, err := d.Cluster.NodeSliceDrivers(ctx, d.Config.NodeName)
		if err != nil {
			return false, fmt.Errorf("%w: listing ResourceSlices: %w", ErrAPI, err)
		}
		return slices.Contains(drivers, gpuDriver) || slices.Contains(drivers, computeDomainDriver), nil
	}
	found, err := has()
	if err != nil || !found {
		return "", err
	}
	if err := d.Sleep(ctx, d.Config.SliceRecheck); err != nil {
		return "", fmt.Errorf("waiting to re-check ResourceSlices: %w", err)
	}
	found, err = has()
	if err != nil || !found {
		return "", err
	}
	return "the DRA driver published ResourceSlices for the node", nil
}
