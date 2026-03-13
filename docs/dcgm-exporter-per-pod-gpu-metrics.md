# Per-Pod GPU Utilization with DCGM Exporter (Time-Slicing)

## Overview

When GPU time-slicing is enabled via `ClusterPolicy`, multiple pods share a
single physical GPU device. Standard DCGM metrics report aggregate utilization
for the whole device — `dcgm_fi_dev_gpu_util` cannot distinguish how much of
the GPU proxy, embeddings, or inference pods are each using.

GPU Operator v24.x+ integrates with dcgm-exporter's per-pod GPU utilization
feature to restore workload-level attribution without requiring MIG.

## Prerequisite: dcgm-exporter v3.4.0+

This feature requires dcgm-exporter v3.4.0 or later, which adds the
`--enable-per-pod-gpu-util` flag and `dcgm_fi_dev_sm_util_per_pod` metric.

See: [NVIDIA/dcgm-exporter#587](https://github.com/NVIDIA/dcgm-exporter/issues/587)

## Enabling Time-Slicing + Per-Pod Metrics

A complete `ClusterPolicy` for a T4 cluster running three shared workloads:

```yaml
apiVersion: nvidia.com/v1
kind: ClusterPolicy
metadata:
  name: gpu-cluster-policy
spec:
  # 1. Configure time-slicing: 3 virtual slices per physical GPU
  devicePlugin:
    config:
      name: time-slicing-config
      default: any

  # 2. Enable per-pod GPU utilization metrics in dcgm-exporter
  dcgmExporter:
    perPodGPUUtil:
      enabled: true
      # Optional: custom path (default: /var/lib/kubelet/pod-resources/kubelet.sock)
      # podResourcesSocketPath: /var/lib/kubelet/pod-resources/kubelet.sock
```

The time-slicing ConfigMap referenced above must be deployed separately:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: time-slicing-config
  namespace: gpu-operator
data:
  any: |-
    version: v1
    flags:
      migStrategy: none
    sharing:
      timeSlicing:
        replicas: 3
        renameByDefault: false
        resources:
          - name: nvidia.com/gpu
            replicas: 3
```

## What GPU Operator does automatically

When `dcgmExporter.perPodGPUUtil.enabled: true` is set, GPU Operator:

1. Sets `DCGM_EXPORTER_ENABLE_PER_POD_GPU_UTIL=true` in the dcgm-exporter
   DaemonSet environment.
2. Mounts `/var/lib/kubelet/pod-resources/` as a read-only `hostPath` volume
   so dcgm-exporter can reach the kubelet pod-resources gRPC socket.
3. Sets `hostPID: true` on the DaemonSet so dcgm-exporter can read
   `/proc/<pid>/cgroup` to resolve NVML PIDs to containers.

## Emitted metric

```
# HELP dcgm_fi_dev_sm_util_per_pod SM utilization attributed to a pod (time-slicing)
# TYPE dcgm_fi_dev_sm_util_per_pod gauge
dcgm_fi_dev_sm_util_per_pod{
  gpu="0",
  uuid="GPU-abc123",
  pod="synapse-proxy-7f9d4b-xkz2p",
  namespace="synapse-staging",
  container="proxy"
} 42
dcgm_fi_dev_sm_util_per_pod{...,pod="synapse-jina-...",container="jina"} 18
dcgm_fi_dev_sm_util_per_pod{...,pod="synapse-vllm-...",container="vllm"} 35
```

## Example Prometheus alert

```yaml
groups:
  - name: per-pod-gpu
    rules:
      - alert: PodGPUHighUtilization
        expr: dcgm_fi_dev_sm_util_per_pod > 80
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "{{ $labels.namespace }}/{{ $labels.pod }} using >80% GPU SM"
```

## Cost model (example: g4dn.xlarge T4)

| Setup | Nodes | Cost/day |
|-------|-------|----------|
| 3 workloads, no time-slicing | 3 × g4dn.xlarge | ~$38/day |
| 3 workloads, time-slicing (3 replicas) | 1 × g4dn.xlarge | ~$13/day |
| **Savings** | | **~$25/day (~$9,000/year)** |

Time-slicing is appropriate for inference + embedding workloads that do not
fully saturate the GPU. For compute-bound training workloads, MIG or dedicated
GPUs remain the right choice.

## Security considerations

Enabling `perPodGPUUtil` grants dcgm-exporter:
- Read access to `/var/lib/kubelet/pod-resources/` (lists all GPU-using pods)
- Host PID namespace access (to read `/proc/<pid>/cgroup`)

These are the same permissions used by other node-level monitoring agents
(e.g., node-exporter, cAdvisor). Review your security policy before enabling
in sensitive environments.

## Compatibility

| GPU Operator | dcgm-exporter | Feature available |
|-------------|---------------|-------------------|
| < v24.x | any | No |
| ≥ v24.x | < v3.4.0 | Field accepted but no-op |
| ≥ v24.x | ≥ v3.4.0 | Yes |

## Related

- dcgm-exporter feature: [docs/per-pod-gpu-metrics.md](https://github.com/NVIDIA/dcgm-exporter/blob/main/docs/per-pod-gpu-metrics.md)
- Time-slicing setup: [GPU Sharing with Time-Slicing](https://docs.nvidia.com/datacenter/cloud-native/gpu-operator/latest/gpu-sharing.html)
- Issue: [NVIDIA/dcgm-exporter#587](https://github.com/NVIDIA/dcgm-exporter/issues/587)
