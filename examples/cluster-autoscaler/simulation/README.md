# Simulating the autoscaler flow on kind (no GPUs)

This validates the full flow from the [parent guide](../README.md) on a
machine without GPUs: nodes that join the cluster already tainted (as a node
pool template would create them), the NPD → condition → NRC → untaint chain,
and a scale-up that adds a fresh node mid-flow. The probe checks a marker
file instead of running nvidia-smi, so you control readiness by hand. The
GPU Operator is not involved: the kind config registers each worker with the
GPU label (simulates NFD and the GPU Operator) and the startup taint
(simulates the node pool template).

Requires `kind`, `docker`, and `jq` on the local machine. Run all commands
from this directory (`examples/cluster-autoscaler/simulation/`).

| File | Purpose |
|---|---|
| `npd-gpu-ready-simulation.yaml` | Variant of `../npd-gpu-ready.yaml` whose probe checks the marker file `/var/lib/gpu-ready-sim/ready` on the node instead of running nvidia-smi |
| `kind-config.yaml` | kind cluster whose workers join with the startup taint and GPU label already applied, like a node pool template |
| `reset.sh` | Re-arms the simulation on a node so the flow can be run again |

The scale-up step also uses `kindscaler.sh` from the Node Readiness
Controller repository; step 8 downloads it rather than vendoring a copy
here.

## Walkthrough

1. Create the cluster. The config registers both workers with
   `nvidia.com/gpu.present=true` and the startup taint, so they are tainted
   from the moment they join:

   ```sh
   kind create cluster --config kind-config.yaml
   kubectl get nodes -o custom-columns='NAME:.metadata.name,TAINTS:.spec.taints[*].key'
   ```

   Expected: both workers list `readiness.k8s.io/nvidia-gpu-not-ready`.

2. Install NRC
   ([step 1 of the parent guide's Prerequisites](../README.md#1-install-the-node-readiness-controller)).

3. Install the simulation NPD and verify the condition appears as `False`
   on the workers:

   ```sh
   kubectl apply -f npd-gpu-ready-simulation.yaml
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
   kubectl apply -f ../node-readiness-rule.yaml
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

   # kindscaler.sh adds a worker to a running kind cluster. Download it from
   # the Node Readiness Controller repository (pinned to the version used here):
   curl -fsSL -o kindscaler.sh \
     https://raw.githubusercontent.com/kubernetes-sigs/node-readiness-controller/v0.3.0/hack/test-workloads/kindscaler.sh
   chmod +x kindscaler.sh
   ./kindscaler.sh gpu-sim 1
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
    ./reset.sh gpu-sim-worker
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

## Troubleshooting and cleanup

The [parent guide's Troubleshooting section](../README.md#troubleshooting)
applies here too — in particular the entries on a missing condition, on
multiple condition writers, and on NRC not removing the taint.

`kind delete cluster --name gpu-sim` removes everything, including nodes
added by the scaler (they carry the kind cluster label).
