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

package loadid

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRead(t *testing.T) {
	dir := t.TempDir()
	module := filepath.Join(dir, "nvidia")

	_, ok, err := Read(module)
	require.NoError(t, err)
	require.False(t, ok)

	require.NoError(t, os.Mkdir(module, 0o700))
	l, ok, err := Read(module)
	require.NoError(t, err)
	require.True(t, ok)

	fi, err := os.Stat(module)
	require.NoError(t, err)
	require.Equal(t, fi.Sys().(*syscall.Stat_t).Ino, l)
	require.NotZero(t, l)
}

// The format nvidia-validator's driver step writes.
const containerizedDriver = `IS_HOST_DRIVER=false
NVIDIA_DRIVER_ROOT=/run/nvidia/driver
DRIVER_ROOT_CTR_PATH=/driver-root
NVIDIA_DEV_ROOT=/run/nvidia/driver
DEV_ROOT_CTR_PATH=/driver-root
`

const hostDriver = `IS_HOST_DRIVER=true
NVIDIA_DRIVER_ROOT=/
DRIVER_ROOT_CTR_PATH=/host
NVIDIA_DEV_ROOT=/
DEV_ROOT_CTR_PATH=/host
`

func TestReadDriverInfo(t *testing.T) {
	cases := map[string]struct {
		content string
		want    *DriverInfo
		wantErr bool
	}{
		"containerized driver": {
			content: containerizedDriver,
			want: &DriverInfo{
				DriverRoot: "/run/nvidia/driver", DriverRootCtrPath: "/driver-root",
				DevRoot: "/run/nvidia/driver", DevRootCtrPath: "/driver-root",
			},
		},
		"host driver": {
			content: hostDriver,
			want: &DriverInfo{
				IsHostDriver: true, DriverRoot: "/", DriverRootCtrPath: "/host",
				DevRoot: "/", DevRootCtrPath: "/host",
			},
		},
		"unknown keys and blank lines are ignored": {
			content: "IS_HOST_DRIVER=true\n\nNEW_KEY=x\n",
			want:    &DriverInfo{IsHostDriver: true},
		},
		"missing IS_HOST_DRIVER": {
			content: "NVIDIA_DRIVER_ROOT=/\n",
			wantErr: true,
		},
		"invalid IS_HOST_DRIVER": {
			content: "IS_HOST_DRIVER=maybe\n",
			wantErr: true,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, DriverReadyFile), []byte(tc.content), 0o600))
			got, ok, err := readDriverInfo(dir)
			if tc.wantErr {
				require.Error(t, err)
				require.False(t, ok)
				return
			}
			require.NoError(t, err)
			require.True(t, ok)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestReadDriverInfoMissing(t *testing.T) {
	info, ok, err := readDriverInfo(t.TempDir())
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, info)
}

func TestReady(t *testing.T) {
	cases := map[string]struct {
		driverReady     string
		ctrReady        bool
		operatorManaged bool
		want            bool
	}{
		"no driver-ready":                          {want: false},
		"no driver-ready, container ready":         {ctrReady: true, operatorManaged: true, want: false},
		"host driver":                              {driverReady: hostDriver, operatorManaged: true, want: true},
		"operator-managed container, not ready":    {driverReady: containerizedDriver, operatorManaged: true, want: false},
		"operator-managed container, ready":        {driverReady: containerizedDriver, ctrReady: true, operatorManaged: true, want: true},
		"container the operator doesn't manage":    {driverReady: containerizedDriver, want: true},
		"unmanaged container with a leftover file": {driverReady: containerizedDriver, ctrReady: true, want: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.driverReady != "" {
				require.NoError(t, os.WriteFile(filepath.Join(dir, DriverReadyFile), []byte(tc.driverReady), 0o600))
			}
			if tc.ctrReady {
				require.NoError(t, os.WriteFile(filepath.Join(dir, DriverCtrReadyFile), nil, 0o600))
			}
			info, ok, err := Ready(dir, tc.operatorManaged)
			require.NoError(t, err)
			require.Equal(t, tc.want, ok)
			require.Equal(t, tc.want, info != nil)
		})
	}
}

func TestReadyErrorOnUnparsableDriverReady(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, DriverReadyFile), []byte("NVIDIA_DRIVER_ROOT=/\n"), 0o600))
	_, ok, err := Ready(dir, true)
	require.Error(t, err)
	require.False(t, ok)
}

func TestReadyErrorOnUnreadableDriverReady(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, DriverReadyFile), 0o700))
	info, ok, err := Ready(dir, true)
	require.Error(t, err)
	require.False(t, ok)
	require.Nil(t, info)
}

type fakeLister struct {
	dss       []appsv1.DaemonSet
	err       error
	namespace string
	selector  string
}

func (f *fakeLister) ListDaemonSets(_ context.Context, namespace, labelSelector string) ([]appsv1.DaemonSet, error) {
	f.namespace, f.selector = namespace, labelSelector
	return f.dss, f.err
}

func daemonSet(apiVersion, kind string, controller bool) appsv1.DaemonSet {
	return appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{
		OwnerReferences: []metav1.OwnerReference{{APIVersion: apiVersion, Kind: kind, Name: "owner", Controller: &controller}},
	}}
}

func TestManagedByOperator(t *testing.T) {
	cases := map[string]struct {
		dss  []appsv1.DaemonSet
		want bool
	}{
		"no driver DaemonSet":              {},
		"owned by ClusterPolicy":           {dss: []appsv1.DaemonSet{daemonSet("nvidia.com/v1", "ClusterPolicy", true)}, want: true},
		"owned by NVIDIADriver":            {dss: []appsv1.DaemonSet{daemonSet("nvidia.com/v1alpha1", "NVIDIADriver", true)}, want: true},
		"owner isn't the controller":       {dss: []appsv1.DaemonSet{daemonSet("nvidia.com/v1", "ClusterPolicy", false)}},
		"owned by another kind":            {dss: []appsv1.DaemonSet{daemonSet("nvidia.com/v1", "GPUCluster", true)}},
		"owned by another API group":       {dss: []appsv1.DaemonSet{daemonSet("example.com/v1", "ClusterPolicy", true)}},
		"no owner":                         {dss: []appsv1.DaemonSet{{}}},
		"one of several is operator-owned": {dss: []appsv1.DaemonSet{{}, daemonSet("nvidia.com/v1", "ClusterPolicy", true)}, want: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			l := &fakeLister{dss: tc.dss}
			got, err := ManagedByOperator(context.Background(), l, "gpu-operator")
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
			require.Equal(t, "gpu-operator", l.namespace)
			require.Equal(t, "app.kubernetes.io/component=nvidia-driver", l.selector)
		})
	}
}

func TestManagedByOperatorListError(t *testing.T) {
	_, err := ManagedByOperator(context.Background(), &fakeLister{err: errors.New("forbidden")}, "gpu-operator")
	require.ErrorContains(t, err, "forbidden")
}
