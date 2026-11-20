# Driver rootfs mount regression

Run this test after installing the operator on a disposable Linux Kubernetes
test node. It requires `kubectl`, `jq`, `cmp`, permission to create privileged
Pods and namespaces, and a runtime that supports mount propagation.

```sh
TARGET_NODE=my-test-node OPERATOR_NAMESPACE=gpu-operator \
  bash tests/scripts/verify-driver-rootfs-mount.sh
```

The test reads the installed toolkit DaemonSet and exercises both its
`driver-validation` and toolkit container driver-root mount definitions in
temporary Pods on the selected node. It publishes rootfs generation A, verifies
that both running consumers can read A, then replaces that mount with generation
B. Both generations report the same synthetic driver version, so stale roots
cannot pass simply because their versions agree. Candidate mode requires the
same consumer containers to read B and remain Ready without restarts.

To confirm the original direct-leaf behavior on an unmodified installation:

```sh
TARGET_NODE=my-test-node OPERATOR_NAMESPACE=gpu-operator \
  bash tests/scripts/verify-driver-rootfs-mount.sh --mode=baseline
```

Baseline mode succeeds when the original containers retain A after the producer
publishes B. It is expected to fail against the corrected parent-mount contract.

The test uses only a unique `/var/tmp/gpu-operator-rootfs-*` directory on the
selected node and removes its mounts, files, and namespace on exit. It does not
change node labels, installed operator resources, or `/run/nvidia/driver`.
`ARTIFACT_DIR` controls the local artifact parent directory; each run preserves
its DaemonSet contract, node/runtime versions, Pod/container identities, mount
tables, and observed generations in a separate directory. `TEST_IMAGE` overrides
the BusyBox image, and `TIMEOUT` overrides the readiness and cleanup timeout.

This tests the actual container runtime's mount propagation with the installed
mount contract. It does not load NVML, exercise driver-manager eviction or DRA
claims, or replace GPU workload and operand-readiness end-to-end tests.
