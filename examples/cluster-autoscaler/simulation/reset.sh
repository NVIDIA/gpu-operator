#!/bin/sh
# Reset the simulation so the flow can be run again on the same node.
# Usage: ./reset.sh <node-name>
set -e

NODE="${1:?usage: $0 <node-name>}"

# Remove the marker file. NPD flips nvidia.com/GPUReady back to False on
# its next probe interval (10s).
docker exec "$NODE" rm -f /var/lib/gpu-ready-sim/ready

# Wait for the condition to turn False before re-tainting. Re-tainting while
# it is still True triggers an NRC reconcile that can remove the new taint
# and re-write the bootstrap annotation, undoing the reset.
echo "Waiting for nvidia.com/GPUReady to turn False..."
i=0
while :; do
  status="$(kubectl get node "$NODE" -o jsonpath='{.status.conditions[?(@.type=="nvidia.com/GPUReady")].status}')"
  [ "$status" = "False" ] && break
  i=$((i + 1))
  if [ "$i" -ge 30 ]; then
    echo "condition did not turn False after 60s; is the NPD pod running on $NODE?" >&2
    exit 1
  fi
  sleep 2
done

# Re-apply the startup taint. In production the node pool template applies
# it when a node is created.
kubectl taint node "$NODE" readiness.k8s.io/nvidia-gpu-not-ready=pending:NoSchedule --overwrite

# bootstrap-only mode acts once per node: after removing the taint, NRC
# records this annotation and ignores the node afterwards. Remove it so NRC
# manages the node again.
kubectl annotate node "$NODE" readiness.k8s.io/bootstrap-completed-nvidia-gpu-readiness-

echo "Reset complete. The taint stays until the marker file is recreated."
