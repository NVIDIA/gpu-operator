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
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/apply"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/apply/fake"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/config"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/statefile"
)

const (
	// A gate passed at an earlier driver load: the reload variant.
	reloadState = `{"version":1,"apply":{"module":100,"value":"applied","config":"sha256:old","time":"2026-01-01T00:00:00Z"},"gate":{"module":100,"time":"2026-01-01T00:00:00Z"}}`
	docYAML     = "version: v1\ndefault:\n  powerLimit: 500\n"
)

var errBoom = errors.New("boom")

func TestEvidence(t *testing.T) {
	cases := map[string]struct {
		stack    Stack
		state    string
		setup    func(*env)
		wantSkip bool
		// wantSleeps are the re-check waits the decision makes.
		wantSleeps []time.Duration
	}{
		"nothing: first load applies": {stack: ContainerStack},
		"a gate passed at this driver load": {
			stack:    DRAStack,
			state:    `{"version":1,"gate":{"module":110711,"time":"2026-01-01T00:00:00Z"}}`,
			wantSkip: true,
		},
		"a gate passed with no decision": {
			stack:    ContainerStack,
			state:    `{"version":1,"gate":{"module":0,"time":"2026-01-01T00:00:00Z"}}`,
			wantSkip: true,
		},
		"toolkit-ready on the container stack": {
			stack: ContainerStack, setup: func(e *env) { e.touch(ToolkitReadyFile) }, wantSkip: true,
		},
		"toolkit-ready ignored on the DRA stack": {
			stack: DRAStack, setup: func(e *env) { e.touch(ToolkitReadyFile) },
		},
		"GPU allocatable": {
			stack: ContainerStack, wantSkip: true,
			setup: func(e *env) { e.allocatable("nvidia.com/gpu", "8") },
		},
		"shared GPU allocatable": {
			stack: ContainerStack, wantSkip: true,
			setup: func(e *env) { e.allocatable("nvidia.com/gpu.shared", "16") },
		},
		"MIG allocatable": {
			stack: ContainerStack, wantSkip: true,
			setup: func(e *env) { e.allocatable("nvidia.com/mig-1g.10gb", "7") },
		},
		"renamed GPU allocatable": {
			stack: ContainerStack, wantSkip: true,
			setup: func(e *env) { e.allocatable("nvidia.com/a100", "8") },
		},
		"another domain's allocatable": {
			stack: ContainerStack, setup: func(e *env) { e.allocatable("example.com/gpu", "8") },
		},
		"zero GPU allocatable": {
			stack: ContainerStack, setup: func(e *env) { e.allocatable("nvidia.com/gpu", "0") },
		},
		"allocatable ignored on the DRA stack": {
			stack: DRAStack, setup: func(e *env) { e.allocatable("nvidia.com/gpu", "8") },
		},
		"slice still there after the re-check": {
			stack: DRAStack, wantSkip: true, wantSleeps: []time.Duration{DefaultSliceRecheck},
			setup: func(e *env) { e.cluster.sliceReads = [][]string{{gpuDriver}} },
		},
		"compute-domain slice counts": {
			stack: DRAStack, wantSkip: true, wantSleeps: []time.Duration{DefaultSliceRecheck},
			setup: func(e *env) { e.cluster.sliceReads = [][]string{{computeDomainDriver}} },
		},
		"a slice that appeared during the wait counts": {
			stack: DRAStack, wantSkip: true, wantSleeps: []time.Duration{DefaultSliceRecheck},
			setup: func(e *env) { e.cluster.sliceReads = [][]string{{gpuDriver}, {computeDomainDriver}} },
		},
		"stale slice gone after the re-check": {
			stack: DRAStack, wantSleeps: []time.Duration{DefaultSliceRecheck},
			setup: func(e *env) { e.cluster.sliceReads = [][]string{{gpuDriver}, {}} },
		},
		"another driver's slice": {
			stack: DRAStack, setup: func(e *env) { e.cluster.sliceReads = [][]string{{"example.com"}} },
		},
		"slices ignored on the container stack": {
			stack: ContainerStack, setup: func(e *env) { e.cluster.sliceReads = [][]string{{gpuDriver}} },
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, tc.stack)
			if tc.state != "" {
				e.writeState(tc.state)
			}
			if tc.setup != nil {
				tc.setup(e)
			}
			res, err := e.decide()
			require.NoError(t, err)
			require.Equal(t, tc.wantSleeps, e.sleeps)
			if tc.wantSkip {
				require.Equal(t, Skipped, res.Outcome)
				require.NotEmpty(t, res.Reason)
				require.Empty(t, e.log.Calls, "NVML devices touched on a skip")
				require.Equal(t, statefile.Skipped, e.state().Apply.Value)
				return
			}
			require.Equal(t, Applied, res.Outcome, res.Reason)
			require.NotEmpty(t, e.log.Calls)
			require.True(t, e.state().AppliedAt(testLoadID))
		})
	}
}

func (e *env) allocatable(name, qty string) {
	e.cluster.node.Status.Allocatable = corev1.ResourceList{corev1.ResourceName(name): resource.MustParse(qty)}
}

func TestPodRules(t *testing.T) {
	allocator := func(opts ...podOpt) corev1.Pod {
		base := []podOpt{inNS(operatorNS), label("app", "nvidia-device-plugin-daemonset"), initContainers(toolkitContainer)}
		return newPod("device-plugin", append(base, opts...)...)
	}
	gate := func(ds string, opts ...podOpt) corev1.Pod {
		base := []podOpt{inNS(operatorNS), ownedBy(ds), initContainers("driver-validation", GateContainer, toolkitContainer)}
		return newPod("gate", append(base, opts...)...)
	}
	cases := map[string]struct {
		stack    Stack
		pod      corev1.Pod
		wantSkip bool
	}{
		"allocator still waiting for toolkit-ready": {
			stack: ContainerStack,
			pod:   allocator(status(toolkitContainer, running()), status("main", waiting())),
		},
		"allocator past toolkit-validation": {
			stack: ContainerStack, wantSkip: true,
			pod: allocator(status(toolkitContainer, terminated())),
		},
		"allocator killed while waiting for toolkit-ready": {
			stack: ContainerStack,
			pod:   allocator(terminating(), status(toolkitContainer, killed()), status("main", waiting())),
		},
		"allocator main container running": {
			stack: ContainerStack, wantSkip: true,
			pod: allocator(status(toolkitContainer, running()), status("main", running())),
		},
		"allocator main container restarted": {
			stack: ContainerStack, wantSkip: true,
			pod: allocator(status(toolkitContainer, running()), statusWithLast("main", waiting(), terminated())),
		},
		"MPS control daemon past its wait": {
			stack: ContainerStack, wantSkip: true,
			pod: newPod("mps", inNS(operatorNS), label("app", "nvidia-device-plugin-mps-control-daemon"),
				initContainers(toolkitContainer), status(toolkitContainer, terminated())),
		},
		"allocator outside the operator namespace": {
			stack: ContainerStack,
			pod:   allocator(inNS("other"), status(toolkitContainer, terminated())),
		},
		"allocator rule doesn't apply on the DRA stack": {
			stack: DRAStack,
			pod:   allocator(status(toolkitContainer, terminated())),
		},
		"gate pod still in its gate": {
			stack: ContainerStack,
			pod: gate(ValidatorDaemonSet, status("driver-validation", terminated()),
				status(GateContainer, running()), status(toolkitContainer, waiting()), status("main", waiting())),
		},
		"gate passed": {
			stack: ContainerStack, wantSkip: true,
			pod: gate(ValidatorDaemonSet, status(GateContainer, terminated())),
		},
		"gate killed while holding": {
			stack: ContainerStack,
			pod: gate(ValidatorDaemonSet, terminating(), status("driver-validation", terminated()),
				status(GateContainer, killed()), status(toolkitContainer, waiting()), status("main", waiting())),
		},
		"DRA kubelet plugin gate killed while holding": {
			stack: DRAStack,
			pod:   gate(DRAKubeletPluginDaemonSet, terminating(), status(GateContainer, killed())),
		},
		"container after the gate running": {
			stack: ContainerStack, wantSkip: true,
			pod: gate(ValidatorDaemonSet, status(GateContainer, waiting()), status("main", running())),
		},
		"container after the gate restarted": {
			stack: ContainerStack, wantSkip: true,
			pod: gate(ValidatorDaemonSet, status(GateContainer, waiting()), statusWithLast(toolkitContainer, waiting(), terminated())),
		},
		"DRA kubelet plugin gate passed": {
			stack: DRAStack, wantSkip: true,
			pod: gate(DRAKubeletPluginDaemonSet, status(GateContainer, terminated())),
		},
		"gate pod known by its owner, not its app label": {
			stack: DRAStack,
			pod: newPod("dra-validator", inNS(operatorNS), label("app", "nvidia-operator-validator"),
				ownedBy("nvidia-operator-validator-dra"), initContainers(GateContainer), status(GateContainer, terminated())),
		},
		"gate pod without a gpu-config container that ran": {
			stack: ContainerStack, wantSkip: true,
			pod: newPod("old-gate", inNS(operatorNS), ownedBy(ValidatorDaemonSet), initContainers(toolkitContainer),
				status(toolkitContainer, terminated()), status("main", running())),
		},
		"gate pod without a gpu-config container, nothing ran": {
			stack: ContainerStack,
			pod: newPod("old-gate", inNS(operatorNS), ownedBy(ValidatorDaemonSet), initContainers(toolkitContainer),
				status(toolkitContainer, waiting()), status("main", waiting())),
		},
		"gate DaemonSet name in another namespace": {
			stack: ContainerStack,
			pod:   gate(ValidatorDaemonSet, inNS("other"), status(GateContainer, terminated())),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, tc.stack)
			e.writeState(reloadState)
			e.pods([]corev1.Pod{tc.pod})
			res, err := e.decide()
			require.NoError(t, err)
			if tc.wantSkip {
				require.Equal(t, Skipped, res.Outcome)
				return
			}
			require.Equal(t, Applied, res.Outcome, res.Reason)
		})
	}
}

func TestPodRulesNeedAGatePassThisBoot(t *testing.T) {
	e := newEnv(t, ContainerStack)
	e.pods([]corev1.Pod{newPod("gate", inNS(operatorNS), ownedBy(ValidatorDaemonSet),
		initContainers(GateContainer), status(GateContainer, terminated()))})
	res, err := e.decide()
	require.NoError(t, err)
	require.Equal(t, Applied, res.Outcome)
}

func TestPodCheckFirstLoad(t *testing.T) {
	gpuPod := func(opts ...podOpt) corev1.Pod {
		return newPod("job", append([]podOpt{gpuRequest("nvidia.com/gpu")}, opts...)...)
	}
	cases := map[string]struct {
		reads      [][]corev1.Pod
		wantSkip   bool
		wantSleeps []time.Duration
	}{
		"no pods": {},
		"running GPU pod in both reads": {
			reads:    [][]corev1.Pod{{gpuPod(status("main", running()))}},
			wantSkip: true, wantSleeps: []time.Duration{DefaultPodRecheck},
		},
		"running GPU pod gone at the second read": {
			reads:      [][]corev1.Pod{{gpuPod(status("main", running()))}, {}},
			wantSleeps: []time.Duration{DefaultPodRecheck},
		},
		"running GPU pod idle at the second read": {
			reads:      [][]corev1.Pod{{gpuPod(status("main", running()))}, {gpuPod(status("main", waiting()))}},
			wantSleeps: []time.Duration{DefaultPodRecheck},
		},
		"a pod that starts running only at the second read isn't a candidate": {
			reads:      [][]corev1.Pod{{gpuPod(named("a"), status("main", running()))}, {gpuPod(named("b"), status("main", running()))}},
			wantSleeps: []time.Duration{DefaultPodRecheck},
		},
		"running operator DCGM pod doesn't count at a first load": {
			reads: [][]corev1.Pod{{newPod("dcgm", inNS(operatorNS), label("app", "nvidia-dcgm-dra"), ownedBy("nvidia-dcgm-dra"),
				gpuRequest("nvidia.com/gpu"), status("main", running()))}},
		},
		"idle GPU pod never counts at a first load": {
			reads: [][]corev1.Pod{{gpuPod(status("main", waiting()))}},
		},
		"terminating running GPU pod counts": {
			reads:    [][]corev1.Pod{{gpuPod(terminating(), status("main", running()))}},
			wantSkip: true, wantSleeps: []time.Duration{DefaultPodRecheck},
		},
		"GPU pod running only its sidecar counts": {
			reads:    [][]corev1.Pod{{gpuPod(sidecar("proxy"), status("proxy", running()), status("main", waiting()))}},
			wantSkip: true, wantSleeps: []time.Duration{DefaultPodRecheck},
		},
		"running init container counts": {
			reads:    [][]corev1.Pod{{gpuPod(initContainers("setup"), status("setup", running()))}},
			wantSkip: true, wantSleeps: []time.Duration{DefaultPodRecheck},
		},
		"GPU set only in requests": {
			reads:    [][]corev1.Pod{{newPod("job", gpuRequestOnly("nvidia.com/gpu"), status("main", running()))}},
			wantSkip: true, wantSleeps: []time.Duration{DefaultPodRecheck},
		},
		"running pod without a GPU": {
			reads: [][]corev1.Pod{{newPod("web", status("main", running()))}},
		},
		"MIG request": {
			reads:    [][]corev1.Pod{{newPod("mig", gpuRequest("nvidia.com/mig-1g.10gb"), status("main", running()))}},
			wantSkip: true, wantSleeps: []time.Duration{DefaultPodRecheck},
		},
		"renamed GPU resource": {
			reads:    [][]corev1.Pod{{newPod("job", gpuRequest("nvidia.com/a100"), status("main", running()))}},
			wantSkip: true, wantSleeps: []time.Duration{DefaultPodRecheck},
		},
		"another domain's resource": {
			reads: [][]corev1.Pod{{newPod("job", gpuRequest("example.com/gpu"), status("main", running()))}},
		},
		"idle validation workload pod counts": {
			reads:    [][]corev1.Pod{{newPod("cuda", inNS(operatorNS), label("app", "nvidia-cuda-validator"), status("main", waiting()))}},
			wantSkip: true, wantSleeps: []time.Duration{DefaultPodRecheck},
		},
		"validation label outside the operator namespace": {
			reads: [][]corev1.Pod{{newPod("cuda", label("app", "nvidia-cuda-validator"), status("main", waiting()))}},
		},
		"the applier's own pod is ignored": {
			reads: [][]corev1.Pod{{newPod("applier", inNS(operatorNS), ownedBy(applierDS), gpuRequest("nvidia.com/gpu"), status("main", running()))}},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, ContainerStack)
			e.pods(tc.reads...)
			res, err := e.decide()
			require.NoError(t, err)
			require.Equal(t, tc.wantSleeps, e.sleeps)
			if tc.wantSkip {
				require.Equal(t, Skipped, res.Outcome)
				return
			}
			require.Equal(t, Applied, res.Outcome, res.Reason)
		})
	}
}

func TestPodCheckReload(t *testing.T) {
	draPod := func(opts ...podOpt) corev1.Pod {
		base := []podOpt{inNS(operatorNS), label("app", "nvidia-dcgm-dra"), ownedBy("nvidia-dcgm-dra"),
			claim("dcgm"), status("main", waiting())}
		return newPod("dcgm", append(base, opts...)...)
	}
	gpuClaim := map[string][]string{"gpu-operator/dcgm": {gpuDriver}}
	cases := map[string]struct {
		pod      corev1.Pod
		claims   map[string][]string
		wantSkip bool
	}{
		"idle GPU pod counts at a reload": {
			pod: newPod("job", gpuRequest("nvidia.com/gpu"), status("main", waiting())), wantSkip: true,
		},
		"terminating idle GPU pod counts": {
			pod: newPod("job", gpuRequest("nvidia.com/gpu"), terminating(), status("main", waiting())), wantSkip: true,
		},
		"running GPU pod counts at once": {
			pod: newPod("job", gpuRequest("nvidia.com/gpu"), status("main", running())), wantSkip: true,
		},
		"idle operator DCGM pod isn't a GPU workload": {
			pod: draPod(), claims: gpuClaim,
		},
		"running operator DCGM pod isn't a GPU workload": {
			pod: draPod(func(p *corev1.Pod) { p.Status.ContainerStatuses[0].State = running() }), claims: gpuClaim,
		},
		"operator DCGM exporter pod": {
			pod: newPod("exporter", inNS(operatorNS), label("app", "nvidia-dcgm-exporter-dra"), ownedBy("nvidia-dcgm-exporter-dra"),
				claim("dcgm"), status("main", running())),
			claims: gpuClaim,
		},
		"DRA validator by its name label": {
			pod: newPod("dra-validator", inNS(operatorNS), label("app.kubernetes.io/name", "nvidia-dra-validator"),
				ownedBy("nvidia-dra-validator"), claim("dcgm"), status("main", running())),
			claims: gpuClaim,
		},
		"DCGM-labeled pod outside the operator namespace": {
			pod: draPod(inNS("other")), claims: map[string][]string{"other/dcgm": {gpuDriver}}, wantSkip: true,
		},
		"DCGM-labeled pod of a DaemonSet that isn't the operator's": {
			pod: draPod(ownedBy("my-gpu-job")), claims: gpuClaim, wantSkip: true,
		},
		"DCGM-labeled pod of another operator DRA DaemonSet": {
			pod: draPod(ownedBy("nvidia-dcgm-exporter-dra")), claims: gpuClaim, wantSkip: true,
		},
		"DCGM-labeled pod without a controller": {
			pod: draPod(func(p *corev1.Pod) { p.OwnerReferences = nil }), claims: gpuClaim, wantSkip: true,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, DRAStack)
			e.writeState(reloadState)
			e.cluster.claims = tc.claims
			e.pods([]corev1.Pod{tc.pod})
			res, err := e.decide()
			require.NoError(t, err)
			require.Empty(t, e.sleeps)
			require.Equal(t, 1, e.cluster.podCalls, "the reload variant reads the pod list once")
			if tc.wantSkip {
				require.Equal(t, Skipped, res.Outcome)
				return
			}
			require.Equal(t, Applied, res.Outcome, res.Reason)
		})
	}
}

func TestClaimReferences(t *testing.T) {
	str := func(s string) *string { return &s }
	cases := map[string]struct {
		pod      corev1.Pod
		claims   map[string][]string
		claimErr error
		wantSkip bool
		wantErr  error
	}{
		"claim allocated by the GPU driver": {
			pod: newPod("job", claim("c")), claims: map[string][]string{"default/c": {gpuDriver}}, wantSkip: true,
		},
		"claim allocated by the compute-domain driver": {
			pod: newPod("job", claim("c")), claims: map[string][]string{"default/c": {computeDomainDriver}}, wantSkip: true,
		},
		"claim allocated by another driver": {
			pod: newPod("job", claim("c")), claims: map[string][]string{"default/c": {"example.com"}},
		},
		"claim not allocated yet": {
			pod: newPod("job", claim("c")), claims: map[string][]string{"default/c": nil},
		},
		"claim not found": {
			pod: newPod("job", claim("c")), wantSkip: true,
		},
		"template whose claim isn't generated yet": {
			pod: newPod("job", func(p *corev1.Pod) {
				p.Spec.ResourceClaims = []corev1.PodResourceClaim{{Name: "gpu", ResourceClaimTemplateName: str("t")}}
			}),
			wantSkip: true,
		},
		"template status entry without a claim name": {
			pod: newPod("job", func(p *corev1.Pod) {
				p.Spec.ResourceClaims = []corev1.PodResourceClaim{{Name: "gpu", ResourceClaimTemplateName: str("t")}}
				p.Status.ResourceClaimStatuses = []corev1.PodResourceClaimStatus{{Name: "gpu"}}
			}),
			wantSkip: true,
		},
		"template with its generated claim": {
			pod: newPod("job", func(p *corev1.Pod) {
				p.Spec.ResourceClaims = []corev1.PodResourceClaim{{Name: "gpu", ResourceClaimTemplateName: str("t")}}
				p.Status.ResourceClaimStatuses = []corev1.PodResourceClaimStatus{{Name: "gpu", ResourceClaimName: str("job-gpu-x")}}
			}),
			claims:   map[string][]string{"default/job-gpu-x": {gpuDriver}},
			wantSkip: true,
		},
		"extended resource backed by a claim": {
			pod: newPod("job", func(p *corev1.Pod) {
				p.Status.ExtendedResourceClaimStatus = &corev1.PodExtendedResourceClaimStatus{ResourceClaimName: "ext"}
			}),
			claims:   map[string][]string{"default/ext": {gpuDriver}},
			wantSkip: true,
		},
		"claim read error": {
			pod: newPod("job", claim("c")), claimErr: errBoom, wantErr: ErrAPI,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, DRAStack)
			e.writeState(reloadState)
			e.cluster.claims = tc.claims
			if tc.claimErr != nil {
				e.cluster.claimErr = map[string]error{"default/c": tc.claimErr}
			}
			// Idle, so only the reload variant counts it.
			pod := tc.pod
			pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "main", State: waiting()}}
			e.pods([]corev1.Pod{pod})
			res, err := e.decide()
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				e.requireUnchanged(reloadState)
				return
			}
			require.NoError(t, err)
			if tc.wantSkip {
				require.Equal(t, Skipped, res.Outcome)
				return
			}
			require.Equal(t, Applied, res.Outcome, res.Reason)
		})
	}
}

func TestDecideFlow(t *testing.T) {
	cases := map[string]struct {
		state       string
		setup       func(*env)
		wantOutcome Outcome
		wantErrIs   []error
		// wantFatal: the error wraps neither ErrAPI nor ErrTransient.
		wantFatal  bool
		wantState  string
		wantConfig *string
	}{
		"already decided for this driver load: idle without the lock": {
			state:       `{"version":1,"apply":{"module":110711,"value":"skipped","config":"","time":"2026-01-01T00:00:00Z"}}`,
			wantOutcome: Idle,
		},
		"state file doesn't parse": {
			state:     `{"version":2}`,
			wantErrIs: []error{ErrTransient, statefile.ErrParse},
		},
		"load ID changed before the decision": {
			setup:       func(e *env) { e.loadIDs = []uint64{999} },
			wantOutcome: LoadChanged,
		},
		"module unloaded before the decision": {
			setup:       func(e *env) { e.loadIDs = []uint64{0} },
			wantOutcome: LoadChanged,
		},
		"node read fails": {
			setup:     func(e *env) { e.cluster.nodeErr = errBoom },
			wantErrIs: []error{ErrAPI},
		},
		"pod list fails": {
			setup:     func(e *env) { e.cluster.podErr = errBoom },
			wantErrIs: []error{ErrAPI},
		},
		"driver not ready": {
			setup:       func(e *env) { e.ready = false },
			wantOutcome: Retry,
		},
		"load ID changed once NVML opened a device": {
			setup:       func(e *env) { e.loadIDs = []uint64{testLoadID, 999} },
			wantOutcome: LoadChanged,
		},
		"NVML fails to initialize": {
			setup:     func(e *env) { e.lib.InitErr = errBoom },
			wantFatal: true,
		},
		"NVML device count fails": {
			setup:     func(e *env) { e.lib.DeviceCountErr = errBoom },
			wantFatal: true,
		},
		"NVML device handle fails": {
			setup:     func(e *env) { e.lib.DeviceByIndexErr = errBoom },
			wantFatal: true,
		},
		"NVML fails to initialize as the load ID changes": {
			setup: func(e *env) {
				e.lib.InitErr = errBoom
				e.loadIDs = []uint64{testLoadID, 999}
			},
			wantOutcome: LoadChanged,
		},
		"NVML device count fails as the load ID changes": {
			setup: func(e *env) {
				e.lib.DeviceCountErr = errBoom
				e.loadIDs = []uint64{testLoadID, 999}
			},
			wantOutcome: LoadChanged,
		},
		"NVML device handle fails as the module unloads": {
			setup: func(e *env) {
				e.lib.DeviceByIndexErr = errBoom
				e.loadIDs = []uint64{testLoadID, 0}
			},
			wantOutcome: LoadChanged,
		},
		"no document: applied with an empty hash": {
			wantOutcome: Applied, wantState: "applied", wantConfig: new(""),
		},
		"document: settings written and its hash recorded": {
			setup: func(e *env) {
				e.doc = &config.Source{Name: "gpu-config", Data: map[string]string{config.ConfigKey: docYAML}}
			},
			wantOutcome: Applied, wantState: "applied",
		},
		"no GPU bound to the driver": {
			setup:       func(e *env) { e.lib.Devices = nil },
			wantOutcome: Applied, wantState: "applied",
		},
		"vGPU guest: skipped, no settings": {
			setup: func(e *env) {
				e.lib.Devices[0].Virtualization = apply.VirtualizationVGPU
				e.doc = &config.Source{Name: "gpu-config", Data: map[string]string{config.ConfigKey: docYAML}}
			},
			wantOutcome: Skipped, wantState: "skipped", wantConfig: new(""),
		},
		"mode NVML can't report: skipped, no settings": {
			setup: func(e *env) {
				e.lib.Devices[0].Errs["VirtualizationMode"] = apply.ErrNotSupported
				e.doc = &config.Source{Name: "gpu-config", Data: map[string]string{config.ConfigKey: docYAML}}
			},
			wantOutcome: Skipped, wantState: "skipped", wantConfig: new(""),
		},
		"broken document fails the apply": {
			setup: func(e *env) {
				e.doc = &config.Source{Name: "gpu-config", Data: map[string]string{config.ConfigKey: "version: v1\nbogus: 1"}}
			},
			wantFatal: true, wantErrIs: []error{config.ErrSchema},
		},
		"ConfigMap without config.yaml fails the apply": {
			setup:     func(e *env) { e.doc = &config.Source{Name: "gpu-config", Data: map[string]string{}} },
			wantFatal: true, wantErrIs: []error{config.ErrSchema},
		},
		"range error fails the apply": {
			setup: func(e *env) {
				e.doc = &config.Source{Name: "gpu-config", Data: map[string]string{config.ConfigKey: "version: v1\ndefault:\n  powerLimit: 900\n"}}
			},
			wantFatal: true, wantErrIs: []error{apply.ErrRange},
		},
		"GPU identity read error fails the apply": {
			setup: func(e *env) {
				e.lib.Devices[0].Errs["Name"] = errBoom
				e.doc = &config.Source{Name: "gpu-config", Data: map[string]string{config.ConfigKey: docYAML}}
			},
			wantFatal: true,
		},
		"skip over another applier's pending apply for this driver load keeps its config": {
			state:       `{"version":1,"apply":{"module":110711,"value":"pending","config":"sha256:keep","time":"2026-01-01T00:00:00Z"},"gate":{"module":110711,"time":"2026-01-01T00:00:00Z"}}`,
			wantOutcome: Skipped, wantState: "skipped", wantConfig: new("sha256:keep"),
		},
		"skip at a new driver load drops the old config": {
			state:       `{"version":1,"apply":{"module":100,"value":"applied","config":"sha256:old","time":"2026-01-01T00:00:00Z"},"gate":{"module":0,"time":"2026-01-01T00:00:00Z"}}`,
			wantOutcome: Skipped, wantState: "skipped", wantConfig: new(""),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, ContainerStack)
			if tc.state != "" {
				e.writeState(tc.state)
			}
			if tc.setup != nil {
				tc.setup(e)
			}
			res, err := e.decide()
			for _, want := range tc.wantErrIs {
				require.ErrorIs(t, err, want)
			}
			if tc.wantFatal {
				require.Error(t, err)
				require.NotErrorIs(t, err, ErrAPI)
				require.NotErrorIs(t, err, ErrTransient)
			}
			if tc.wantErrIs == nil && !tc.wantFatal {
				require.NoError(t, err)
			}
			if err != nil {
				require.Equal(t, None, res.Outcome)
				e.requireUnchanged(tc.state)
				require.False(t, e.lib.Initialized, "NVML left open")
				return
			}
			require.Equal(t, tc.wantOutcome, res.Outcome)
			require.False(t, e.lib.Initialized, "NVML left open")
			if tc.wantState == "" {
				e.requireUnchanged(tc.state)
				return
			}
			st := e.state()
			require.True(t, st.ApplyAt(testLoadID))
			require.Equal(t, statefile.Decision(tc.wantState), st.Apply.Value)
			if tc.wantConfig != nil {
				require.Equal(t, *tc.wantConfig, st.Apply.Config)
			}
			if tc.wantState == "applied" && e.doc != nil {
				require.Equal(t, config.Hash(e.doc), st.Apply.Config)
			}
		})
	}
}

func TestUnsupportedModeWritesNoSetting(t *testing.T) {
	cases := map[string]struct {
		setup      func(*fake.Device)
		wantReason []string
	}{
		"vGPU host": {
			setup:      func(d *fake.Device) { d.Virtualization = apply.VirtualizationHostVGPU },
			wantReason: []string{"GPU-1", "vGPU host"},
		},
		"mode read error": {
			setup:      func(d *fake.Device) { d.Errs["VirtualizationMode"] = errBoom },
			wantReason: []string{"GPU-1", "can't be read", errBoom.Error()},
		},
		"mode and UUID read errors": {
			setup: func(d *fake.Device) {
				d.Errs["VirtualizationMode"] = errBoom
				d.Errs["UUID"] = errBoom
			},
			wantReason: []string{"NVML device 1", "can't be read", errBoom.Error()},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, ContainerStack)
			e.lib.Devices = append(e.lib.Devices, e.device("GPU-1"))
			tc.setup(e.lib.Devices[1])
			e.doc = &config.Source{Name: "gpu-config", Data: map[string]string{config.ConfigKey: docYAML}}
			res, err := e.decide()
			require.NoError(t, err)
			require.Equal(t, Skipped, res.Outcome)
			for _, s := range tc.wantReason {
				require.Contains(t, res.Reason, s)
			}
			require.Empty(t, e.log.Writes)
			st := e.state()
			require.True(t, st.ApplyAt(testLoadID))
			require.Equal(t, statefile.Skipped, st.Apply.Value)
		})
	}
}

func TestApplyFailureKeepsSkips(t *testing.T) {
	e := newEnv(t, ContainerStack)
	e.lib.Devices = append(e.lib.Devices, e.device("GPU-1"))
	e.lib.Devices[0].Errs["SetPowerLimit"] = apply.ErrNotSupported
	e.lib.Devices[1].Errs["SetPowerLimit"] = errBoom
	e.doc = &config.Source{Name: "gpu-config", Data: map[string]string{config.ConfigKey: docYAML}}
	res, err := e.decide()
	require.ErrorIs(t, err, apply.ErrNVML)
	require.ErrorContains(t, err, "skipped as not supported: GPU-0 powerLimit")
	require.Equal(t, None, res.Outcome)
	require.Equal(t, []apply.Skip{{UUID: "GPU-0", Setting: apply.PowerLimit}}, res.Skips)
	e.requireUnchanged("")
}

func TestReloadPodCheckWithoutPodListFails(t *testing.T) {
	e := newEnv(t, ContainerStack)
	dec := &decision{Decider: e.d, claimCache: map[string][]string{}, claimNotFound: map[string]bool{}}
	reason, err := dec.podCheck(context.Background(), true, nil)
	require.Error(t, err)
	require.Empty(t, reason)

	reason, err = dec.podCheck(context.Background(), true, []corev1.Pod{})
	require.NoError(t, err)
	require.Empty(t, reason)
}

func TestLockHeldAcrossWaitsAndWrites(t *testing.T) {
	cases := map[string]struct {
		stack          Stack
		setup          func(*env)
		wantLockChecks int
	}{
		"pod re-check wait": {
			stack: ContainerStack,
			setup: func(e *env) {
				e.pods([]corev1.Pod{newPod("job", gpuRequest("nvidia.com/gpu"), status("main", running()))})
			},
			wantLockChecks: 1,
		},
		"slice re-check wait": {
			stack:          DRAStack,
			setup:          func(e *env) { e.cluster.sliceReads = [][]string{{gpuDriver}} },
			wantLockChecks: 1,
		},
		"NVML setting writes": {
			stack: ContainerStack,
			setup: func(e *env) {
				e.lib.Devices = append(e.lib.Devices, e.device("GPU-1"))
				e.doc = &config.Source{Name: "gpu-config", Data: map[string]string{config.ConfigKey: docYAML}}
			},
			wantLockChecks: 2,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, tc.stack)
			tc.setup(e)
			_, err := e.decide()
			require.NoError(t, err)
			require.Equal(t, tc.wantLockChecks, e.lockChecks)

			l, err := e.store.TryLock()
			require.NoError(t, err, "lock released after the decision")
			require.NoError(t, l.Unlock())
		})
	}
}

func TestApplyWritesSettings(t *testing.T) {
	e := newEnv(t, ContainerStack)
	e.doc = &config.Source{Name: "gpu-config", Data: map[string]string{config.ConfigKey: docYAML}}
	_, err := e.decide()
	require.NoError(t, err)
	require.Equal(t, []string{"GPU-0 SetPowerLimit 500000"}, e.log.Writes)
}

func TestScenarioFirstBoot(t *testing.T) {
	e := newEnv(t, ContainerStack)
	e.doc = &config.Source{Name: "gpu-config", Data: map[string]string{config.ConfigKey: docYAML}}
	// A GPU pod from before the reboot can't run without an allocator.
	e.pods([]corev1.Pod{newPod("job", gpuRequest("nvidia.com/gpu"), status("main", waiting()))})

	res, err := e.decide()
	require.NoError(t, err)
	require.Equal(t, Applied, res.Outcome)
	require.Empty(t, e.sleeps)

	l, err := e.store.Lock()
	require.NoError(t, err)
	_, err = l.Read()
	require.NoError(t, err)
	require.NoError(t, l.WriteGatePass(testLoadID))
	require.NoError(t, l.Unlock())

	st := e.state()
	require.True(t, st.AppliedAt(testLoadID))
	require.True(t, st.Passed(testLoadID))
	require.Equal(t, config.Hash(e.doc), st.Apply.Config)
}

func TestScenarioDriverManagerReload(t *testing.T) {
	const newLoadID = 118200
	inService := `{"version":1,"apply":{"module":110711,"value":"applied","config":"sha256:h","time":"2026-01-01T00:00:00Z"},"gate":{"module":110711,"time":"2026-01-01T00:00:05Z"}}`
	recreated := []corev1.Pod{
		newPod("validator", inNS(operatorNS), ownedBy(ValidatorDaemonSet), initContainers("driver-validation", GateContainer, toolkitContainer),
			status("driver-validation", terminated()), status(GateContainer, running()), status(toolkitContainer, waiting()), status("main", waiting())),
		newPod("device-plugin", inNS(operatorNS), label("app", "nvidia-device-plugin-daemonset"), initContainers(toolkitContainer),
			status(toolkitContainer, running()), status("main", waiting())),
	}

	for name, tc := range map[string]struct {
		extra       []corev1.Pod
		wantOutcome Outcome
	}{
		"no GPU pod left: applies":     {wantOutcome: Applied},
		"a GPU pod left behind: skips": {extra: []corev1.Pod{newPod("job", gpuRequest("nvidia.com/gpu"), status("main", waiting()))}, wantOutcome: Skipped},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, ContainerStack)
			e.writeState(inService)
			e.loadIDs = []uint64{newLoadID}
			e.pods(append(append([]corev1.Pod{}, recreated...), tc.extra...))

			res, err := e.d.Decide(t.Context(), newLoadID)
			require.NoError(t, err)
			require.Equal(t, tc.wantOutcome, res.Outcome, res.Reason)
			require.Empty(t, e.sleeps)
			require.Equal(t, 1, e.cluster.podCalls)
			st := e.state()
			require.True(t, st.ApplyAt(newLoadID))
			require.Equal(t, uint64(110711), st.Gate.Module, "the gate object is carried through")
		})
	}
}

func TestScenarioOptInOnNodeInService(t *testing.T) {
	e := newEnv(t, ContainerStack)
	// Opted out at boot: the gate passed as not expected and recorded this driver load.
	e.writeState(`{"version":1,"created":"2026-01-01T00:00:00Z","gate":{"module":110711,"time":"2026-01-01T00:00:00Z"}}`)
	e.doc = &config.Source{Name: "gpu-config", Data: map[string]string{config.ConfigKey: docYAML}}

	res, err := e.decide()
	require.NoError(t, err)
	require.Equal(t, Skipped, res.Outcome)
	require.NotContains(t, e.cluster.order, "pods", "evidence settles it without a pod list")
	st := e.state()
	require.Equal(t, statefile.Skipped, st.Apply.Value)
	require.Empty(t, st.Apply.Config)
	require.Empty(t, e.log.Writes)
}

func TestPodListReadAfterSliceRecheck(t *testing.T) {
	e := newEnv(t, DRAStack)
	e.writeState(reloadState)
	// A stale slice: present at the first list, gone after the wait.
	e.cluster.sliceReads = [][]string{{gpuDriver}, {}}
	e.pods([]corev1.Pod{})
	res, err := e.decide()
	require.NoError(t, err)
	require.Equal(t, Applied, res.Outcome)
	require.Equal(t, []string{"slices", "slices", "pods"}, e.cluster.order)
}
