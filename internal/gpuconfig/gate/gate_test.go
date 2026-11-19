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

package gate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/statefile"
)

const testLoadID = 110711

var errBoom = errors.New("boom")

type fakeCluster struct {
	exists    bool
	dsErr     error
	labels    map[string]string
	nodeErr   error
	dsReads   int
	nodeReads int
	// noDeadline counts reads made without a context deadline.
	noDeadline int
}

func (c *fakeCluster) DaemonSetExists(ctx context.Context, _, _ string) (bool, error) {
	c.dsReads++
	if _, ok := ctx.Deadline(); !ok {
		c.noDeadline++
	}
	return c.exists, c.dsErr
}

func (c *fakeCluster) NodeLabels(ctx context.Context, _ string) (map[string]string, error) {
	c.nodeReads++
	if _, ok := ctx.Deadline(); !ok {
		c.noDeadline++
	}
	return c.labels, c.nodeErr
}

type env struct {
	t       *testing.T
	dir     string
	store   *statefile.Store
	cluster *fakeCluster
	now     time.Time
	start   time.Time
	// module returns the load ID at the current time; 0 is absent.
	module func(now time.Time) uint64
	// onSleep runs before each sleep, for example to check the lock is free.
	onSleep   func()
	maxSleeps int
	sleeps    int
	jitter    time.Duration
	infos     []string
	warns     []string
	g         *Gate
}

func newEnv(t *testing.T) *env {
	e := &env{
		t:         t,
		dir:       t.TempDir(),
		cluster:   &fakeCluster{exists: true, labels: map[string]string{DeployLabel: "true"}},
		now:       time.Date(2026, 9, 26, 8, 0, 0, 0, time.UTC),
		module:    func(time.Time) uint64 { return testLoadID },
		maxSleeps: 200,
	}
	e.start = e.now
	e.store = statefile.New(e.dir)
	e.g = &Gate{
		Config: Config{NodeName: "node-1", Namespace: "gpu-operator", ApplierDaemonSet: "applier",
			Interval: DefaultInterval, NoModuleBound: DefaultNoModuleBound},
		Store:   e.store,
		Cluster: e.cluster,
		LoadID: func() (uint64, bool, error) {
			l := e.module(e.now)
			return l, l != 0, nil
		},
		Now: func() time.Time { return e.now },
		Sleep: func(_ context.Context, d time.Duration) error {
			if e.onSleep != nil {
				e.onSleep()
			}
			e.sleeps++
			if e.sleeps > e.maxSleeps {
				return context.Canceled
			}
			e.now = e.now.Add(d)
			return nil
		},
		Jitter: func(time.Duration) time.Duration { return e.jitter },
		Infof:  func(format string, args ...any) { e.infos = append(e.infos, fmt.Sprintf(format, args...)) },
		Warnf:  func(format string, args ...any) { e.warns = append(e.warns, fmt.Sprintf(format, args...)) },
	}
	return e
}

func (e *env) run() error {
	_, err := e.g.Run(context.Background())
	return err
}

// underLock reports whether the gate holds the state file lock.
func (e *env) underLock() bool {
	l, err := e.store.TryLock()
	if err != nil {
		return true
	}
	require.NoError(e.t, l.Unlock())
	return false
}

func (e *env) writeState(content string) {
	require.NoError(e.t, os.WriteFile(filepath.Join(e.dir, statefile.StateFileName), []byte(content), 0o600))
}

func (e *env) readState() string {
	b, err := os.ReadFile(filepath.Join(e.dir, statefile.StateFileName))
	if os.IsNotExist(err) {
		return ""
	}
	require.NoError(e.t, err)
	return string(b)
}

func (e *env) gate() *statefile.Gate {
	f, err := e.store.Read()
	require.NoError(e.t, err)
	return f.Gate
}

func (e *env) elapsed() time.Duration { return e.now.Sub(e.start) }

func TestApplierExpected(t *testing.T) {
	for value, want := range map[string]bool{
		"":                          true,
		"true":                      true,
		"paused-for-driver-upgrade": true,
		"false":                     false,
		"False":                     false,
		"True":                      false,
		"bogus":                     false,
	} {
		require.Equal(t, want, applierExpected(value), "%q", value)
	}
}

func TestReadExpected(t *testing.T) {
	cases := map[string]struct {
		cluster *fakeCluster
		want    bool
	}{
		"deployed and opted in":           {cluster: &fakeCluster{exists: true, labels: map[string]string{DeployLabel: "true"}}, want: true},
		"label absent":                    {cluster: &fakeCluster{exists: true, labels: map[string]string{}}, want: true},
		"opted out":                       {cluster: &fakeCluster{exists: true, labels: map[string]string{DeployLabel: "false"}}},
		"applier not deployed":            {cluster: &fakeCluster{labels: map[string]string{DeployLabel: "true"}}},
		"DaemonSet read fails":            {cluster: &fakeCluster{dsErr: errBoom}, want: true},
		"node read fails":                 {cluster: &fakeCluster{exists: true, nodeErr: errBoom}, want: true},
		"opted out, DaemonSet read fails": {cluster: &fakeCluster{dsErr: errBoom, labels: map[string]string{DeployLabel: "false"}}, want: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.g.Cluster = tc.cluster
			require.Equal(t, tc.want, e.g.readExpected(context.Background()))
			require.Zero(t, tc.cluster.noDeadline, "an API read had no timeout")
		})
	}
}

func TestModulePresent(t *testing.T) {
	cases := map[string]struct {
		state         string
		notExpected   bool
		wantPass      bool
		wantModule    uint64
		wantUnchanged bool
	}{
		"decided for this load: passes with the load ID": {
			state:    `{"version":1,"apply":{"module":110711,"value":"applied","config":"","time":"2026-01-01T00:00:00Z"}}`,
			wantPass: true, wantModule: testLoadID,
		},
		"decided for this load, not expected: passes with the load ID": {
			state:       `{"version":1,"apply":{"module":110711,"value":"skipped","config":"","time":"2026-01-01T00:00:00Z"}}`,
			notExpected: true, wantPass: true, wantModule: testLoadID,
		},
		"no decision, expected: holds": {
			wantUnchanged: true,
		},
		"decided at another load, expected: holds": {
			state:         `{"version":1,"apply":{"module":100,"value":"applied","config":"","time":"2026-01-01T00:00:00Z"}}`,
			wantUnchanged: true,
		},
		"pending for this load, expected: holds": {
			state:         `{"version":1,"apply":{"module":110711,"value":"pending","config":"","time":"2026-01-01T00:00:00Z"},"gate":{"module":110711,"time":"2026-01-01T00:00:00Z"}}`,
			wantUnchanged: true,
		},
		"no file, not expected: passes with the load ID": {
			notExpected: true, wantPass: true, wantModule: testLoadID,
		},
		"another load's pending, not expected: passes with the load ID": {
			state:       `{"version":1,"apply":{"module":100,"value":"pending","config":"","time":"2026-01-01T00:00:00Z"}}`,
			notExpected: true, wantPass: true, wantModule: testLoadID,
		},
		"pending for this load, not expected: passes with 0": {
			state:       `{"version":1,"apply":{"module":110711,"value":"pending","config":"","time":"2026-01-01T00:00:00Z"},"gate":{"module":110711,"time":"2026-01-01T00:00:00Z"}}`,
			notExpected: true, wantPass: true, wantModule: 0,
		},
		"unknown value for this load, not expected: passes with 0": {
			state:       `{"version":1,"apply":{"module":110711,"value":"future","config":"","time":"2026-01-01T00:00:00Z"}}`,
			notExpected: true, wantPass: true, wantModule: 0,
		},
		"state file doesn't parse: holds": {
			state:       `{"version":2}`,
			notExpected: true, wantUnchanged: true,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.maxSleeps = 20
			if tc.state != "" {
				e.writeState(tc.state)
			}
			if tc.notExpected {
				e.cluster.labels[DeployLabel] = "false"
			}
			err := e.run()
			if !tc.wantPass {
				require.ErrorIs(t, err, context.Canceled)
				if tc.wantUnchanged {
					require.Equal(t, tc.state, e.readState())
				}
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantModule, e.gate().Module)
		})
	}
}

func TestHoldsWithoutTheLock(t *testing.T) {
	e := newEnv(t)
	e.maxSleeps = 5
	e.onSleep = func() {
		l, err := e.store.TryLock()
		require.NoError(t, err, "the gate waited while holding the lock")
		require.NoError(t, l.Unlock())
	}
	require.ErrorIs(t, e.run(), context.Canceled)
}

func TestBusyLockWaits(t *testing.T) {
	e := newEnv(t)
	e.writeState(`{"version":1,"apply":{"module":110711,"value":"applied","config":"","time":"2026-01-01T00:00:00Z"}}`)
	held, err := e.store.Lock()
	require.NoError(t, err)
	e.onSleep = func() {
		if e.sleeps == 3 {
			require.NoError(t, held.Unlock())
		}
	}
	require.NoError(t, e.run())
	require.Equal(t, 4, e.sleeps)
	require.Equal(t, uint64(testLoadID), e.gate().Module)
}

func TestPassReason(t *testing.T) {
	cases := map[string]struct {
		setup func(e *env)
		want  string
	}{
		"no applier DaemonSet": {
			setup: func(e *env) { e.cluster.exists = false },
			want:  "no GPU configuration manager is expected: DaemonSet gpu-operator/applier doesn't exist",
		},
		"opted out": {
			setup: func(e *env) { e.cluster.labels[DeployLabel] = "false" },
			want:  `no GPU configuration manager is expected: node label nvidia.com/gpu.deploy.gpu-config-manager="false"`,
		},
		"decided": {
			setup: func(e *env) {
				e.writeState(`{"version":1,"apply":{"module":110711,"value":"applied","config":"","time":"2026-01-01T00:00:00Z"}}`)
			},
			want: "the GPU configuration manager decided for this driver load (load ID 110711)",
		},
		"no module, not expected": {
			setup: func(e *env) {
				e.cluster.exists = false
				e.module = func(time.Time) uint64 { return 0 }
			},
			want: "the nvidia module isn't loaded and no GPU configuration manager is expected: DaemonSet gpu-operator/applier doesn't exist",
		},
		"no module, expected": {
			setup: func(e *env) { e.module = func(time.Time) uint64 { return 0 } },
			want:  "the nvidia module isn't loaded after 5m0s",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			tc.setup(e)
			reason, err := e.g.Run(context.Background())
			require.NoError(t, err)
			require.Equal(t, tc.want, reason)
		})
	}
}

func TestNoApplierPassesAtTheFirstIteration(t *testing.T) {
	e := newEnv(t)
	e.cluster.exists = false
	e.jitter = 30 * time.Second
	require.NoError(t, e.run())
	require.Zero(t, e.sleeps)
	require.Equal(t, uint64(testLoadID), e.gate().Module)
	require.Empty(t, e.infos, "a reason found before the first read was logged")
}

func TestReadsRepeatEvery30To60s(t *testing.T) {
	e := newEnv(t)
	e.jitter = 10 * time.Second
	e.maxSleeps = 60 // 5 minutes at 5 s
	require.ErrorIs(t, e.run(), context.Canceled)
	// First read at 0 s, then every 40 s.
	require.Equal(t, 8, e.cluster.dsReads)
}

func TestNoModule(t *testing.T) {
	t.Run("expected: passes with 0 after the bound", func(t *testing.T) {
		e := newEnv(t)
		e.module = func(time.Time) uint64 { return 0 }
		require.NoError(t, e.run())
		require.GreaterOrEqual(t, e.elapsed(), DefaultNoModuleBound)
		require.Less(t, e.elapsed(), DefaultNoModuleBound+DefaultInterval+time.Second)
		require.Equal(t, uint64(0), e.gate().Module)
	})
	t.Run("not expected: passes with 0 after the first read", func(t *testing.T) {
		e := newEnv(t)
		e.module = func(time.Time) uint64 { return 0 }
		e.cluster.labels[DeployLabel] = "false"
		require.NoError(t, e.run())
		require.Equal(t, time.Duration(0), e.elapsed())
		require.Equal(t, uint64(0), e.gate().Module)
	})
	t.Run("the timer restarts when the module is seen", func(t *testing.T) {
		e := newEnv(t)
		// Absent, present at 2 min for one iteration, then absent again.
		e.module = func(now time.Time) uint64 {
			if d := now.Sub(e.start); d >= 2*time.Minute && d < 2*time.Minute+DefaultInterval {
				return testLoadID
			}
			return 0
		}
		require.NoError(t, e.run())
		require.GreaterOrEqual(t, e.elapsed(), 2*time.Minute+DefaultInterval+DefaultNoModuleBound)
		require.Equal(t, uint64(0), e.gate().Module)
	})
	t.Run("the timer restarts when the module is seen under the lock", func(t *testing.T) {
		e := newEnv(t)
		seen := false
		e.g.LoadID = func() (uint64, bool, error) {
			if !seen && e.underLock() {
				seen = true
				return testLoadID, true, nil
			}
			return 0, false, nil
		}
		require.NoError(t, e.run())
		require.True(t, seen)
		require.GreaterOrEqual(t, e.elapsed(), 2*DefaultNoModuleBound)
		require.Equal(t, uint64(0), e.gate().Module)
	})
	t.Run("parse error holds", func(t *testing.T) {
		e := newEnv(t)
		e.module = func(time.Time) uint64 { return 0 }
		e.cluster.labels[DeployLabel] = "false"
		e.writeState(`{}`)
		e.maxSleeps = 10
		require.ErrorIs(t, e.run(), context.Canceled)
		require.Equal(t, `{}`, e.readState())
	})
	t.Run("module appears under the lock: takes the module-present path", func(t *testing.T) {
		e := newEnv(t)
		e.cluster.labels[DeployLabel] = "false"
		calls := 0
		e.g.LoadID = func() (uint64, bool, error) {
			calls++
			if calls <= 1 {
				return 0, false, nil
			}
			return testLoadID, true, nil
		}
		require.NoError(t, e.run())
		require.Equal(t, uint64(testLoadID), e.gate().Module)
		require.Equal(t, 1, e.sleeps)
	})
}

func TestModuleChangesWhilePassing(t *testing.T) {
	e := newEnv(t)
	e.writeState(`{"version":1,"apply":{"module":110711,"value":"applied","config":"","time":"2026-01-01T00:00:00Z"}}`)
	calls := 0
	e.g.LoadID = func() (uint64, bool, error) {
		calls++
		if calls <= 2 {
			return testLoadID, true, nil
		}
		return testLoadID + 1, true, nil
	}
	e.maxSleeps = 5
	require.ErrorIs(t, e.run(), context.Canceled)
	// The write stays: a recorded pass that didn't happen only adds evidence.
	require.Equal(t, uint64(testLoadID), e.gate().Module)
}

func TestLoadIDErrorUnderTheLock(t *testing.T) {
	cases := map[string]struct {
		notExpected bool
		// Every failEvery-th read under the lock fails.
		failEvery int
		module    uint64
	}{
		"no module, rereading":     {notExpected: true, failEvery: 1},
		"module present, reading":  {failEvery: 1, module: testLoadID},
		"module present, checking": {failEvery: 2, module: testLoadID},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.maxSleeps = 5
			if tc.notExpected {
				e.cluster.labels[DeployLabel] = "false"
			}
			e.writeState(`{"version":1,"apply":{"module":110711,"value":"applied","config":"","time":"2026-01-01T00:00:00Z"}}`)
			locked := 0
			e.g.LoadID = func() (uint64, bool, error) {
				if e.underLock() {
					locked++
					if locked%tc.failEvery == 0 {
						return 0, false, errBoom
					}
				}
				return tc.module, tc.module != 0, nil
			}
			require.ErrorIs(t, e.run(), context.Canceled)
			require.Empty(t, e.infos)
			require.NotEmpty(t, e.warns)
			for _, w := range e.warns {
				require.Equal(t, "waiting: reading the load ID: boom", w)
			}
		})
	}
}

func TestLoadIDErrorWaits(t *testing.T) {
	e := newEnv(t)
	e.g.LoadID = func() (uint64, bool, error) { return 0, false, errBoom }
	e.maxSleeps = 3
	require.ErrorIs(t, e.run(), context.Canceled)
	require.Empty(t, e.readState())
}
