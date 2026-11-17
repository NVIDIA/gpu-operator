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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/apply"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/apply/fake"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/config"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/statefile"
)

const (
	testLoadID = 110711
	operatorNS = "gpu-operator"
	applierDS  = "nvidia-gpu-config-manager"
	testNode   = "node-1"
)

type fakeCluster struct {
	node    *corev1.Node
	nodeErr error

	// podReads holds successive pod lists; the last one repeats.
	podReads [][]corev1.Pod
	podCalls int
	podErr   error

	claims   map[string][]string
	claimErr map[string]error

	// sliceReads holds successive slice-driver lists; the last one repeats.
	sliceReads [][]string
	sliceCalls int

	// order records "pods" and "slices" reads in order.
	order []string
}

func (c *fakeCluster) GetNode(_ context.Context, _ string) (*corev1.Node, error) {
	return c.node, c.nodeErr
}

func (c *fakeCluster) ListNodePods(_ context.Context, _ string) ([]corev1.Pod, error) {
	c.order = append(c.order, "pods")
	if c.podErr != nil {
		return nil, c.podErr
	}
	i := min(c.podCalls, len(c.podReads)-1)
	c.podCalls++
	if i < 0 {
		return nil, nil
	}
	return c.podReads[i], nil
}

func (c *fakeCluster) ClaimDrivers(_ context.Context, ns, name string) ([]string, error) {
	key := ns + "/" + name
	if err := c.claimErr[key]; err != nil {
		return nil, err
	}
	drivers, ok := c.claims[key]
	if !ok {
		return nil, ErrClaimNotFound
	}
	return drivers, nil
}

func (c *fakeCluster) NodeSliceDrivers(_ context.Context, _ string) ([]string, error) {
	c.order = append(c.order, "slices")
	i := min(c.sliceCalls, len(c.sliceReads)-1)
	c.sliceCalls++
	if i < 0 {
		return nil, nil
	}
	return c.sliceReads[i], nil
}

type env struct {
	t       *testing.T
	dir     string
	store   *statefile.Store
	cluster *fakeCluster
	lib     *fake.Library
	log     *fake.Log
	// loadIDs are returned by successive LoadID calls; the last one repeats.
	loadIDs   []uint64
	loadCalls int
	doc       *config.Source
	ready     bool
	sleeps    []time.Duration
	// lockChecks counts the sleeps and NVML setting writes, each of which
	// requires the state file lock to be held.
	lockChecks int
	d          *Decider
}

func newEnv(t *testing.T, stack Stack) *env {
	e := &env{
		t:       t,
		dir:     t.TempDir(),
		cluster: &fakeCluster{node: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: testNode}}},
		log:     &fake.Log{},
		loadIDs: []uint64{testLoadID},
		ready:   true,
	}
	e.store = statefile.New(e.dir)
	e.lib = &fake.Library{Devices: []*fake.Device{e.device("GPU-0")}}
	e.d = &Decider{
		Config: Config{
			Stack: stack, NodeName: testNode, Namespace: operatorNS, ApplierDaemonSet: applierDS,
			ValidationsDir: e.dir, PodRecheck: DefaultPodRecheck, SliceRecheck: DefaultSliceRecheck,
		},
		Store:   e.store,
		Cluster: e.cluster,
		NVML:    &lockedLibrary{Library: e.lib, requireLocked: e.requireLocked},
		LoadID: func() (uint64, bool, error) {
			l := e.loadIDs[min(e.loadCalls, len(e.loadIDs)-1)]
			e.loadCalls++
			return l, l != 0, nil
		},
		Document:    func() *config.Source { return e.doc },
		DriverReady: func() (bool, error) { return e.ready, nil },
		Sleep: func(_ context.Context, d time.Duration) error {
			e.requireLocked()
			e.sleeps = append(e.sleeps, d)
			return nil
		},
	}
	return e
}

// requireLocked fails the test unless another holder is locked out of the
// state file, as a gate would be.
func (e *env) requireLocked() {
	e.t.Helper()
	e.lockChecks++
	l, err := e.store.TryLock()
	if err == nil {
		_ = l.Unlock()
	}
	require.ErrorIs(e.t, err, statefile.ErrLockBusy, "state file lock not held")
}

// lockedLibrary wraps the fake NVML so that every setting write checks the
// state file lock.
type lockedLibrary struct {
	*fake.Library
	requireLocked func()
}

func (l *lockedLibrary) DeviceByIndex(i int) (apply.Device, error) {
	dev, err := l.Library.DeviceByIndex(i)
	if err != nil {
		return nil, err
	}
	return &lockedDevice{Device: dev, requireLocked: l.requireLocked}, nil
}

type lockedDevice struct {
	apply.Device
	requireLocked func()
}

func (d *lockedDevice) SetPowerLimit(milliwatts uint32) error {
	d.requireLocked()
	return d.Device.SetPowerLimit(milliwatts)
}

func (d *lockedDevice) SetGPULockedClocks(minMHz, maxMHz uint32) error {
	d.requireLocked()
	return d.Device.SetGPULockedClocks(minMHz, maxMHz)
}

func (d *lockedDevice) SetMemoryLockedClocks(minMHz, maxMHz uint32) error {
	d.requireLocked()
	return d.Device.SetMemoryLockedClocks(minMHz, maxMHz)
}

func (d *lockedDevice) SetECCMode(enabled bool) error {
	d.requireLocked()
	return d.Device.SetECCMode(enabled)
}

func (e *env) device(uuid string) *fake.Device {
	return &fake.Device{
		Log: e.log, UUIDValue: uuid, NameValue: "NVIDIA H100 80GB HBM3", PCIDeviceIDValue: 0x233010DE,
		PowerMinMilliwatts: 200000, PowerMaxMilliwatts: 700000,
		Errs: map[string]error{},
	}
}

func (e *env) writeState(content string) {
	require.NoError(e.t, os.WriteFile(filepath.Join(e.dir, statefile.StateFileName), []byte(content), 0o600))
}

func (e *env) state() *statefile.File {
	f, err := e.store.Read()
	require.NoError(e.t, err)
	return f
}

func (e *env) touch(name string) {
	require.NoError(e.t, os.WriteFile(filepath.Join(e.dir, name), nil, 0o600))
}

func (e *env) pods(reads ...[]corev1.Pod) {
	e.cluster.podReads = reads
}

func (e *env) decide() (Result, error) {
	return e.d.Decide(context.Background(), testLoadID)
}

// requireUnchanged asserts the state file is as the test wrote it, or absent.
func (e *env) requireUnchanged(before string) {
	b, err := os.ReadFile(filepath.Join(e.dir, statefile.StateFileName))
	if before == "" {
		require.True(e.t, os.IsNotExist(err), "state file was written")
		return
	}
	require.NoError(e.t, err)
	require.Equal(e.t, before, string(b))
}

type podOpt func(*corev1.Pod)

func newPod(name string, opts ...podOpt) corev1.Pod {
	p := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", UID: types.UID(name), Labels: map[string]string{}},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}},
	}
	for _, o := range opts {
		o(&p)
	}
	return p
}

func named(n string) podOpt {
	return func(p *corev1.Pod) { p.Name, p.UID = n, types.UID(n) }
}

func inNS(ns string) podOpt { return func(p *corev1.Pod) { p.Namespace = ns } }

func label(k, v string) podOpt { return func(p *corev1.Pod) { p.Labels[k] = v } }

func ownedBy(ds string) podOpt {
	return func(p *corev1.Pod) {
		yes := true
		p.OwnerReferences = []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "DaemonSet", Name: ds, Controller: &yes}}
	}
}

func gpuRequestOnly(res string) podOpt {
	return func(p *corev1.Pod) {
		p.Spec.Containers[0].Resources.Requests = corev1.ResourceList{corev1.ResourceName(res): resource.MustParse("1")}
	}
}

func gpuRequest(res string) podOpt {
	return func(p *corev1.Pod) {
		p.Spec.Containers[0].Resources.Limits = corev1.ResourceList{corev1.ResourceName(res): resource.MustParse("1")}
	}
}

func initContainers(names ...string) podOpt {
	return func(p *corev1.Pod) {
		for _, n := range names {
			p.Spec.InitContainers = append(p.Spec.InitContainers, corev1.Container{Name: n})
		}
	}
}

func running() corev1.ContainerState {
	return corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
}

func terminated() corev1.ContainerState {
	return corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{}}
}

// killed is a container ended by SIGKILL, as when its pod is deleted.
func killed() corev1.ContainerState {
	return corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 137}}
}

func waiting() corev1.ContainerState {
	return corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "PodInitializing"}}
}

func status(name string, state corev1.ContainerState) podOpt {
	return statusWithLast(name, state, corev1.ContainerState{})
}

func statusWithLast(name string, state, last corev1.ContainerState) podOpt {
	return func(p *corev1.Pod) {
		s := corev1.ContainerStatus{Name: name, State: state, LastTerminationState: last}
		for _, c := range p.Spec.InitContainers {
			if c.Name == name {
				p.Status.InitContainerStatuses = append(p.Status.InitContainerStatuses, s)
				return
			}
		}
		p.Status.ContainerStatuses = append(p.Status.ContainerStatuses, s)
	}
}

func claim(name string) podOpt {
	return func(p *corev1.Pod) {
		p.Spec.ResourceClaims = append(p.Spec.ResourceClaims, corev1.PodResourceClaim{Name: name, ResourceClaimName: &name})
	}
}

func terminating() podOpt {
	return func(p *corev1.Pod) {
		now := metav1.Now()
		p.DeletionTimestamp = &now
	}
}

func sidecar(name string) podOpt {
	return func(p *corev1.Pod) {
		always := corev1.ContainerRestartPolicyAlways
		p.Spec.InitContainers = append(p.Spec.InitContainers, corev1.Container{Name: name, RestartPolicy: &always})
	}
}
