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

// Package gate holds a node's GPU allocators until the applier has decided for
// the current driver load, and records every pass in the state file so that a
// later decision sees that allocators were released.
package gate

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/statefile"
)

// DeployLabel places the applier on a node. It must match the label the
// operator sets; a test in controllers checks that it does.
const DeployLabel = "nvidia.com/gpu.deploy.gpu-config-manager"

const (
	DefaultInterval = 5 * time.Second
	// DefaultNoModuleBound lets the gate pass on a node where nothing loads
	// the nvidia module before the gate runs.
	DefaultNoModuleBound = 5 * time.Minute
)

const (
	// After the first read, whether the applier is expected is read again
	// every readInterval plus up to readJitter.
	readInterval = 30 * time.Second
	readJitter   = 30 * time.Second
	// readTimeout bounds the API reads: while a read hangs, the gate doesn't
	// check the state file, so it would miss a decision recorded meanwhile.
	readTimeout = 30 * time.Second
)

// Cluster is the Kubernetes API the gate reads. Both reads are served from
// the API server's cache.
type Cluster interface {
	// DaemonSetExists reports whether the DaemonSet exists; NotFound is
	// false with no error.
	DaemonSetExists(ctx context.Context, namespace, name string) (bool, error)
	NodeLabels(ctx context.Context, name string) (map[string]string, error)
}

// Config holds the gate's parameters.
type Config struct {
	NodeName  string
	Namespace string
	// ApplierDaemonSet is the applier DaemonSet of the gate's stack.
	ApplierDaemonSet string
	Interval         time.Duration
	NoModuleBound    time.Duration
}

// Gate runs one gate.
type Gate struct {
	Config  Config
	Store   *statefile.Store
	Cluster Cluster
	// LoadID reads the current load ID; ok is false while the module is
	// absent.
	LoadID func() (l uint64, ok bool, err error)
	Now    func() time.Time
	// Sleep waits d or until ctx is done.
	Sleep func(ctx context.Context, d time.Duration) error
	// Jitter returns a random duration in [0, max).
	Jitter func(max time.Duration) time.Duration
	// Infof and Warnf, if set, receive why the gate holds on each iteration;
	// errors go to Warnf.
	Infof func(format string, args ...any)
	Warnf func(format string, args ...any)

	expected bool
	// notExpected says why no applier is expected, while expected is false.
	notExpected  string
	nextRead     time.Time
	absentSince  time.Time
	moduleAbsent bool
	passReason   string
}

// Run holds until the gate passes, and returns why it passed then. It returns
// early, with an error, only when ctx is done.
func (g *Gate) Run(ctx context.Context) (string, error) {
	g.nextRead = g.Now()
	for {
		if !g.Now().Before(g.nextRead) {
			g.expected = g.readExpected(ctx)
			g.nextRead = g.Now().Add(readInterval + g.Jitter(readJitter))
		}
		if g.iterate() {
			return g.passReason, nil
		}
		if err := g.Sleep(ctx, g.Config.Interval); err != nil {
			return "", err
		}
	}
}

// readExpected reports whether an applier will decide on this node. A failed
// read counts as expected.
func (g *Gate) readExpected(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	exists, err := g.Cluster.DaemonSetExists(ctx, g.Config.Namespace, g.Config.ApplierDaemonSet)
	if err != nil {
		g.warnf("reading the applier DaemonSet %s, treating the applier as expected: %v", g.Config.ApplierDaemonSet, err)
		return true
	}
	if !exists {
		g.notExpected = fmt.Sprintf("DaemonSet %s/%s doesn't exist", g.Config.Namespace, g.Config.ApplierDaemonSet)
		return false
	}
	labels, err := g.Cluster.NodeLabels(ctx, g.Config.NodeName)
	if err != nil {
		g.warnf("reading node %s, treating the applier as expected: %v", g.Config.NodeName, err)
		return true
	}
	value := labels[DeployLabel]
	g.notExpected = fmt.Sprintf("node label %s=%q", DeployLabel, value)
	return applierExpected(value)
}

// applierExpected reports whether a deploy label value places the applier on
// the node. An absent or empty label holds the node until the operator sets
// it. A paused value is one k8s-driver-manager writes while it reloads the
// driver.
func applierExpected(deployLabel string) bool {
	return deployLabel == "" || deployLabel == "true" || strings.Contains(deployLabel, "paused-for-")
}

func (g *Gate) iterate() bool {
	_, present, err := g.LoadID()
	if err != nil {
		g.warnf("waiting: reading the load ID: %v", err)
		return false
	}
	if present {
		// The no-module timer restarts whenever the module is seen.
		g.moduleAbsent = false
		return g.modulePresent()
	}
	if !g.moduleAbsent {
		g.moduleAbsent = true
		g.absentSince = g.Now()
	}
	if g.expected && g.Now().Sub(g.absentSince) < g.Config.NoModuleBound {
		g.infof("waiting: the nvidia module isn't loaded")
		return false
	}
	return g.noModule()
}

// noModule passes with module 0: there is no load ID to record, and a module
// that loads afterwards sees the 0 as evidence and skips.
func (g *Gate) noModule() bool {
	lock, ok := g.tryLock()
	if !ok {
		return false
	}
	defer func() { _ = lock.Unlock() }()

	_, present, err := g.LoadID()
	if err != nil {
		g.warnf("waiting: reading the load ID: %v", err)
		return false
	}
	if present {
		g.moduleAbsent = false
		g.infof("waiting: the nvidia module appeared")
		return false
	}
	if _, err := lock.Read(); err != nil {
		g.warnf("waiting: reading the state file: %v", err)
		return false
	}
	if err := lock.WriteGatePass(0); err != nil {
		g.warnf("waiting: recording the pass: %v", err)
		return false
	}
	if g.expected {
		g.passReason = fmt.Sprintf("the nvidia module isn't loaded after %s", g.Config.NoModuleBound)
	} else {
		g.passReason = "the nvidia module isn't loaded and no GPU configuration manager is expected: " + g.notExpected
	}
	return true
}

// modulePresent passes once the applier has decided for the current driver
// load, or when no applier is expected.
func (g *Gate) modulePresent() bool {
	lock, ok := g.tryLock()
	if !ok {
		return false
	}
	defer func() { _ = lock.Unlock() }()

	l, present, err := g.LoadID()
	if err != nil {
		g.warnf("waiting: reading the load ID: %v", err)
		return false
	}
	if !present {
		g.infof("waiting: the nvidia module is gone")
		return false
	}
	f, err := lock.Read()
	if err != nil {
		g.warnf("waiting: reading the state file: %v", err)
		return false
	}

	var m uint64
	reason := "no GPU configuration manager is expected: " + g.notExpected
	switch {
	case f.Decided(l):
		m = l
		reason = fmt.Sprintf("the GPU configuration manager decided for this driver load (load ID %d)", l)
	case g.expected:
		g.infof("waiting: the applier hasn't decided for the current driver load (load ID %d)", l)
		return false
	case f.Undecided(l):
		// An applier that leaves this load undecided ignores a pass recorded
		// at this load, so record 0 for it to see the pass.
		m = 0
	default:
		m = l
	}
	if err := lock.WriteGatePass(m); err != nil {
		g.warnf("waiting: recording the pass: %v", err)
		return false
	}

	// Pass only if the module read above is still the loaded one. The write
	// stays either way: a recorded pass that didn't happen only adds
	// evidence.
	again, present, err := g.LoadID()
	if err != nil {
		g.warnf("waiting: reading the load ID: %v", err)
		return false
	}
	if !present || again != l {
		g.infof("waiting: the nvidia module changed while passing")
		return false
	}
	g.passReason = reason
	return true
}

// tryLock takes the lock without waiting: a gate never waits while holding
// it, so it can't delay the applier or another gate.
func (g *Gate) tryLock() (*statefile.LockedStore, bool) {
	lock, err := g.Store.TryLock()
	if err != nil {
		if !errors.Is(err, statefile.ErrLockBusy) {
			g.warnf("waiting: taking the state file lock: %v", err)
		}
		return nil, false
	}
	return lock, true
}

func (g *Gate) infof(format string, args ...any) {
	if g.Infof != nil {
		g.Infof(format, args...)
	}
}

func (g *Gate) warnf(format string, args ...any) {
	if g.Warnf != nil {
		g.Warnf(format, args...)
	}
}
