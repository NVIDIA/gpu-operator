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

package publish

import (
	"context"
	"errors"
	"maps"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/apply"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/apply/fake"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/status"
)

var errBoom = errors.New("boom")

func gpu(current, pending bool, err error) *fake.Device {
	d := &fake.Device{UUIDValue: "GPU", ECCCurrent: current, ECCPending: pending, Errs: map[string]error{}}
	if err != nil {
		d.Errs["ECCModes"] = err
	}
	return d
}

type failingIndexLibrary struct {
	*fake.Library
	index int
	err   error
}

func (l *failingIndexLibrary) DeviceByIndex(i int) (apply.Device, error) {
	if i == l.index {
		return nil, l.err
	}
	return l.Library.DeviceByIndex(i)
}

func TestPendingActivation(t *testing.T) {
	cases := map[string]struct {
		devices        []*fake.Device
		initErr        error
		deviceCountErr error
		deviceIndexErr error
		failIndex      *int
		want           string
		wantKnown      bool
		wantErr        bool
		wantReadErrs   int
		wantECCReads   []string
	}{
		"no GPUs":              {want: PendingNone, wantKnown: true},
		"nothing staged":       {devices: []*fake.Device{gpu(true, true, nil), gpu(false, false, nil)}, want: PendingNone, wantKnown: true},
		"enable staged":        {devices: []*fake.Device{gpu(true, true, nil), gpu(false, true, nil)}, want: PendingReboot, wantKnown: true},
		"disable staged":       {devices: []*fake.Device{gpu(true, false, nil)}, want: PendingReboot, wantKnown: true},
		"GPU without ECC":      {devices: []*fake.Device{gpu(false, false, apply.ErrNotSupported)}, want: PendingNone, wantKnown: true},
		"read error keeps old": {devices: []*fake.Device{gpu(false, true, nil), gpu(false, false, errBoom)}, wantReadErrs: 1},
		"init failure":         {initErr: errBoom, wantErr: true},
		"device count error": {
			devices: []*fake.Device{gpu(true, false, nil)}, deviceCountErr: errBoom,
			wantReadErrs: 1, wantECCReads: []string{},
		},
		"device handle error on every GPU": {
			devices: []*fake.Device{gpu(true, false, nil), gpu(true, true, nil)}, deviceIndexErr: errBoom,
			wantReadErrs: 2, wantECCReads: []string{},
		},
		"device handle error on one GPU": {
			devices: []*fake.Device{gpu(true, true, nil), gpu(true, false, nil)}, deviceIndexErr: errBoom, failIndex: new(0),
			wantReadErrs: 1, wantECCReads: []string{"GPU ECCModes"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			log := &fake.Log{}
			for _, d := range tc.devices {
				d.Log = log
			}
			fl := &fake.Library{Devices: tc.devices, InitErr: tc.initErr, DeviceCountErr: tc.deviceCountErr, DeviceByIndexErr: tc.deviceIndexErr}
			var lib apply.Library = fl
			if tc.failIndex != nil {
				fl.DeviceByIndexErr = nil
				lib = &failingIndexLibrary{Library: fl, index: *tc.failIndex, err: tc.deviceIndexErr}
			}
			got, known, readErrs, err := PendingActivation(lib)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.False(t, fl.Initialized, "NVML left open")
			require.Equal(t, tc.wantKnown, known)
			if tc.wantECCReads != nil {
				require.Equal(t, tc.wantECCReads, append([]string{}, log.Calls...))
			}
			if !known {
				require.Len(t, readErrs, tc.wantReadErrs)
				return
			}
			require.Empty(t, readErrs)
			require.Equal(t, tc.want, got)
		})
	}
}

type fakeNode struct {
	node     *corev1.Node
	getErr   error
	patchErr error
	patches  []map[string]string
}

func (f *fakeNode) GetNode(context.Context, string) (*corev1.Node, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.node.DeepCopy(), nil
}

func (f *fakeNode) PatchNodeLabels(_ context.Context, _ string, labels map[string]string) error {
	f.patches = append(f.patches, maps.Clone(labels))
	if f.patchErr != nil {
		return f.patchErr
	}
	if f.node.Labels == nil {
		f.node.Labels = map[string]string{}
	}
	maps.Copy(f.node.Labels, labels)
	return nil
}

func TestPublish(t *testing.T) {
	ctx := context.Background()
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1", Labels: map[string]string{"other": "x"}}}
	f := &fakeNode{node: node}
	p := &Publisher{NodeName: "node-1", Client: f}

	// Without pending-activation before a status read.
	require.NoError(t, p.Publish(ctx, status.OutOfSync))
	require.Equal(t, []map[string]string{{SyncLabel: status.OutOfSync, SchemaMaxLabel: "v1"}}, f.patches)

	// The node already carries the values: no PATCH.
	require.NoError(t, p.Publish(ctx, status.OutOfSync))
	require.Len(t, f.patches, 1)

	p.SetPendingActivation(PendingReboot)
	require.NoError(t, p.Publish(ctx, status.InSync))
	require.Equal(t, map[string]string{SyncLabel: status.InSync, SchemaMaxLabel: "v1", PendingActivationLabel: PendingReboot}, f.patches[1])
	require.Equal(t, "x", node.Labels["other"])
}

func TestPublishRestoresLabelsChangedBySomeoneElse(t *testing.T) {
	cases := map[string]func(labels map[string]string){
		"sync removed":       func(l map[string]string) { delete(l, SyncLabel) },
		"sync changed":       func(l map[string]string) { l[SyncLabel] = status.OutOfSync },
		"schema-max removed": func(l map[string]string) { delete(l, SchemaMaxLabel) },
		"all removed":        func(l map[string]string) { clear(l) },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			f := &fakeNode{node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}}}
			p := &Publisher{NodeName: "node-1", Client: f}
			require.NoError(t, p.Publish(ctx, status.InSync))
			require.Len(t, f.patches, 1)

			edit(f.node.Labels)
			require.NoError(t, p.Publish(ctx, status.InSync))
			require.Len(t, f.patches, 2)
			require.Equal(t, status.InSync, f.node.Labels[SyncLabel])
			require.Equal(t, SchemaMax, f.node.Labels[SchemaMaxLabel])
		})
	}
}

func TestPublishError(t *testing.T) {
	cases := map[string]struct {
		getErr, patchErr error
		wantPatches      int
	}{
		"read fails":  {getErr: errBoom, wantPatches: 1},
		"patch fails": {patchErr: errBoom, wantPatches: 2},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := &fakeNode{node: &corev1.Node{}, getErr: tc.getErr, patchErr: tc.patchErr}
			p := &Publisher{NodeName: "node-1", Client: f}
			require.ErrorIs(t, p.Publish(context.Background(), status.InSync), errBoom)

			f.getErr, f.patchErr = nil, nil
			require.NoError(t, p.Publish(context.Background(), status.InSync))
			require.Len(t, f.patches, tc.wantPatches)
		})
	}
}
