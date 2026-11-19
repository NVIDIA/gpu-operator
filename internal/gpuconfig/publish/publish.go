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

// Package publish writes the applier's status labels on its node.
package publish

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/apply"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/config"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/status"
)

// Status labels. Neither the applier nor the gate reads them; they are for
// administrators.
const (
	SyncLabel              = status.SyncLabel
	PendingActivationLabel = "nvidia.com/gpu-config.pending-activation"
	SchemaMaxLabel         = "nvidia.com/gpu-config.schema-max"
)

// Values of PendingActivationLabel. A GPU reset activates a staged ECC mode,
// and the applier performs none, so the action it names is a reboot.
const (
	PendingNone   = "none"
	PendingReboot = "reboot"
)

// SchemaMax is the value of SchemaMaxLabel.
var SchemaMax = fmt.Sprintf("v%d", config.MaxVersion)

// PendingActivation returns PendingReboot when any GPU's pending ECC mode differs from its current one.
// known is false when a GPU couldn't be read; only an NVML initialization failure is an error.
func PendingActivation(lib apply.Library) (value string, known bool, readErrs []error, err error) {
	if err := lib.Init(); err != nil {
		return "", false, nil, fmt.Errorf("initializing NVML: %w", err)
	}
	defer func() { _ = lib.Shutdown() }()

	n, err := lib.DeviceCount()
	if err != nil {
		return "", false, []error{fmt.Errorf("NVML device count: %w", err)}, nil
	}
	value = PendingNone
	for i := range n {
		dev, err := lib.DeviceByIndex(i)
		if err != nil {
			readErrs = append(readErrs, fmt.Errorf("NVML device %d: %w", i, err))
			continue
		}
		current, pending, err := dev.ECCModes()
		switch {
		case errors.Is(err, apply.ErrNotSupported):
			// No ECC on this GPU: nothing can be staged.
		case err != nil:
			readErrs = append(readErrs, fmt.Errorf("NVML device %d ECC mode: %w", i, err))
		case current != pending:
			value = PendingReboot
		}
	}
	if len(readErrs) > 0 {
		return "", false, readErrs, nil
	}
	return value, true, nil, nil
}

// NodeClient reads a Node and merges labels into it.
type NodeClient interface {
	GetNode(ctx context.Context, name string) (*corev1.Node, error)
	PatchNodeLabels(ctx context.Context, node string, labels map[string]string) error
}

// Publisher writes the status labels for one node. It isn't safe for
// concurrent use; the applier serializes its callers.
type Publisher struct {
	NodeName string
	Client   NodeClient

	pending string
}

// SetPendingActivation records the result of the latest status read.
func (p *Publisher) SetPendingActivation(value string) {
	p.pending = value
}

// Publish writes sync, schema-max and, once a status read has succeeded,
// pending-activation. It skips the PATCH when the node already carries the
// values.
func (p *Publisher) Publish(ctx context.Context, sync string) error {
	want := map[string]string{SyncLabel: sync, SchemaMaxLabel: SchemaMax}
	if p.pending != "" {
		want[PendingActivationLabel] = p.pending
	}
	// The node is read every time rather than remembered from this
	// container's own writes, so that a label someone else changed or
	// removed is written back.
	node, err := p.Client.GetNode(ctx, p.NodeName)
	if err != nil {
		return fmt.Errorf("reading node %s: %w", p.NodeName, err)
	}
	if hasLabels(node.Labels, want) {
		return nil
	}
	if err := p.Client.PatchNodeLabels(ctx, p.NodeName, want); err != nil {
		return fmt.Errorf("patching status labels on node %s: %w", p.NodeName, err)
	}
	return nil
}

func hasLabels(labels, want map[string]string) bool {
	for k, v := range want {
		if got, ok := labels[k]; !ok || got != v {
			return false
		}
	}
	return true
}
