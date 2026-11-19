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

// Package applier runs the applier's main loop: it binds to one driver load,
// decides once at it, publishes status, and exits when the driver load
// changes so that its restart binds to the new one.
package applier

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	appsv1 "k8s.io/api/apps/v1"

	"github.com/sirupsen/logrus"

	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/apply"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/config"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/decision"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/loadid"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/publish"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/statefile"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/status"
)

const (
	DefaultPollInterval  = 5 * time.Second
	DefaultPublishJitter = 60 * time.Second
	minBackoff           = time.Second
	maxBackoff           = 30 * time.Second
)

// Cluster is the Kubernetes API the applier uses.
type Cluster interface {
	decision.Cluster
	publish.NodeClient
	ListDaemonSetsFromCache(ctx context.Context, namespace, labelSelector string) ([]appsv1.DaemonSet, error)
}

// Documents gives the document the node uses.
type Documents interface {
	Snapshot() *config.Source
}

// Config holds the applier's parameters.
type Config struct {
	NodeName       string
	Namespace      string
	Stack          decision.Stack
	ValidationsDir string
	PollInterval   time.Duration
	PublishJitter  time.Duration
}

// Applier runs one applier container.
type Applier struct {
	Config    Config
	Store     *statefile.Store
	Cluster   Cluster
	Documents Documents
	// LoadID reads the current load ID; ok is false while the module is
	// absent.
	LoadID func() (id uint64, ok bool, err error)
	// OpenNVML returns NVML for the driver root as mounted in the container.
	OpenNVML func(driverRoot string) (apply.Library, error)
	// Sleep waits d or until ctx is done.
	Sleep func(ctx context.Context, d time.Duration) error
	// Jitter returns a random duration in [0, max).
	Jitter func(max time.Duration) time.Duration
	Log    logrus.FieldLogger

	managed     bool
	boundLoadID uint64
	nvml        apply.Library
	decider     *decision.Decider
	publisher   *publish.Publisher
	// decided is set once this container has made a decision or found one
	// already recorded for this driver load; it publishes nothing before.
	decided     bool
	pendingRead bool
	backoff     time.Duration
	// publishQueued is set when a status publish is due after publishIn: a
	// document changed, or a publish failed and is retried.
	publishQueued bool
	publishIn     time.Duration

	// documentChanged is the only field set from another goroutine: the
	// informer's, through DocumentChanged.
	documentChanged atomic.Bool
}

// Run runs the applier until its driver load changes, and returns nil then so
// that the container exits 0 and its restart binds to the new driver load.
// An error fails the container.
func (a *Applier) Run(ctx context.Context) error {
	a.publisher = &publish.Publisher{NodeName: a.Config.NodeName, Client: a.Cluster}

	for {
		rebind, err := a.poll(ctx)
		if err != nil || rebind {
			return err
		}
		wait := a.Config.PollInterval
		if a.backoff > 0 {
			wait = a.backoff
		}
		if err := a.Sleep(ctx, wait); err != nil {
			return err
		}
		a.checkAndPublish(ctx, wait)
	}
}

// poll reports rebind when the container should exit 0.
func (a *Applier) poll(ctx context.Context) (rebind bool, err error) {
	id, present, err := a.LoadID()
	if err != nil {
		a.Log.WithError(err).Warn("reading the load ID")
		return false, nil
	}
	if !present {
		return false, nil
	}
	if a.boundLoadID == 0 {
		// Whether the operator manages the driver decides whether readiness
		// needs .driver-ctr-ready. A driver DaemonSet can appear after the
		// container starts, so it's re-read until the container binds.
		managed, err := loadid.ManagedByOperator(ctx, cachedDaemonSets(a.Cluster.ListDaemonSetsFromCache), a.Config.Namespace)
		if err != nil {
			a.backoff = nextBackoff(a.backoff)
			a.Log.WithError(err).WithField("backoff", a.backoff).Warn("listing driver DaemonSets")
			return false, nil
		}
		a.backoff = 0
		a.managed = managed
	}
	info, ready, err := loadid.Ready(a.Config.ValidationsDir, a.managed)
	if err != nil {
		a.Log.WithError(err).Warn("checking driver readiness")
		return false, nil
	}

	if a.boundLoadID == 0 {
		// The container binds to the first driver load it sees ready, and
		// loads NVML from that load's driver root.
		if !ready {
			return false, nil
		}
		if err := a.bind(id, info); err != nil {
			return false, err
		}
	}
	if id != a.boundLoadID {
		if ready {
			a.Log.WithFields(logrus.Fields{"old": a.boundLoadID, "new": id}).Info("the driver load changed; exiting to bind to the new one")
			return true, nil
		}
		return false, nil
	}

	return false, a.decide(ctx)
}

func (a *Applier) bind(id uint64, info *loadid.DriverInfo) error {
	lib, err := a.OpenNVML(info.DriverRootCtrPath)
	if err != nil {
		return fmt.Errorf("loading NVML from %s: %w", info.DriverRootCtrPath, err)
	}
	a.boundLoadID, a.nvml = id, lib
	a.decider = &decision.Decider{
		Config: decision.Config{
			Stack:            a.Config.Stack,
			NodeName:         a.Config.NodeName,
			Namespace:        a.Config.Namespace,
			ApplierDaemonSet: decision.ApplierDaemonSet(a.Config.Stack),
			ValidationsDir:   a.Config.ValidationsDir,
			PodRecheck:       decision.DefaultPodRecheck,
			SliceRecheck:     decision.DefaultSliceRecheck,
		},
		Store:    a.Store,
		Cluster:  a.Cluster,
		NVML:     lib,
		LoadID:   a.LoadID,
		Document: a.Documents.Snapshot,
		DriverReady: func() (bool, error) {
			_, ok, err := loadid.Ready(a.Config.ValidationsDir, a.managed)
			return ok, err
		},
		Sleep: a.Sleep,
	}
	a.Log.WithFields(logrus.Fields{"loadID": id, "driverRoot": info.DriverRootCtrPath}).Info("bound to driver load")
	return nil
}

// decide runs the decision, and publishes after one completes or at the
// first idle test.
func (a *Applier) decide(ctx context.Context) error {
	res, err := a.decider.Decide(ctx, a.boundLoadID)
	if err != nil {
		return a.handleError(ctx, err)
	}
	a.backoff = 0

	switch res.Outcome {
	case decision.Idle:
		if !a.decided {
			a.decided = true
			return a.statusAndPublish(ctx)
		}
		// The first idle test may have found the driver not ready, so the
		// ECC modes weren't read; read them once it is.
		if !a.pendingRead {
			if err := a.readPending(); err != nil {
				return err
			}
			if a.pendingRead {
				a.publish(ctx)
				return nil
			}
		}
	case decision.Applied, decision.Skipped:
		for _, s := range res.Skips {
			a.Log.WithFields(logrus.Fields{"gpu": s.UUID, "setting": s.Setting}).Info("setting not supported on this GPU; skipped")
		}
		a.Log.WithFields(logrus.Fields{"loadID": a.boundLoadID, "outcome": outcomeName(res.Outcome), "reason": res.Reason}).Info("decided")
		a.decided = true
		return a.statusAndPublish(ctx)
	}
	return nil
}

// handleError classifies a failed decision. It returns an error only for a
// failure that fails the container.
func (a *Applier) handleError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, statefile.ErrParse):
		a.Log.WithError(err).Error("the state file doesn't parse; writing nothing")
		return nil
	case errors.Is(err, decision.ErrTransient):
		a.Log.WithError(err).Warn("retrying at the next poll")
		return nil
	case errors.Is(err, decision.ErrAPI):
		a.backoff = nextBackoff(a.backoff)
		a.Log.WithError(err).WithField("backoff", a.backoff).Warn("Kubernetes API error; retrying")
		return nil
	default:
		return err
	}
}

// statusAndPublish fails the container if NVML fails to initialize, even on a node in service.
func (a *Applier) statusAndPublish(ctx context.Context) error {
	if err := a.readPending(); err != nil {
		return err
	}
	a.publish(ctx)
	return nil
}

// readPending reads the GPUs' ECC modes for pending-activation when the
// driver is ready, until one read succeeds.
func (a *Applier) readPending() error {
	if a.pendingRead {
		return nil
	}
	if _, ready, err := loadid.Ready(a.Config.ValidationsDir, a.managed); err != nil || !ready {
		return nil
	}
	value, known, readErrs, err := publish.PendingActivation(a.nvml)
	if err != nil {
		return err
	}
	if !known {
		// Idle polls retry the read.
		a.Log.WithError(errors.Join(readErrs...)).Warn("reading ECC modes; pending-activation keeps its value until a read succeeds")
		return nil
	}
	a.publisher.SetPendingActivation(value)
	a.pendingRead = true
	return nil
}

// publish writes the status labels. A failure queues a retry for the next
// poll.
func (a *Applier) publish(ctx context.Context) {
	f, err := a.Store.Read()
	if err != nil {
		a.Log.WithError(err).Warn("reading the state file for status")
		a.publishQueued, a.publishIn = true, 0
		return
	}
	sync := status.Sync(a.boundLoadID, &f.State, config.Hash(a.Documents.Snapshot()))
	if err := a.publisher.Publish(ctx, sync); err != nil {
		a.Log.WithError(err).Warn("publishing status")
		a.publishQueued, a.publishIn = true, 0
	}
}

// DocumentChanged records that a watched document changed. It runs on the
// informer's goroutine; Run publishes the new status after a random delay.
func (a *Applier) DocumentChanged() {
	a.documentChanged.Store(true)
}

// checkAndPublish runs the queued publish once its delay has passed. A
// document change queues one after a random delay, so an edit to a shared
// document doesn't make every node patch at once. It starts no decision, and
// publishes nothing before the first decision. elapsed is the time since the
// last call.
func (a *Applier) checkAndPublish(ctx context.Context, elapsed time.Duration) {
	if a.publishQueued {
		a.publishIn -= elapsed
	} else if a.documentChanged.Swap(false) {
		a.publishQueued = true
		a.publishIn = a.Jitter(a.Config.PublishJitter)
	}
	if !a.publishQueued || a.publishIn > 0 {
		return
	}
	a.publishQueued = false
	// The publish reads the current document, so it covers changes made
	// while it was queued.
	a.documentChanged.Store(false)
	if a.decided {
		a.publish(ctx)
	}
}

// RetryAPI runs fn until it succeeds or ctx is done, backing off from
// minBackoff to maxBackoff between attempts.
func RetryAPI(ctx context.Context, sleep func(context.Context, time.Duration) error, log logrus.FieldLogger, what string, fn func() error) error {
	var backoff time.Duration
	for {
		err := fn()
		if err == nil {
			return nil
		}
		backoff = nextBackoff(backoff)
		log.WithError(err).WithField("backoff", backoff).Warn(what + " failed; retrying")
		if err := sleep(ctx, backoff); err != nil {
			return err
		}
	}
}

func nextBackoff(d time.Duration) time.Duration {
	return min(max(2*d, minBackoff), maxBackoff)
}

// cachedDaemonSets adapts a cache read to loadid.DaemonSetLister.
type cachedDaemonSets func(ctx context.Context, namespace, labelSelector string) ([]appsv1.DaemonSet, error)

func (f cachedDaemonSets) ListDaemonSets(ctx context.Context, namespace, labelSelector string) ([]appsv1.DaemonSet, error) {
	return f(ctx, namespace, labelSelector)
}

func outcomeName(o decision.Outcome) string {
	switch o {
	case decision.Applied:
		return "applied"
	case decision.Skipped:
		return "skipped"
	default:
		return fmt.Sprint(int(o))
	}
}
