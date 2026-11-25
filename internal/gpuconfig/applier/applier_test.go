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

package applier

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"github.com/NVIDIA/gpu-operator/internal/driver"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/apply"
	nvmlfake "github.com/NVIDIA/gpu-operator/internal/gpuconfig/apply/fake"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/config"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/decision"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/kube"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/publish"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/statefile"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/status"
)

const (
	testLoadID = 110711
	nodeName   = "node-1"
	ns         = "gpu-operator"
	hostDrv    = "IS_HOST_DRIVER=true\nNVIDIA_DRIVER_ROOT=/\nDRIVER_ROOT_CTR_PATH=/host\n"
	ctrDrv     = "IS_HOST_DRIVER=false\nNVIDIA_DRIVER_ROOT=/run/nvidia/driver\nDRIVER_ROOT_CTR_PATH=/driver-root\n"
	docYAML    = "version: v1\ndefault:\n  powerLimit: 500\n"
)

type staticDocs struct{ src *config.Source }

func (d staticDocs) Snapshot() *config.Source { return d.src }

type env struct {
	t   *testing.T
	dir string
	a   *Applier
	c   client.WithWatch
	lib *nvmlfake.Library
	// loadIDs returns the load ID for a poll number; 0 is absent.
	loadIDs   func(poll int) uint64
	polls     int
	sleeps    []time.Duration
	maxSleeps int
	// beforeSleep runs before each sleep, with the number of sleeps so far.
	beforeSleep func(n int)
	driverRoots []string
}

func newEnv(t *testing.T, objs ...client.Object) *env {
	e := &env{t: t, dir: t.TempDir(), maxSleeps: 5, loadIDs: func(int) uint64 { return testLoadID }}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName, ResourceVersion: "1"}}
	e.c = fake.NewClientBuilder().
		WithScheme(kube.NewScheme()).
		WithObjects(append([]client.Object{node}, objs...)...).
		WithIndex(&corev1.Pod{}, kube.NodeNameField, func(o client.Object) []string { return []string{o.(*corev1.Pod).Spec.NodeName} }).
		Build()
	e.lib = &nvmlfake.Library{Devices: []*nvmlfake.Device{{
		UUIDValue: "GPU-0", NameValue: "NVIDIA H100", PCIDeviceIDValue: 0x233010DE,
		PowerMinMilliwatts: 200000, PowerMaxMilliwatts: 700000, Errs: map[string]error{},
	}}}
	e.a = &Applier{
		Config: Config{
			NodeName: nodeName, Namespace: ns, Stack: decision.ContainerStack,
			ValidationsDir: e.dir, PollInterval: DefaultPollInterval, PublishJitter: DefaultPublishJitter,
		},
		Store:     statefile.New(e.dir),
		Cluster:   &kube.Client{C: e.c},
		Documents: staticDocs{&config.Source{Name: "gpu-config", Data: map[string]string{config.ConfigKey: docYAML}}},
		LoadID: func() (uint64, bool, error) {
			id := e.loadIDs(e.polls)
			return id, id != 0, nil
		},
		OpenNVML: func(root string) (apply.Library, error) {
			e.driverRoots = append(e.driverRoots, root)
			return e.lib, nil
		},
		Sleep: func(ctx context.Context, d time.Duration) error {
			if d < DefaultPollInterval && d != minBackoff && d != 2*minBackoff && d != 4*minBackoff {
				// Jitter and re-check waits pass straight through.
				return ctx.Err()
			}
			if e.beforeSleep != nil {
				e.beforeSleep(len(e.sleeps))
			}
			e.sleeps = append(e.sleeps, d)
			e.polls++
			if len(e.sleeps) >= e.maxSleeps {
				return context.Canceled
			}
			return nil
		},
		Jitter: func(time.Duration) time.Duration { return 0 },
		Log:    discardLogger(),
	}
	return e
}

func (e *env) write(name, content string) {
	require.NoError(e.t, os.WriteFile(filepath.Join(e.dir, name), []byte(content), 0o600))
}

func (e *env) run() error {
	return e.a.Run(context.Background())
}

func (e *env) node() *corev1.Node {
	node := &corev1.Node{}
	require.NoError(e.t, e.c.Get(context.Background(), client.ObjectKey{Name: nodeName}, node))
	return node
}

func (e *env) state() *statefile.File {
	f, err := e.a.Store.Read()
	require.NoError(e.t, err)
	return f
}

func TestFirstBootAppliesAndPublishes(t *testing.T) {
	e := newEnv(t)
	// The driver becomes ready only at the third poll.
	e.beforeSleep = func(n int) {
		if n == 1 {
			e.write(driver.ReadyFile, hostDrv)
		}
	}
	require.ErrorIs(t, e.run(), context.Canceled)

	require.Equal(t, []string{"/host"}, e.driverRoots)
	st := e.state()
	require.True(t, st.AppliedAt(testLoadID))
	require.Equal(t, config.Hash(e.a.Documents.Snapshot()), st.Apply.Config)
	require.Equal(t, uint32(500000), e.lib.Devices[0].PowerLimit)
	require.False(t, e.lib.Initialized, "NVML left open")

	labels := e.node().Labels
	require.Equal(t, status.InSync, labels[publish.SyncLabel])
	require.Equal(t, publish.PendingNone, labels[publish.PendingActivationLabel])
	require.Equal(t, publish.SchemaMax, labels[publish.SchemaMaxLabel])
}

func TestOperatorManagedDriverWaitsForTheContainer(t *testing.T) {
	cases := map[string]struct {
		objs  []client.Object
		setup func(e *env)
		// steps run before the sleep with the same index.
		steps      map[int]func(e *env)
		ctrReadyAt int
		maxSleeps  int
	}{
		"DaemonSet present at start": {
			objs:       []client.Object{operatorDriverDaemonSet()},
			setup:      func(e *env) { e.write(driver.ReadyFile, ctrDrv) },
			ctrReadyAt: 2,
			maxSleeps:  5,
		},
		"DaemonSet created after start": {
			steps: map[int]func(e *env){
				0: func(e *env) { require.NoError(e.t, e.c.Create(context.Background(), operatorDriverDaemonSet())) },
				1: func(e *env) { e.write(driver.ReadyFile, ctrDrv) },
			},
			ctrReadyAt: 3,
			maxSleeps:  6,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, tc.objs...)
			if tc.setup != nil {
				tc.setup(e)
			}
			var rootsBeforeCtrReady []string
			e.beforeSleep = func(n int) {
				if step := tc.steps[n]; step != nil {
					step(e)
				}
				if n == tc.ctrReadyAt {
					rootsBeforeCtrReady = append([]string(nil), e.driverRoots...)
					e.write(driver.CtrReadyFile, "")
				}
			}
			e.maxSleeps = tc.maxSleeps
			require.ErrorIs(t, e.run(), context.Canceled)
			require.Empty(t, rootsBeforeCtrReady, "bound before the driver container was ready")
			require.Equal(t, []string{"/driver-root"}, e.driverRoots)
			require.True(t, e.state().AppliedAt(testLoadID))
		})
	}
}

func operatorDriverDaemonSet() *appsv1.DaemonSet {
	yes := true
	return &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{
		Name: "nvidia-driver-daemonset", Namespace: ns,
		Labels:          map[string]string{"app.kubernetes.io/component": "nvidia-driver"},
		OwnerReferences: []metav1.OwnerReference{{APIVersion: "nvidia.com/v1", Kind: "ClusterPolicy", Name: "cp", UID: "u", Controller: &yes}},
	}}
}

func TestNoBindWhileNothingIsReady(t *testing.T) {
	cases := map[string]func(e *env){
		"driver not ready": func(*env) {},
		"module absent": func(e *env) {
			e.write(driver.ReadyFile, hostDrv)
			e.loadIDs = func(int) uint64 { return 0 }
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			setup(e)
			require.ErrorIs(t, e.run(), context.Canceled)
			require.Empty(t, e.driverRoots)
			require.False(t, e.state().Exists())
			require.NotContains(t, e.node().Labels, publish.SyncLabel)
		})
	}
}

func TestPendingActivationIsReadOnceTheDriverIsReadyAgain(t *testing.T) {
	e := newEnv(t, operatorDriverDaemonSet())
	e.write(driver.ReadyFile, ctrDrv)
	e.write(driver.CtrReadyFile, "")
	e.write(statefile.StateFileName, `{"version":1,"apply":{"module":110711,"value":"applied","config":"","time":"2026-01-01T00:00:00Z"}}`)
	open := e.a.OpenNVML
	e.a.OpenNVML = func(root string) (apply.Library, error) {
		// The driver container restarts right after the bind, so the first
		// idle test finds the driver not ready.
		require.NoError(t, os.Remove(filepath.Join(e.dir, driver.CtrReadyFile)))
		return open(root)
	}
	var labelsWhileNotReady map[string]string
	e.beforeSleep = func(n int) {
		if n == 1 {
			labelsWhileNotReady = e.node().Labels
			e.write(driver.CtrReadyFile, "")
		}
	}
	require.ErrorIs(t, e.run(), context.Canceled)
	require.NotContains(t, labelsWhileNotReady, publish.PendingActivationLabel)
	require.Equal(t, publish.PendingNone, e.node().Labels[publish.PendingActivationLabel])
}

func TestFailedPendingActivationReadIsRetried(t *testing.T) {
	e := newEnv(t)
	e.write(driver.ReadyFile, hostDrv)
	e.write(statefile.StateFileName, `{"version":1,"apply":{"module":110711,"value":"skipped","config":"","time":"2026-01-01T00:00:00Z"}}`)
	var logs bytes.Buffer
	log := logrus.New()
	log.SetOutput(&logs)
	e.a.Log = log
	e.lib.Devices[0].Errs["ECCModes"] = errors.New("GPU is lost")
	var labelsWhileFailing map[string]string
	e.beforeSleep = func(n int) {
		if n == 2 {
			labelsWhileFailing = e.node().Labels
			delete(e.lib.Devices[0].Errs, "ECCModes")
		}
	}
	require.ErrorIs(t, e.run(), context.Canceled)
	require.NotContains(t, labelsWhileFailing, publish.PendingActivationLabel)
	require.Equal(t, publish.PendingNone, e.node().Labels[publish.PendingActivationLabel])
	require.Equal(t, 3, strings.Count(logs.String(), "reading ECC modes"), "every failed read before the recovery warns")
}

func TestDriverDaemonSetListReadsFromCacheAndBacksOff(t *testing.T) {
	e := newEnv(t)
	var rvs []string
	e.a.Cluster = &kube.Client{C: interceptor.NewClient(e.c, interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*appsv1.DaemonSetList); ok {
				o := &client.ListOptions{}
				o.ApplyOptions(opts)
				rvs = append(rvs, o.AsListOptions().ResourceVersion)
				if len(rvs) <= 3 {
					return apierrors.NewServiceUnavailable("etcd")
				}
			}
			return c.List(ctx, list, opts...)
		},
	})}
	e.maxSleeps = 4
	require.ErrorIs(t, e.run(), context.Canceled)
	require.Equal(t, []time.Duration{minBackoff, 2 * minBackoff, 4 * minBackoff, DefaultPollInterval}, e.sleeps)
	require.Equal(t, []string{"0", "0", "0", "0"}, rvs)
}

func TestIdlePublishesOnceAtTheFirstIdleTest(t *testing.T) {
	e := newEnv(t)
	e.write(driver.ReadyFile, hostDrv)
	e.write(statefile.StateFileName, `{"version":1,"apply":{"module":110711,"value":"skipped","config":"","time":"2026-01-01T00:00:00Z"}}`)
	// Publish skips the PATCH when the labels match, so count its node reads:
	// an idle decision reads no node.
	publishes := 0
	e.a.Cluster = &kube.Client{C: interceptor.NewClient(e.c, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, ok := obj.(*corev1.Node); ok {
				publishes++
			}
			return c.Get(ctx, key, obj, opts...)
		},
	})}
	require.ErrorIs(t, e.run(), context.Canceled)
	require.Equal(t, 1, publishes)
	require.Empty(t, e.lib.Devices[0].PowerLimit, "an idle applier writes nothing")
	require.Equal(t, status.OutOfSync, e.node().Labels[publish.SyncLabel])
}

func TestLoadIDChangeExitsForRebind(t *testing.T) {
	e := newEnv(t)
	e.write(driver.ReadyFile, hostDrv)
	e.loadIDs = func(poll int) uint64 {
		if poll >= 2 {
			return testLoadID + 1
		}
		return testLoadID
	}
	e.maxSleeps = 10
	require.NoError(t, e.run())
	require.True(t, e.state().AppliedAt(testLoadID))
}

func TestUnsupportedModeSkips(t *testing.T) {
	e := newEnv(t)
	e.write(driver.ReadyFile, hostDrv)
	e.lib.Devices[0].Virtualization = apply.VirtualizationVGPU
	require.ErrorIs(t, e.run(), context.Canceled)
	require.Equal(t, statefile.Skipped, e.state().Apply.Value)
	require.Empty(t, e.lib.Devices[0].PowerLimit)
	require.Equal(t, status.OutOfSync, e.node().Labels[publish.SyncLabel])
}

func TestUnparsableStateFileWritesNothing(t *testing.T) {
	e := newEnv(t)
	e.write(driver.ReadyFile, hostDrv)
	e.write(statefile.StateFileName, `{}`)
	require.ErrorIs(t, e.run(), context.Canceled)
	data, err := os.ReadFile(filepath.Join(e.dir, statefile.StateFileName))
	require.NoError(t, err)
	require.Equal(t, `{}`, string(data))
	require.Empty(t, e.lib.Devices[0].PowerLimit)
	require.NotContains(t, e.node().Labels, publish.SyncLabel)
}

func TestAPIErrorBacksOff(t *testing.T) {
	e := newEnv(t)
	e.write(driver.ReadyFile, hostDrv)
	e.a.Cluster = &kube.Client{C: interceptor.NewClient(e.c, interceptor.Funcs{
		Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if _, ok := obj.(*corev1.Node); ok {
				return apierrors.NewForbidden(schema.GroupResource{Resource: "nodes"}, key.Name, errors.New("rbac"))
			}
			return c.Get(ctx, key, obj, opts...)
		},
	})}
	e.maxSleeps = 3
	require.ErrorIs(t, e.run(), context.Canceled)
	require.Equal(t, []time.Duration{minBackoff, 2 * minBackoff, 4 * minBackoff}, e.sleeps)
	require.False(t, e.state().Exists())
}

func TestFailedPublishIsRetriedAtTheNextPoll(t *testing.T) {
	e := newEnv(t)
	e.write(driver.ReadyFile, hostDrv)
	e.write(statefile.StateFileName, `{"version":1,"apply":{"module":110711,"value":"skipped","config":"","time":"2026-01-01T00:00:00Z"}}`)
	var patches []int
	e.a.Cluster = &kube.Client{C: interceptor.NewClient(e.c, interceptor.Funcs{
		Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			patches = append(patches, len(e.sleeps))
			if len(patches) == 1 {
				return errors.New("API server unavailable")
			}
			return c.Patch(ctx, obj, patch, opts...)
		},
	})}
	require.ErrorIs(t, e.run(), context.Canceled)
	require.Equal(t, []int{0, 1}, patches, "the failed publish is retried once, at the next poll")
	require.Equal(t, status.OutOfSync, e.node().Labels[publish.SyncLabel])
}

func TestNVMLInitFailureFailsTheContainer(t *testing.T) {
	e := newEnv(t)
	e.write(driver.ReadyFile, hostDrv)
	e.lib.InitErr = errors.New("driver/library version mismatch")
	err := e.run()
	require.ErrorContains(t, err, "version mismatch")
	require.False(t, e.state().Exists())
}

func TestDocumentChangePublishesAfterTheJitter(t *testing.T) {
	const decided = `{"version":1,"apply":{"module":110711,"value":"skipped","config":"","time":"2026-01-01T00:00:00Z"}}`
	cases := map[string]struct {
		decided bool
		applied bool
		edit    bool
		// changes lists the sleeps before which a document changes.
		changes []int
		// want lists the sleep count at each publish; the first idle test
		// publishes at 0.
		want []int
		// wantSyncs lists the sync label the node carries at each publish.
		wantSyncs []string
		wantSync  string
	}{
		"a change publishes once the jitter has passed":  {decided: true, changes: []int{1}, want: []int{0, 5}},
		"changes while a publish is queued publish once": {decided: true, changes: []int{1, 2, 3}, want: []int{0, 5}},
		"a change after a publish queues another":        {decided: true, changes: []int{1, 6}, want: []int{0, 5, 10}},
		"nothing is published before a decision":         {changes: []int{1}},
		"an edit to an applied document publishes out-of-sync": {
			applied: true, edit: true, changes: []int{1},
			want: []int{0, 5}, wantSyncs: []string{"", status.InSync}, wantSync: status.OutOfSync,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.maxSleeps = 14
			e.lib.Devices[0].Log = &nvmlfake.Log{}
			docs := &staticDocs{&config.Source{Name: "gpu-config", Data: map[string]string{config.ConfigKey: docYAML}}}
			e.a.Documents = docs
			if tc.decided {
				e.write(driver.ReadyFile, hostDrv)
				e.write(statefile.StateFileName, decided)
			}
			if tc.applied {
				e.write(driver.ReadyFile, hostDrv)
				e.write(statefile.StateFileName, `{"version":1,"apply":{"module":110711,"value":"applied","config":"`+
					config.Hash(docs.Snapshot())+`","time":"2026-01-01T00:00:00Z"}}`)
			}
			// Only a publish reads the node here: an idle or undecided
			// applier makes no decision.
			var publishes []int
			var syncs []string
			e.a.Cluster = &kube.Client{C: interceptor.NewClient(e.c, interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					err := c.Get(ctx, key, obj, opts...)
					if node, ok := obj.(*corev1.Node); ok {
						publishes = append(publishes, len(e.sleeps))
						syncs = append(syncs, node.Labels[publish.SyncLabel])
					}
					return err
				},
			})}
			e.a.Jitter = func(time.Duration) time.Duration { return 12 * time.Second }
			e.beforeSleep = func(n int) {
				if slices.Contains(tc.changes, n) {
					if tc.edit {
						docs.src = &config.Source{Name: "gpu-config", Data: map[string]string{config.ConfigKey: "version: v1\ndefault:\n  powerLimit: 400\n"}}
					}
					e.a.DocumentChanged()
				}
			}
			require.ErrorIs(t, e.run(), context.Canceled)
			require.Equal(t, tc.want, publishes)
			require.Empty(t, e.lib.Devices[0].Log.Writes)
			if tc.wantSyncs != nil {
				require.Equal(t, tc.wantSyncs, syncs)
				require.Equal(t, tc.wantSync, e.node().Labels[publish.SyncLabel])
			}
		})
	}
}
func TestNextBackoff(t *testing.T) {
	cases := map[string]struct {
		prev, want time.Duration
	}{
		"first":      {prev: 0, want: minBackoff},
		"doubles":    {prev: minBackoff, want: 2 * minBackoff},
		"at the cap": {prev: maxBackoff, want: maxBackoff},
		"capped":     {prev: 20 * time.Second, want: maxBackoff},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, nextBackoff(tc.prev))
		})
	}
}

func discardLogger() logrus.FieldLogger {
	l := logrus.New()
	l.SetOutput(io.Discard)
	return l
}
