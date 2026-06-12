# Cluster Autoscaler integration with the GPU Operator

This guide shows how to keep workloads off autoscaled GPU nodes until the GPU
stack is actually ready, using a startup taint that is removed when the node
passes a GPU readiness probe.

When the Cluster Autoscaler adds a GPU node, the node reports `Ready` long
before the GPU Operator has finished installing the driver, container toolkit,
and device plugin. Workloads scheduled during that window fail, or occupy the
node so the autoscaler considers the scale-up satisfied. Scale-from-zero with
MIG has the same problem: pods request MIG resources the node cannot
advertise yet.

The integration adds three components; the GPU Operator itself is unchanged:

| Component | Role |
|---|---|
| Node pool template (cloud provider) | Applies the startup taint to every new GPU node |
| [Node Problem Detector (NPD)](https://github.com/kubernetes/node-problem-detector) | Runs a GPU readiness probe on each GPU node and publishes the `nvidia.com/GPUReady` node condition |
| [Node Readiness Controller (NRC)](https://github.com/kubernetes-sigs/node-readiness-controller) | Removes the startup taint once the condition is `True` |
| GPU Operator | Unchanged; its operands tolerate the startup taint via existing toleration settings |

The flow on a freshly provisioned node:

```
node pool template applies the startup taint
        |
        v
new GPU node joins: NoSchedule for regular pods
        |                                          cluster-autoscaler is informed the
        |                                          taint is temporary via
        |                                          --startup-taint=<key>
        v
GPU Operator operands roll out (they tolerate the taint)
        |
        v
NPD probe succeeds (nvidia-smi works)
        |
        v
node condition nvidia.com/GPUReady = True
        |
        v
NRC removes the startup taint
        |
        v
pending GPU pods schedule
```

## Names used in this example

| Object | Value |
|---|---|
| Node condition | `nvidia.com/GPUReady` |
| Startup taint | `readiness.k8s.io/nvidia-gpu-not-ready=pending:NoSchedule` |
| NodeReadinessRule | `nvidia-gpu-readiness` |
| NPD monitor source | `gpu-ready-monitor` |
| NPD ConfigMap / DaemonSet | `npd-gpu-ready-config` / `node-problem-detector` (namespace `kube-system`) |
| NRC bootstrap annotation | `readiness.k8s.io/bootstrap-completed-nvidia-gpu-readiness` (written by NRC after it removes the taint) |
| Simulation marker file | `/var/lib/gpu-ready-sim/ready` (on the node) |

Two naming constraints to be aware of if you change these:

- NRC requires the taint key to use the `readiness.k8s.io/` prefix; the
  `NodeReadinessRule` CRD rejects other prefixes.
- Because of that prefix, you cannot use the Cluster Autoscaler's
  auto-detected startup-taint prefix
  (`startup-taint.cluster-autoscaler.kubernetes.io/`). Passing the explicit
  `--startup-taint` flag to the autoscaler is therefore required, not
  optional. A feature request to allow the autoscaler's startup-taint prefix
  in NRC rules is open:
  [node-readiness-controller#279](https://github.com/kubernetes-sigs/node-readiness-controller/issues/279).

The same taint key appears in five places and must match exactly: the node
pool template, the `--startup-taint` flag, the `NodeReadinessRule`, the
GPU Operator toleration values, and the NPD DaemonSet tolerations in
`npd-gpu-ready.yaml`.

## Files in this directory

| File | Purpose |
|---|---|
| `npd-gpu-ready.yaml` | NPD DaemonSet + RBAC + ConfigMap with the nvidia-smi readiness probe |
| `node-readiness-rule.yaml` | NRC rule that removes the startup taint when the condition is `True` |
| `simulation/npd-gpu-ready-simulation.yaml` | NPD variant whose probe checks a marker file instead of nvidia-smi, for clusters without GPUs |
| `simulation/kind-config.yaml` | kind cluster whose workers join with the startup taint and GPU label already applied, like a node pool template |
| `simulation/kindscaler.sh` | Adds workers to the running kind cluster to simulate a scale-up (vendored from the NRC repo) |
| `simulation/reset.sh` | Re-arms the simulation on a node so the flow can be run again |

All `kubectl apply -f <file>` commands in this guide are run from this
directory (`examples/cluster-autoscaler/`) of a repository clone.

## Prerequisites

These steps target a real GPU cluster and are referenced from Walkthrough B.
For the no-GPU simulation, only step 1 (NRC) is needed — Walkthrough A
applies its own NPD variant and the readiness rule inline.

### 1. Install the Node Readiness Controller

NRC is an alpha component ([KEP-5233](https://github.com/kubernetes/enhancements/issues/5233)).
This example was validated with v0.3.0.

```sh
VERSION=v0.3.0
kubectl apply -f https://github.com/kubernetes-sigs/node-readiness-controller/releases/download/${VERSION}/crds.yaml
kubectl wait --for condition=established --timeout=30s crd/nodereadinessrules.readiness.node.x-k8s.io
kubectl apply -f https://github.com/kubernetes-sigs/node-readiness-controller/releases/download/${VERSION}/install.yaml
kubectl -n nrr-system rollout status deploy/nrr-controller-manager --timeout=120s
```

This deploys the controller into the `nrr-system` namespace. See the
[NRC installation guide](https://node-readiness-controller.sigs.k8s.io/user-guide/installation.html)
for the full-install variant (metrics, validation webhook).

### 2. Install NPD with the GPU readiness plugin

```sh
kubectl apply -f npd-gpu-ready.yaml
```

This deploys NPD to nodes labeled `nvidia.com/gpu.present=true` — the label
the GPU Operator applies to nodes that Node Feature Discovery (NFD, deployed
as a GPU Operator subchart) has identified as having an NVIDIA GPU — with a
single custom-plugin monitor. The probe
runs `nvidia-smi` every 10 seconds — through the driver-container root
(`/run/nvidia/driver`) or the host root — and publishes the
result as the `nvidia.com/GPUReady` node condition. Both the monitor
configuration and the probe script live in the `npd-gpu-ready-config`
ConfigMap.

If your cluster already runs NPD (some managed Kubernetes offerings deploy
it), do not install a second copy. Add the `gpu-ready-monitor.json` and
`check-gpu-ready.sh` keys from the ConfigMap to your existing NPD
configuration and pass an additional
`--config.custom-plugin-monitor=/config/gpu-ready-monitor.json` flag.

NPD reads its configuration at startup, and ConfigMap updates do not restart
running pods. After changing the config, run
`kubectl -n kube-system rollout restart daemonset/node-problem-detector`
(substitute your NPD DaemonSet's name).

### 3. Configure GPU Operator tolerations

The GPU Operator's operands must run while the startup taint is still on the
node — they are what makes the node GPU ready. Two separate values control this,
and both replace their defaults rather than appending, so keep the existing
entries. Save the following as `values-autoscaler.yaml` and apply it with
`helm upgrade --install gpu-operator nvidia/gpu-operator -n gpu-operator
--create-namespace -f values-autoscaler.yaml`:

```yaml
daemonsets:
  tolerations:
    # Default entry -- this list replaces the default, so keep it.
    - key: nvidia.com/gpu
      operator: Exists
      effect: NoSchedule
    - key: readiness.k8s.io/nvidia-gpu-not-ready
      operator: Exists
      effect: NoSchedule

# The NFD subchart is not covered by daemonsets.tolerations. NFD must run on
# new nodes while they are still tainted so the GPU Operator can label them
# (nvidia.com/gpu.present=true).
node-feature-discovery:
  worker:
    tolerations:
      # First two entries are the chart defaults -- keep them.
      - key: node-role.kubernetes.io/control-plane
        operator: Equal
        value: ""
        effect: NoSchedule
      - key: nvidia.com/gpu
        operator: Exists
        effect: NoSchedule
      - key: readiness.k8s.io/nvidia-gpu-not-ready
        operator: Exists
        effect: NoSchedule
```

Regular GPU workloads must NOT tolerate the startup taint — the taint is what
keeps them off the node until it is ready.

### 4. Apply the readiness rule

On a cluster that already has GPU nodes, preview the rule's effect first.
NRC adds the taint to any matching node whose condition is not `True` — in
both enforcement modes — so applying the rule before NPD reports readiness
on every existing node makes those nodes unschedulable for new pods. Set
`dryRun: true` in the rule spec; the controller then reports intended taint
changes in the rule's `status.dryRunResults` without modifying nodes:

```sh
kubectl apply -f node-readiness-rule.yaml
kubectl get nodereadinessrule nvidia-gpu-readiness -o jsonpath='{.status.dryRunResults}'
```

Once the dry run shows no unexpected taint additions, remove `dryRun: true`
and re-apply.

## Walkthrough A: simulation without GPUs (kind)

This validates the full flow on a machine without GPUs: nodes that join the
cluster already tainted (as a node pool template would create them), the
NPD → condition → NRC → untaint chain, and a scale-up that adds a fresh node
The probe checks a marker file instead of running nvidia-smi, so
you control readiness by hand. The GPU Operator is not involved: the kind
config registers each worker with the GPU label (simulates NFD and the
GPU Operator) and the startup taint (simulates the node pool
template). Requires `kind`, `docker`, and `jq` on the local machine.

1. Create the cluster. The config registers both workers with
   `nvidia.com/gpu.present=true` and the startup taint, so they are tainted
   from the moment they join:

   ```sh
   kind create cluster --config simulation/kind-config.yaml
   kubectl get nodes -o custom-columns='NAME:.metadata.name,TAINTS:.spec.taints[*].key'
   ```

   Expected: both workers list `readiness.k8s.io/nvidia-gpu-not-ready`.

2. Install NRC (step 1 of Prerequisites above).

3. Install the simulation NPD and verify the condition appears as `False`
   on the workers:

   ```sh
   kubectl apply -f simulation/npd-gpu-ready-simulation.yaml
   kubectl get node gpu-sim-worker -o jsonpath='{.status.conditions[?(@.type=="nvidia.com/GPUReady")]}' | jq
   ```

   Expected within ~15 seconds:

   ```
   {
     "type": "nvidia.com/GPUReady",
     "status": "False",
     "reason": "GPUReadinessPending",
     ...
   }
   ```

4. Apply the readiness rule:

   ```sh
   kubectl apply -f node-readiness-rule.yaml
   ```

   NRC adopts the existing taints; they stay in place because the
   condition is `False`.

5. Create a pod that needs a GPU node and confirm it stays `Pending`:

   ```sh
   cat <<EOF | kubectl apply -f -
   apiVersion: v1
   kind: Pod
   metadata:
     name: gpu-workload-sim
   spec:
     nodeSelector:
       nvidia.com/gpu.present: "true"
     containers:
       - name: app
         image: registry.k8s.io/pause:3.9
   EOF
   kubectl get pod gpu-workload-sim   # STATUS: Pending
   ```

6. Mark the simulated GPUs ready by creating the marker file on both
   workers (kind nodes are docker containers):

   ```sh
   for node in gpu-sim-worker gpu-sim-worker2; do
     docker exec "$node" mkdir -p /var/lib/gpu-ready-sim
     docker exec "$node" touch /var/lib/gpu-ready-sim/ready
   done
   ```

7. Watch the chain complete. Within ~10s the conditions flip to `True`
   (reason `GPUReady`), NRC removes the taints and records the bootstrap
   annotation, and the pod schedules:

   ```sh
   kubectl get node gpu-sim-worker -o jsonpath='{.status.conditions[?(@.type=="nvidia.com/GPUReady")]}' | jq
   kubectl get nodes -o custom-columns='NAME:.metadata.name,TAINTS:.spec.taints[*].key'   # startup taints gone
   kubectl get node gpu-sim-worker -o jsonpath='{.metadata.annotations.readiness\.k8s\.io/bootstrap-completed-nvidia-gpu-readiness}'
   kubectl get pod gpu-workload-sim                                    # STATUS: Running
   ```

8. Simulate a scale-up. In production the sequence is: a pod goes
   `Pending`, the autoscaler creates a node from the pool template, the
   node joins tainted, and the gate holds the pod off until the node is
   ready. Reproduce it manually — cordon the existing workers (the state
   that makes the autoscaler scale up), create a second pending pod,
   then add a node with the scaler script (plays the role of the cloud provider):

   ```sh
   kubectl cordon gpu-sim-worker gpu-sim-worker2

   cat <<EOF | kubectl apply -f -
   apiVersion: v1
   kind: Pod
   metadata:
     name: gpu-workload-sim-2
   spec:
     nodeSelector:
       nvidia.com/gpu.present: "true"
     containers:
       - name: app
         image: registry.k8s.io/pause:3.9
   EOF
   kubectl get pod gpu-workload-sim-2   # STATUS: Pending

   simulation/kindscaler.sh gpu-sim 1
   ```

   The new node joins as `gpu-sim-worker3`, already tainted and labeled —
   the scaler clones worker2's join configuration. Wait for the NPD pod on
   it to reach `Running`, and verify the node is gated:

   ```sh
   kubectl get pods -n kube-system -l app=node-problem-detector -o wide
   kubectl get node gpu-sim-worker3 -o jsonpath='{.spec.taints}'
   kubectl get pod gpu-workload-sim-2   # still Pending
   ```

9. Mark the new node ready and watch the pod schedule on it:

   ```sh
   docker exec gpu-sim-worker3 mkdir -p /var/lib/gpu-ready-sim
   docker exec gpu-sim-worker3 touch /var/lib/gpu-ready-sim/ready
   kubectl get pod gpu-workload-sim-2 -o wide -w   # Running on gpu-sim-worker3
   ```

   Uncordon the other workers afterwards:

   ```sh
   kubectl uncordon gpu-sim-worker gpu-sim-worker2
   ```

10. To repeat: re-run step 8 to add more nodes (`worker4`, ...), or re-run
    the bootstrap flow on an existing node:

    ```sh
    kubectl delete pod gpu-workload-sim
    ./simulation/reset.sh gpu-sim-worker
    ```

    The reset script removes the marker file, waits for the condition to
    turn `False`, re-applies the taint, and removes the bootstrap
    annotation (in `bootstrap-only` mode NRC acts once per node; the
    annotation records that the node completed bootstrap, and NRC ignores
    annotated nodes). Remove a scaled-up node with
    `kubectl delete node gpu-sim-worker3 && docker rm -f gpu-sim-worker3`.
    The scaler copies the cluster's kubeadm join token, which expires about
    24 hours after cluster creation; if joining fails on an older cluster,
    recreate the cluster.

## Walkthrough B: real cluster with Cluster Autoscaler

The steps are ordered so the untaint machinery is in place before any node
arrives tainted; a node created with the startup taint while NRC or NPD is
missing keeps the taint indefinitely.

1. Install the GPU Operator with the toleration values from Prerequisites
   step 3.

2. Install NRC, NPD, and the readiness rule (Prerequisites steps 1, 2, 4 —
   including the dry-run check in step 4 if GPU nodes already exist).

3. Pass the startup-taint flag to the Cluster Autoscaler deployment:

   ```
   --startup-taint=readiness.k8s.io/nvidia-gpu-not-ready
   ```

   Without this flag the autoscaler treats the taint as permanent: it
   considers pending GPU pods unschedulable on the new node and may scale up
   again, and nodes waiting for readiness look like scale-down candidates.
   The flag cannot be avoided by using the autoscaler's auto-detected
   `startup-taint.cluster-autoscaler.kubernetes.io/` prefix, because NRC
   requires the `readiness.k8s.io/` prefix.

   This step requires an autoscaler deployment whose flags you control (for
   example, the self-managed deployment typical on EKS). On managed
   offerings such as AKS and GKE the autoscaler's flags are not
   user-editable — check whether the provider exposes startup-taint
   configuration before adopting this pattern, or use Karpenter's native
   `startupTaints` (step 4). On those platforms the only startup-taint
   mechanism is the autoscaler's reserved key prefix, which NRC currently
   rejects;
   [node-readiness-controller#279](https://github.com/kubernetes-sigs/node-readiness-controller/issues/279)
   tracks allowing it. Confirm the flag is active:

   ```sh
   kubectl -n kube-system get deploy cluster-autoscaler \
     -o jsonpath='{.spec.template.spec.containers[0].args}'
   ```

4. Add the startup taint to the GPU node pool template so every new node
   starts tainted. Examples:

   ```sh
   # AKS (see the managed-autoscaler caveat in step 3)
   az aks nodepool update ... --node-taints "readiness.k8s.io/nvidia-gpu-not-ready=pending:NoSchedule"

   # GKE (see the managed-autoscaler caveat in step 3)
   gcloud container node-pools create ... --node-taints "readiness.k8s.io/nvidia-gpu-not-ready=pending:NoSchedule"

   # EKS managed node group: set the taint in the node group config.
   # For self-managed node group ASGs, also tag the ASG so the autoscaler
   # knows the template taint when the group is at zero nodes:
   #   key:   k8s.io/cluster-autoscaler/node-template/taint/readiness.k8s.io/nvidia-gpu-not-ready
   #   value: pending:NoSchedule

   # Karpenter: add the taint under the NodePool's spec.template.spec.startupTaints
   # (Karpenter has native startup-taint handling; the --startup-taint flag
   # in step 3 applies to the Cluster Autoscaler, not Karpenter).
   ```

   Multiple GPU node pools can share this taint key — one readiness rule and
   one `--startup-taint` entry cover them all. Distinct keys per pool work
   too; the flag is repeatable.

5. Validate with a scale-up from zero:

   ```sh
   # Create a GPU pod while the GPU node pool is at zero.
   cat <<EOF | kubectl apply -f -
   apiVersion: v1
   kind: Pod
   metadata:
     name: gpu-test
   spec:
     restartPolicy: Never
     containers:
       - name: gpu-test
         image: nvcr.io/nvidia/cuda:12.4.1-base-ubuntu22.04
         command: ["nvidia-smi"]
         resources:
           limits:
             nvidia.com/gpu: 1
   EOF

   # Observe the sequence:
   kubectl get pod gpu-test -w                  # Pending until the node is ready
   kubectl get nodes -w                         # new node joins, tainted
   kubectl get pods -n gpu-operator -o wide -w  # operands roll out on the new node
   kubectl get node <new-node> -o jsonpath='{.status.conditions[?(@.type=="nvidia.com/GPUReady")]}'
   kubectl get node <new-node> -o jsonpath='{.spec.taints}'   # taint removed once True
   ```

   The pod must stay `Pending` until the condition turns `True` and the taint
   is removed, then run `nvidia-smi` successfully.

   This is a basic test: it requests `nvidia.com/gpu: 1` and does not cover
   MIG. See the next section if your node pools use MIG.

### MIG node pools

The shipped probe gates on driver readiness only: `nvidia-smi` succeeds once
the driver is up, which is before MIG partitioning completes and before the
device plugin advertises MIG resources. On a MIG pool the taint therefore
comes off earlier than full readiness. Pods requesting MIG resources (for
example `nvidia.com/mig-3g.20gb`) still wait — the scheduler does not place
them until the resources are allocatable — but that window is not gated by
the taint, and the autoscaler may treat the node as ready before it can
serve MIG pods.

If you use MIG:

- Set the MIG profile in the node pool template (the `nvidia.com/mig.config`
  node label), so partitioning starts as soon as the node joins.
- Do not gate MIG workloads with required node affinity on labels that
  appear only after bootstrap, such as `nvidia.com/mig.config.state` or
  `nvidia.com/mig.strategy`. That affinity is a common workaround for
  pods scheduling before MIG configuration completes, and it is the usual
  cause of the MIG scale-from-zero deadlock: at zero nodes the autoscaler
  evaluates the pod against the pool template, the template never contains
  those labels, so the pool never scales up. The startup taint replaces
  that affinity as the readiness gate. Select the pool through labels that
  exist in the template — the pool name label (for example `agentpool` on
  AKS) or a custom label set on the pool.
- For scale-from-zero, the autoscaler must also learn from the template
  which GPU resources a new node will advertise:
  - With `mig.strategy=single`, MIG devices are advertised as
    `nvidia.com/gpu`, which autoscalers can generally infer from the
    instance type.
  - With `mig.strategy=mixed`, pods request profile-specific resources
    like `nvidia.com/mig-3g.20gb`, which exist only after the device
    plugin reports them — no provider infers them from the instance type.
    Declare them on the pool: on EKS / self-managed node group ASGs, tag
    the ASG with
    `k8s.io/cluster-autoscaler/node-template/resources/nvidia.com/mig-3g.20gb`
    = `2`; on Azure VMSS the same tags are supported with `_` in place of
    `/` (Azure tag names cannot contain slashes); on GKE, set the
    accelerator and `gpu-partition-size` on the node pool.
- To gate the taint on MIG readiness as well, extend `check-gpu-ready.sh`:
  for example, require `nvidia-smi -L` to list MIG devices, or query the
  node's `nvidia.com/mig.config.state` label (requires API access from the
  probe) and exit ready only on `success`.

## Day-2: bootstrap-only vs continuous enforcement

The rule in this example uses `enforcementMode: bootstrap-only`: after NRC
removes the taint from a node, it records the bootstrap annotation and stops
managing that node. A driver upgrade or MIG reconfiguration later flips
`nvidia.com/GPUReady` to `False` (NPD keeps probing), but the node stays
schedulable.

One caveat: the bootstrap annotation is written only when NRC removes a
taint. A node that matched the rule while already untainted and ready never
gets the annotation, so NRC taints it the first time its condition turns
`False` — even in `bootstrap-only` mode. The dry-run check in Prerequisites
step 4 shows which nodes are in this state.

Setting `enforcementMode: continuous` makes NRC re-apply the taint whenever
the condition turns `False`, which extends the same mechanism to day-2
gating. With `continuous`, a routine driver upgrade makes every node briefly
unschedulable, and new pods do not schedule during MIG reconfiguration. For
the autoscaler use case, `bootstrap-only` is the recommended starting point.

## Troubleshooting

**NPD pod crash-loops with `panic: No configuration option for any problem
daemon is specified`.** NPD refuses to start without at least one monitor.
Check that the `--config.custom-plugin-monitor` flag is present and points at
the mounted JSON file.

**The `nvidia.com/GPUReady` condition is absent from the node.** The NPD pod
is probably not running on that node:
`kubectl get pods -n kube-system -l app=node-problem-detector -o wide`. The
shipped DaemonSet tolerates only the startup taint and `nvidia.com/gpu`; if
the GPU node pool carries additional taints, add matching tolerations to the
NPD DaemonSet (and to the GPU Operator operands and NFD).

**The condition keeps being set to `False` (or flips back) unexpectedly.**
More than one writer may be publishing it — typically a second NPD DaemonSet
under a different name left over from earlier experiments. Find DaemonSets
with `kubectl get ds -A | grep -i -e problem -e npd`, and identify which
client owns the condition through managed fields:

```sh
kubectl get node <node> --show-managed-fields -o yaml | grep -B3 'GPUReady'
```

Each `managedFields` entry names the writing client in its `manager` field.

**NRC does not remove the taint.** Check, in order:

1. The condition is actually `True`:
   `kubectl get node <node> -o jsonpath='{.status.conditions[?(@.type=="nvidia.com/GPUReady")]}'`
2. The rule's `nodeSelector` matches the node's labels
   (`nvidia.com/gpu.present=true` in this example — on a real cluster the
   GPU Operator applies that label from NFD's feature labels, so both the
   operator and NFD must be running, and NFD must tolerate the startup
   taint; see Prerequisites step 3).
3. The node does not already have the
   `readiness.k8s.io/bootstrap-completed-nvidia-gpu-readiness` annotation —
   in `bootstrap-only` mode NRC ignores nodes that completed bootstrap once.
   Remove the annotation to make NRC act again.
4. The NRC controller logs: `kubectl logs -n nrr-system deploy/nrr-controller-manager`.

**The NodeReadinessRule is rejected on apply.** The taint key must use the
`readiness.k8s.io/` prefix; the CRD validates this.

**The nvidia-smi probe never succeeds on a real GPU node.** Find the NPD pod
on the affected node
(`kubectl get pods -n kube-system -l app=node-problem-detector -o wide`) and
run the probe by hand:

```sh
kubectl exec -n kube-system <npd-pod> -- /config/check-gpu-ready.sh; echo "exit=$?"
```

Exit 1 means ready, exit 0 means not ready (NPD's plugin contract is built
for problem detection, so the codes are inverted compared to a typical health
check). If it stays at 0, check that the driver install finished
(`/run/nvidia/driver` populated on the host for driver-container installs)
and that the DaemonSet runs privileged with `/` mounted at `/host` with
`mountPropagation: HostToContainer` — without propagation, the bind mount
the driver container creates at `/run/nvidia/driver` is invisible to an NPD
pod that started before the driver installed (restarting the NPD pod hides
the problem, so it looks intermittent).

A node whose probe never succeeds — failed hardware, for example — stays
tainted and `Ready` indefinitely. This pattern does not deprovision such
nodes; that takes admin intervention or node pool health checks.

**Workloads schedule onto the node before the GPU is ready.** The workload
tolerates the startup taint. Only infrastructure that participates in making
the node ready (GPU Operator operands, NFD, NPD) should tolerate it.

## Cleanup

Remove the pieces in this order:

1. Remove the startup taint from the node pool template, and the
   `--startup-taint` flag if you set it. Skipping this leaves every new node
   tainted with nothing in place to untaint it.
2. Delete the rule while NRC is still installed:
   `kubectl delete -f node-readiness-rule.yaml`. NRC's cleanup finalizer
   removes the rule's taint from any node still carrying it; if NRC is
   uninstalled first, the deletion hangs on the finalizer and tainted nodes
   stay tainted.
3. Uninstall NRC and delete NPD: `kubectl delete -f npd-gpu-ready.yaml`
   (or `simulation/npd-gpu-ready-simulation.yaml`).
4. Optionally remove the readiness toleration entries from the GPU Operator
   values.

Stale `nvidia.com/GPUReady` conditions remain on nodes until the Node object
is deleted or another writer overwrites them; they are inert without NRC.
For the kind simulation, `kind delete cluster --name gpu-sim` removes
everything.
