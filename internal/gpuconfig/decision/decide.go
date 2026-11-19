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

// Package decision decides at each driver load whether the GPU configuration
// manager may write GPU settings: only when no allocator was released at this
// driver load (or earlier this boot and still on the node) and no GPU pod is
// on the node.
package decision

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/apply"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/config"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/statefile"
)

// Stack is the deployment the applier serves, which decides the evidence
// items it checks.
type Stack string

const (
	// ContainerStack is ClusterPolicy with the device plugin as allocator.
	ContainerStack Stack = "container"
	// DRAStack is GPUCluster with the DRA kubelet plugin as allocator.
	DRAStack Stack = "dra"
)

// ApplierDaemonSet returns the applier DaemonSet of a stack. Both stacks' pods
// also carry ApplierComponent, which k8s-driver-manager selects them by.
func ApplierDaemonSet(s Stack) string {
	if s == DRAStack {
		return "nvidia-gpu-config-manager-dra"
	}
	return "nvidia-gpu-config-manager"
}

// ApplierComponent is the app.kubernetes.io/component value of every applier
// pod.
const ApplierComponent = "nvidia-gpu-config-manager"

// Default re-check delays. Both run under the lock, so they delay the gates.
const (
	// A pod status can be stale right after a reboot.
	DefaultPodRecheck = 30 * time.Second
	// Kubelet deletes a stale ResourceSlice at most 30 s after its plugin
	// unregisters.
	DefaultSliceRecheck = 60 * time.Second
)

var (
	// ErrAPI marks a failed Kubernetes read the decision depends on. The
	// caller retries with backoff; the gates hold meanwhile.
	ErrAPI = errors.New("kubernetes API error")
	// ErrTransient marks a state file or local file error. The caller retries
	// at its next poll. A state file that doesn't parse also wraps
	// statefile.ErrParse.
	ErrTransient = errors.New("transient error")
)

// Config holds the applier's deployment parameters.
type Config struct {
	Stack     Stack
	NodeName  string
	Namespace string
	// ApplierDaemonSet is the applier's own DaemonSet, whose pods the pod
	// check ignores.
	ApplierDaemonSet string
	// ValidationsDir holds toolkit-ready and the state file.
	ValidationsDir string
	PodRecheck     time.Duration
	SliceRecheck   time.Duration
}

// Cluster is the Kubernetes API the decision reads. Every error except
// ErrClaimNotFound is an API error.
type Cluster interface {
	// GetNode reads the Node with a quorum read.
	GetNode(ctx context.Context, name string) (*corev1.Node, error)
	// ListNodePods lists the node's pods in non-terminal phases. The list must
	// be current, not served from a cache that may lag.
	ListNodePods(ctx context.Context, node string) ([]corev1.Pod, error)
	// ClaimDrivers returns the driver of each device allocated to a
	// ResourceClaim, or ErrClaimNotFound.
	ClaimDrivers(ctx context.Context, namespace, name string) ([]string, error)
	// NodeSliceDrivers returns the driver of each ResourceSlice for the node.
	NodeSliceDrivers(ctx context.Context, node string) ([]string, error)
}

// Decider runs decisions for one applier container.
type Decider struct {
	Config  Config
	Store   *statefile.Store
	Cluster Cluster
	NVML    apply.Library
	// LoadID reads the current load ID; ok is false while the module is
	// absent.
	LoadID func() (loadID uint64, ok bool, err error)
	// Document returns a snapshot of the document the node uses, or nil when
	// none exists.
	Document func() *config.Source
	// DriverReady checks that the driver is ready, right before NVML opens.
	DriverReady func() (bool, error)
	// Sleep waits d or until ctx is done.
	Sleep func(ctx context.Context, d time.Duration) error
}

// Outcome is how a decision ended.
type Outcome int

const (
	// None is the outcome returned with an error.
	None Outcome = iota
	// Idle: the state file already records a decision for this driver load.
	Idle
	// Applied: the settings were written and applied recorded.
	Applied
	// Skipped: skipped recorded, no setting written.
	Skipped
	// Retry: nothing written, because the driver isn't ready. Poll again.
	Retry
	// LoadChanged: nothing written, because the module is gone or has a new
	// load ID. The container rebinds to the new driver load.
	LoadChanged
)

// Result is the outcome of one decision.
type Result struct {
	Outcome Outcome
	// Reason says why a decision skipped.
	Reason string
	// Skips are settings a GPU reported as not supported. A failed apply
	// returns them too, with its error.
	Skips []apply.Skip
}

// decision is the state of one Decide call.
type decision struct {
	*Decider
	claimCache    map[string][]string
	claimNotFound map[string]bool
}

// Decide runs the decision for the container's driver load loadID. An error
// that wraps neither ErrAPI nor ErrTransient fails the apply: the applier
// exits non-zero.
func (d *Decider) Decide(ctx context.Context, loadID uint64) (Result, error) {
	// Idle test without the lock, so an idle applier never delays a gate.
	f, err := d.Store.Read()
	if err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrTransient, err)
	}
	if f.Decided(loadID) {
		return Result{Outcome: Idle}, nil
	}

	lock, err := d.Store.Lock()
	if err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrTransient, err)
	}
	defer func() { _ = lock.Unlock() }()

	// From this re-read to the final write, no gate can pass.
	f, err = lock.Read()
	if err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrTransient, err)
	}
	if changed, err := d.loadChanged(loadID); changed || err != nil {
		return Result{Outcome: LoadChanged}, err
	}
	if f.Decided(loadID) {
		return Result{Outcome: Idle}, nil
	}

	node, err := d.Cluster.GetNode(ctx, d.Config.NodeName)
	if err != nil {
		return Result{}, fmt.Errorf("%w: reading node: %w", ErrAPI, err)
	}
	// Every later step, and the hash the apply records, use this snapshot.
	doc := d.Document()

	dec := &decision{Decider: d, claimCache: map[string][]string{}, claimNotFound: map[string]bool{}}
	reason, reloadPods, err := dec.evidence(ctx, &f.State, loadID, node)
	if err != nil {
		return Result{}, err
	}
	if reason == "" {
		reason, err = dec.podCheck(ctx, f.GatePassedThisBoot(), reloadPods)
		if err != nil {
			return Result{}, err
		}
	}
	if reason != "" {
		return dec.skip(lock, loadID, &f.State, Result{Outcome: Skipped, Reason: reason})
	}

	res, err := dec.apply(ctx, loadID, doc)
	if err != nil || res.Outcome != Applied {
		if res.Outcome == Skipped {
			return dec.skip(lock, loadID, &f.State, res)
		}
		return res, err
	}
	if err := lock.WriteApply(loadID, statefile.Applied, config.Hash(doc)); err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrTransient, err)
	}
	return res, nil
}

// skip keeps apply.config only when the apply object is already at this
// driver load, which only a newer applier can leave.
func (d *decision) skip(lock *statefile.Lock, loadID uint64, st *statefile.State, res Result) (Result, error) {
	if err := lock.WriteApply(loadID, statefile.Skipped, st.ConfigAt(loadID)); err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrTransient, err)
	}
	return res, nil
}

func (d *Decider) loadChanged(loadID uint64) (bool, error) {
	cur, ok, err := d.LoadID()
	if err != nil {
		return false, fmt.Errorf("%w: %w", ErrTransient, err)
	}
	return !ok || cur != loadID, nil
}

func (d *decision) listPods(ctx context.Context) ([]corev1.Pod, error) {
	pods, err := d.Cluster.ListNodePods(ctx, d.Config.NodeName)
	if err != nil {
		return nil, fmt.Errorf("%w: listing node pods: %w", ErrAPI, err)
	}
	out := make([]corev1.Pod, 0, len(pods))
	for i := range pods {
		if !d.isApplierPod(&pods[i]) && !d.isManagementPod(&pods[i]) {
			out = append(out, pods[i])
		}
	}
	return out, nil
}

// podCheck returns why a GPU pod blocks the apply, or "". At a first load only
// pods still running PodRecheck later count: without an allocator no pod runs
// on a GPU, and status can be stale after a reboot. After a gate passed this
// boot, an idle pod can restart on its GPU without a gate, so every GPU pod
// counts; that variant checks reloadPods, the list the evidence step read.
func (d *decision) podCheck(ctx context.Context, reload bool, reloadPods []corev1.Pod) (string, error) {
	if reload {
		// listPods never returns a nil list, so nil means the evidence step
		// didn't read one; checking nothing would wrongly report clean.
		if reloadPods == nil {
			return "", errors.New("reload pod check has no pod list")
		}
		for i := range reloadPods {
			counts, err := d.isGPUPod(ctx, &reloadPods[i])
			if err != nil || counts {
				return podReason(&reloadPods[i], counts), err
			}
		}
		return "", nil
	}

	pods, err := d.listPods(ctx)
	if err != nil {
		return "", err
	}
	candidates := map[string]bool{}
	for i := range pods {
		counts, err := d.countsAtFirstLoad(ctx, &pods[i])
		if err != nil {
			return "", err
		}
		if counts {
			candidates[string(pods[i].UID)] = true
		}
	}
	if len(candidates) == 0 {
		return "", nil
	}
	if err := d.Sleep(ctx, d.Config.PodRecheck); err != nil {
		return "", fmt.Errorf("waiting to re-check GPU pods: %w", err)
	}
	pods, err = d.listPods(ctx)
	if err != nil {
		return "", err
	}
	for i := range pods {
		if !candidates[string(pods[i].UID)] {
			continue
		}
		counts, err := d.countsAtFirstLoad(ctx, &pods[i])
		if err != nil || counts {
			return podReason(&pods[i], counts), err
		}
	}
	return "", nil
}

func podReason(pod *corev1.Pod, counts bool) string {
	if !counts {
		return ""
	}
	return fmt.Sprintf("GPU pod %s/%s is on the node", pod.Namespace, pod.Name)
}

func (d *decision) countsAtFirstLoad(ctx context.Context, pod *corev1.Pod) (bool, error) {
	gpu, err := d.isGPUPod(ctx, pod)
	if err != nil || !gpu {
		return false, err
	}
	// A validation workload pod reaches the GPUs without an allocator, so it
	// counts running or not.
	return d.isValidationPod(pod) || isRunning(pod), nil
}

func (d *decision) apply(ctx context.Context, loadID uint64, doc *config.Source) (Result, error) {
	ready, err := d.DriverReady()
	if err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrTransient, err)
	}
	if !ready {
		return Result{Outcome: Retry}, nil
	}
	// A driver reload racing the open can fail NVML; that isn't an apply
	// failure, so report it as a load change.
	openFailed := func(err error) (Result, error) {
		if changed, lcErr := d.loadChanged(loadID); changed && lcErr == nil {
			return Result{Outcome: LoadChanged}, nil
		}
		return Result{}, err
	}
	if err := d.NVML.Init(); err != nil {
		return openFailed(fmt.Errorf("initializing NVML: %w", err))
	}
	defer func() { _ = d.NVML.Shutdown() }()

	n, err := d.NVML.DeviceCount()
	if err != nil {
		return openFailed(fmt.Errorf("NVML device count: %w", err))
	}
	devices := make([]apply.Device, n)
	for i := range devices {
		if devices[i], err = d.NVML.DeviceByIndex(i); err != nil {
			return openFailed(fmt.Errorf("NVML device %d: %w", i, err))
		}
	}
	// Open device handles pin the module, so a reload that raced the open
	// shows here as a new load ID.
	if changed, err := d.loadChanged(loadID); changed || err != nil {
		return Result{Outcome: LoadChanged}, err
	}

	for i, dev := range devices {
		mode, unsupported, modeErr := apply.UnsupportedMode(dev)
		if modeErr == nil && !unsupported {
			continue
		}
		// A GPU whose mode can't be read often can't report its UUID either,
		// and failing here would hold the gates at this driver load forever.
		gpu := fmt.Sprintf("NVML device %d", i)
		if uuid, err := dev.UUID(); err == nil {
			gpu = "GPU " + uuid
		}
		reason := fmt.Sprintf("%s virtualization mode: %s; the applier writes no settings in it", gpu, mode)
		if modeErr != nil {
			reason = fmt.Sprintf("%s virtualization mode can't be read: %v; the applier writes no settings", gpu, modeErr)
		}
		return Result{Outcome: Skipped, Reason: reason}, nil
	}

	if doc == nil {
		return Result{Outcome: Applied}, nil
	}
	parsed, err := config.ParseSource(doc)
	if err != nil {
		return Result{}, err
	}
	gpus := make([]config.GPU, n)
	for i, dev := range devices {
		if gpus[i], err = identify(dev); err != nil {
			return Result{}, fmt.Errorf("NVML device %d: %w", i, err)
		}
	}
	assignments, err := parsed.Resolve(gpus)
	if err != nil {
		return Result{}, err
	}
	targets := make([]apply.Target, n)
	for i := range targets {
		targets[i] = apply.Target{Device: devices[i], Assignment: assignments[i]}
	}
	skips, err := apply.Apply(targets)
	if err != nil {
		return Result{Skips: skips}, withSkips(err, skips)
	}
	return Result{Outcome: Applied, Skips: skips}, nil
}

// withSkips adds the settings skipped as not supported to an apply error,
// since the caller logs only the error.
func withSkips(err error, skips []apply.Skip) error {
	if len(skips) == 0 {
		return err
	}
	parts := make([]string, 0, len(skips))
	for _, s := range skips {
		parts = append(parts, s.UUID+" "+s.Setting)
	}
	return fmt.Errorf("%w; skipped as not supported: %s", err, strings.Join(parts, ", "))
}

func identify(dev apply.Device) (config.GPU, error) {
	var g config.GPU
	var err error
	if g.UUID, err = dev.UUID(); err != nil {
		return g, fmt.Errorf("UUID: %w", err)
	}
	if g.Name, err = dev.Name(); err != nil {
		return g, fmt.Errorf("name: %w", err)
	}
	if g.PCIDeviceID, err = dev.PCIDeviceID(); err != nil {
		return g, fmt.Errorf("PCI device ID: %w", err)
	}
	return g, nil
}
