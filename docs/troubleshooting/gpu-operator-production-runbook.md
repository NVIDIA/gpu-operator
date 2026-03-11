# NVIDIA GPU Operator — Production Troubleshooting Runbook

## Purpose

This runbook helps Kubernetes operators diagnose and resolve common problems in clusters running the [NVIDIA GPU Operator](https://github.com/NVIDIA/gpu-operator), including:

- GPUs not visible on nodes
- GPU workloads stuck in `Pending`
- Driver / container toolkit / device plugin failures
- Missing GPU metrics from DCGM Exporter
- MIG configuration issues
- Operator upgrades and driver lifecycle problems

Written for production and high-scale environments, with real commands and representative output examples showing both healthy and unhealthy states.

---

## Table of Contents

1. [Architecture Overview](#1-architecture-overview)
2. [First-Response Checklist](#2-first-response-checklist)
3. [GPUs Not Visible on the Node](#3-symptom-gpus-are-not-visible-on-the-node)
4. [GPU Workload Stuck in Pending](#4-symptom-gpu-workload-is-stuck-in-pending)
5. [Driver DaemonSet Failing](#5-symptom-driver-daemonset-is-failing)
6. [Container Toolkit Broken](#6-symptom-container-toolkit-is-broken)
7. [DCGM Exporter Metrics Missing](#7-symptom-dcgm-exporter-metrics-are-missing)
8. [MIG Configuration Issues](#8-symptom-mig-configuration-issues)
9. [Validator Pod Never Completes](#9-symptom-validator-pod-never-completes)
10. [ClusterPolicy and Operand State Issues](#10-symptom-clusterpolicy-and-operand-state-issues)
11. [Driver Upgrade Failures](#11-symptom-driver-upgrade-failures)
12. [GPU Feature Discovery Issues](#12-symptom-gpu-feature-discovery-issues)
13. [Operator Pods Timeout on MIG-Enabled Nodes](#13-symptom-operator-pods-fail-with-context-deadline-exceeded-on-mig-enabled-nodes)
14. [Using the GPU Operator with Host-Installed Drivers](#14-using-the-gpu-operator-with-host-installed-drivers)
15. [High-Scale and Multi-Node Debugging](#15-high-scale-and-multi-node-debugging)
16. [Recommended Debugging Flow](#16-recommended-debugging-flow)
17. [Minimal Command Bundle for Incident Triage](#17-minimal-command-bundle-for-incident-triage)
18. [Useful Node Labels Reference](#18-useful-node-labels-reference)

---

## 1. Architecture Overview

The NVIDIA GPU Operator manages the full software stack required to expose GPUs to Kubernetes workloads. It deploys and manages the following components as DaemonSets via the `ClusterPolicy` custom resource:

| Component | DaemonSet Name | Purpose |
|---|---|---|
| GPU Operator | `gpu-operator` | Controller that reconciles the `ClusterPolicy` CR |
| NVIDIA Driver | `nvidia-driver-daemonset` | Compiles/installs the NVIDIA kernel driver on each GPU node |
| Container Toolkit | `nvidia-container-toolkit-daemonset` | Configures the container runtime (containerd/crio) for GPU access |
| Device Plugin | `nvidia-device-plugin-daemonset` | Registers `nvidia.com/gpu` resources with the kubelet |
| GPU Feature Discovery | `gpu-feature-discovery` | Labels nodes with GPU properties (model, driver version, MIG capability, etc.) |
| DCGM Exporter | `nvidia-dcgm-exporter` | Exposes GPU telemetry on `/metrics` for Prometheus scraping |
| DCGM (standalone) | `nvidia-dcgm` | Standalone DCGM hostengine (disabled by default; embedded in exporter) |
| Operator Validator | `nvidia-operator-validator` | Validates each layer of the GPU stack is functional |
| MIG Manager | `nvidia-mig-manager` | Manages Multi-Instance GPU configuration (deployed on MIG-capable nodes) |
| Node Status Exporter | `nvidia-node-status-exporter` | Reports per-node GPU operand status |

The operator deploys components in a dependency chain. If an upstream component fails, all downstream components will also fail or remain in an init state.

```
Driver → Container Toolkit → Validator → Device Plugin → DCGM → DCGM Exporter → GPU Feature Discovery
```

Each component DaemonSet only runs on nodes that have the corresponding deploy label set to `"true"` (e.g., `nvidia.com/gpu.deploy.driver=true`). These labels are automatically managed by the operator. The operator detects GPU nodes by checking NFD PCI labels (`feature.node.kubernetes.io/pci-10de.present`, `feature.node.kubernetes.io/pci-0302_10de.present`, `feature.node.kubernetes.io/pci-0300_10de.present`) and then sets the `nvidia.com/gpu.present=true` label on matching nodes.

---

## 2. First-Response Checklist

Start every GPU debugging session with these commands. List all operator pods, check the ClusterPolicy status, inspect node resources, and look at recent events:

```bash
kubectl get pods -n gpu-operator -o wide
kubectl get clusterpolicy cluster-policy -o jsonpath='{.status}' | jq .
kubectl describe node <gpu-node-name> | grep -A 15 "Capacity\|Allocatable"
kubectl get events -n gpu-operator --sort-by=.lastTimestamp | tail -30
```

A healthy ClusterPolicy status looks like:

```json
{
  "state": "ready",
  "namespace": "gpu-operator"
}
```

Healthy events show Normal/Started entries for the driver, toolkit, device plugin, and exporter pods:

```
LAST SEEN   TYPE     REASON              OBJECT                                          MESSAGE
2m          Normal   Started             pod/nvidia-driver-daemonset-9l6mz               Started container nvidia-driver-ctr
2m          Normal   Started             pod/nvidia-container-toolkit-daemonset-4p2k9     Started container nvidia-container-toolkit-ctr
2m          Normal   Started             pod/nvidia-device-plugin-daemonset-hxk7l         Started container nvidia-device-plugin
2m          Normal   Started             pod/nvidia-dcgm-exporter-j2p5m                  Started container nvidia-dcgm-exporter
```

Warning events indicate problems — look for `BackOff`, `Failed`, or `Unhealthy` reasons:

```
LAST SEEN   TYPE      REASON              OBJECT                                          MESSAGE
30s         Warning   BackOff             pod/nvidia-driver-daemonset-9l6mz               Back-off restarting failed container nvidia-driver-ctr
45s         Warning   Failed              pod/nvidia-driver-daemonset-9l6mz               Error: failed to start container "nvidia-driver-ctr": ...
1m          Warning   Unhealthy           pod/nvidia-operator-validator-5n8qf              Readiness probe failed
```

### Healthy output

```
$ kubectl get pods -n gpu-operator -o wide
NAME                                                    READY   STATUS    RESTARTS   AGE    NODE
gpu-operator-6b7d9d7c9b-z8v7f                          1/1     Running   0          2d     control-plane-1
nvidia-container-toolkit-daemonset-4p2k9                1/1     Running   0          2d     gpu-node-1
nvidia-dcgm-exporter-j2p5m                              1/1     Running   0          2d     gpu-node-1
nvidia-device-plugin-daemonset-hxk7l                    1/1     Running   0          2d     gpu-node-1
nvidia-driver-daemonset-9l6mz                           1/1     Running   0          2d     gpu-node-1
nvidia-operator-validator-5n8qf                         1/1     Running   0          2d     gpu-node-1
gpu-feature-discovery-7d9x2                             1/1     Running   0          2d     gpu-node-1
```

### Unhealthy output

```
$ kubectl get pods -n gpu-operator -o wide
NAME                                                    READY   STATUS                  RESTARTS   AGE    NODE
gpu-operator-6b7d9d7c9b-z8v7f                          1/1     Running                 0          2d     control-plane-1
nvidia-container-toolkit-daemonset-4p2k9                0/1     CrashLoopBackOff        7          22m    gpu-node-1
nvidia-device-plugin-daemonset-hxk7l                    0/1     Init:CrashLoopBackOff   12         22m    gpu-node-1
nvidia-driver-daemonset-9l6mz                           0/1     Init:Error              4          22m    gpu-node-1
nvidia-dcgm-exporter-j2p5m                              0/1     Pending                 0          22m    gpu-node-1
nvidia-operator-validator-5n8qf                         0/1     Init:2/4                6          22m    gpu-node-1
```

In the unhealthy example above, the driver pod is in `Init:Error`. Since the driver is first in the dependency chain, everything downstream (toolkit, device plugin, validator) is also failing. Always start debugging from the first failing component in the chain.

---

## 3. Symptom: GPUs Are Not Visible on the Node

### What you typically notice

- `kubectl describe node` shows no `nvidia.com/gpu` in `Capacity` / `Allocatable`
- Workloads requesting GPUs stay in `Pending`
- The device plugin pod may be failing or missing

### Check node allocatable resources

```bash
kubectl describe node <gpu-node-name> | grep -A 15 "Capacity\|Allocatable"
```

Healthy — GPUs reported:

```
Capacity:
  cpu:                128
  ephemeral-storage:  1843431736Ki
  hugepages-1Gi:      0
  hugepages-2Mi:      0
  memory:             263700Mi
  nvidia.com/gpu:     8
Allocatable:
  cpu:                127800m
  ephemeral-storage:  1798400Mi
  hugepages-1Gi:      0
  hugepages-2Mi:      0
  memory:             258580Mi
  nvidia.com/gpu:     8
```

Broken — the `nvidia.com/gpu` resource is missing entirely:

```
Capacity:
  cpu:                128
  ephemeral-storage:  1843431736Ki
  memory:             263700Mi
Allocatable:
  cpu:                127800m
  ephemeral-storage:  1798400Mi
  memory:             258580Mi
```

### Likely causes

1. **Driver pod failed** — the NVIDIA kernel module is not loaded
2. **Container toolkit pod failed** — the runtime was not configured
3. **Device plugin failed to register** — GPUs were not advertised to the kubelet
4. **Node Feature Discovery (NFD) not running** — node is not labeled with `nvidia.com/gpu.present=true`, so the operator never schedules operands on it
5. **GPU hardware not detected by the host** — check PCIe visibility at the host level

### Check PCIe visibility (host-level, requires node SSH)

List all NVIDIA PCI devices by vendor ID `10de`:

```bash
lspci -nn | grep -i nvidia
```

Healthy — GPUs visible on the PCIe bus:

```
17:00.0 3D controller [0302]: NVIDIA Corporation GA100 [A100 SXM4 80GB] [10de:20b2] (rev a1)
65:00.0 3D controller [0302]: NVIDIA Corporation GA100 [A100 SXM4 80GB] [10de:20b2] (rev a1)
b5:00.0 3D controller [0302]: NVIDIA Corporation GA100 [A100 SXM4 80GB] [10de:20b2] (rev a1)
ca:00.0 3D controller [0302]: NVIDIA Corporation GA100 [A100 SXM4 80GB] [10de:20b2] (rev a1)
```

No output means the host OS cannot see any NVIDIA devices on the PCIe bus. This indicates a hardware-level problem (GPU not seated, PCIe link failure, BIOS/UEFI configuration, or hypervisor passthrough not configured). Escalate to infrastructure/hardware team.

For more detail on a specific GPU device:

```bash
lspci -vvs 17:00.0
```

Expected output:

```
17:00.0 3D controller: NVIDIA Corporation GA100 [A100 SXM4 80GB] (rev a1)
        Subsystem: NVIDIA Corporation Device 1463
        Physical Slot: 23
        Control: I/O- Mem+ BusMaster+ SpecCycle- ...
        Status: Cap+ 66MHz- UDF- FastB2B- ParErr- ...
        Kernel driver in use: nvidia
        Kernel modules: nvidia
```

If `Kernel driver in use` does not show `nvidia`, the NVIDIA driver is not bound to the device. Check the driver pod logs (Section 5).

### Debugging steps

Check if the operator has labeled the node with GPU labels:

```bash
kubectl get node <gpu-node-name> --show-labels | tr ',' '\n' | grep nvidia
```

Healthy — GPU labels present:

```
nvidia.com/gpu.present=true
nvidia.com/gpu.deploy.driver=true
nvidia.com/gpu.deploy.device-plugin=true
nvidia.com/gpu.deploy.container-toolkit=true
nvidia.com/gpu.product=NVIDIA-A100-SXM4-80GB
```

No output or only partial labels means NFD has not detected the GPU, or the operator hasn't processed the node yet.

List operator pods running on the specific node:

```bash
kubectl get pods -n gpu-operator --field-selector spec.nodeName=<gpu-node-name>
```

Healthy:

```
NAME                                                    READY   STATUS    RESTARTS   AGE
nvidia-container-toolkit-daemonset-4p2k9                1/1     Running   0          2d
nvidia-dcgm-exporter-j2p5m                              1/1     Running   0          2d
nvidia-device-plugin-daemonset-hxk7l                    1/1     Running   0          2d
nvidia-driver-daemonset-9l6mz                           1/1     Running   0          2d
nvidia-operator-validator-5n8qf                         1/1     Running   0          2d
gpu-feature-discovery-7d9x2                             1/1     Running   0          2d
```

No pods listed means the operator is not scheduling operands on this node — check NFD labels.

Check driver and device plugin logs:

```bash
kubectl logs -n gpu-operator -l app=nvidia-driver-daemonset --tail=100
kubectl logs -n gpu-operator -l app=nvidia-device-plugin-daemonset --tail=100
```

### Device plugin, healthy log pattern

```
I0308 10:11:42.123456       1 main.go:154] Starting FS watcher.
I0308 10:11:42.123789       1 main.go:161] Starting OS watcher.
I0308 10:11:42.124900       1 main.go:176] Loading configuration.
I0308 10:11:42.130011       1 server.go:216] Starting GRPC server for 'nvidia.com/gpu'
I0308 10:11:42.130455       1 server.go:148] Registered device plugin for 'nvidia.com/gpu' with Kubelet
```

### Device plugin, broken log pattern

```
E0308 10:15:10.556123       1 main.go:123] error starting plugins:
error getting plugins: failed to construct NVML resource managers:
could not load NVML library
```

If the device plugin cannot initialize NVML, the problem is below the Kubernetes layer. The NVIDIA driver or container toolkit is not ready on the node. Work your way down the dependency chain.

---

## 4. Symptom: GPU Workload Is Stuck in Pending

### Check the pod events

```bash
kubectl describe pod <pod-name> -n <namespace>
```

Common scheduling failure:

```
Events:
  Type     Reason            Age   From               Message
  ----     ------            ----  ----               -------
  Warning  FailedScheduling  2m    default-scheduler  0/10 nodes are available:
                                                      5 Insufficient nvidia.com/gpu,
                                                      3 node(s) had untolerated taint {nvidia.com/gpu: },
                                                      2 node(s) didn't match Pod's node affinity/selector
```

### Common causes

| Cause | How to verify |
|---|---|
| No free GPUs in the cluster | `kubectl describe nodes \| grep -A 5 "nvidia.com/gpu"`, compare `Capacity` vs `Allocatable` vs `Allocated` |
| GPU resource never advertised | See [Section 3](#3-symptom-gpus-are-not-visible-on-the-node) |
| Missing toleration for `nvidia.com/gpu` taint | Check pod spec for required `tolerations` |
| Wrong `nodeSelector` or `nodeAffinity` | Verify pod's scheduling constraints match actual node labels |
| MIG resource type mismatch | Pod requests `nvidia.com/gpu` but node only exposes MIG slices like `nvidia.com/mig-1g.10gb` |

### Check cluster-wide GPU allocation

```bash
kubectl describe nodes | grep -E "nvidia.com/gpu|nvidia.com/mig" | sort | uniq -c
```

Example output (8 GPUs, 5 allocated):

```
     10  nvidia.com/gpu:     8
     10  nvidia.com/gpu:     8
      5  nvidia.com/gpu  5
```

### Healthy pod events

```
Events:
  Type    Reason     Age   From               Message
  ----    ------     ----  ----               -------
  Normal  Scheduled  10s   default-scheduler  Successfully assigned ai/llm-inference-0 to gpu-node-1
  Normal  Pulling    9s    kubelet            Pulling image "nvcr.io/nvidia/pytorch:24.01-py3"
  Normal  Pulled     5s    kubelet            Successfully pulled image
  Normal  Created    4s    kubelet            Created container server
  Normal  Started    4s    kubelet            Started container server
```

---

## 5. Symptom: Driver DaemonSet Is Failing

The GPU Operator compiles and installs the NVIDIA driver on each GPU node (unless configured with `driver.enabled=false` for pre-installed drivers). The driver DaemonSet uses the `OnDelete` update strategy to avoid unintended node disruptions.

### Check the driver pods

```bash
kubectl get pods -n gpu-operator -l app=nvidia-driver-daemonset
kubectl logs -n gpu-operator -l app=nvidia-driver-daemonset --tail=200
```

Healthy:

```
$ kubectl get pods -n gpu-operator -l app=nvidia-driver-daemonset
NAME                                READY   STATUS    RESTARTS   AGE
nvidia-driver-daemonset-9l6mz       1/1     Running   0          2d
nvidia-driver-daemonset-b3k4n       1/1     Running   0          2d
```

Broken:

```
$ kubectl get pods -n gpu-operator -l app=nvidia-driver-daemonset
NAME                                READY   STATUS            RESTARTS   AGE
nvidia-driver-daemonset-9l6mz       0/1     Init:Error        4          22m
nvidia-driver-daemonset-b3k4n       0/1     CrashLoopBackOff  8          22m
```

### Broken example — kernel headers missing

```
$ kubectl logs -n gpu-operator -l app=nvidia-driver-daemonset --tail=50

================ Installing NVIDIA driver for Linux kernel 6.8.0-1012-aws ================
--> Installing Linux kernel headers...
ERROR: Failed to find Linux kernel headers for kernel 6.8.0-1012-aws
ERROR: Cannot build the NVIDIA kernel module without kernel headers.
```

### Broken example — secure boot or module signing

```
ERROR: failed to load NVIDIA kernel module
nvidia.ko: Required key not available
modprobe: ERROR: could not insert 'nvidia': Operation not permitted
```

### Broken example — driver version mismatch during upgrade

```
WARNING: An NVIDIA kernel module 'nvidia' appears to already be loaded in your kernel.
ERROR: Installation of the kernel module for the NVIDIA Accelerated Graphics Driver
(version 580.126.20) failed. ERROR: The kernel module failed to build.
```

### Common causes

| Cause | Resolution |
|---|---|
| Kernel headers not installed on the host | Install matching kernel-headers/kernel-devel package on the node OS |
| Unsupported kernel version | Check the [NVIDIA driver compatibility matrix](https://docs.nvidia.com/datacenter/tesla/drivers/index.html) |
| Secure Boot enabled | Sign the kernel module or disable Secure Boot if policy allows |
| Pre-existing NVIDIA driver on host conflicts | Remove host-installed drivers or set `driver.enabled=false` in ClusterPolicy |
| Node kernel was upgraded without restarting driver pod | Delete the driver pod to trigger reinstallation: `kubectl delete pod -n gpu-operator -l app=nvidia-driver-daemonset --field-selector spec.nodeName=<node>` |

### Host-side validation (if node SSH is available)

Verify the running kernel version matches what the driver pod is building for:

```bash
uname -r
```

Check if kernel headers are present:

```bash
ls /usr/src/kernels/$(uname -r) 2>/dev/null || ls /usr/src/linux-headers-$(uname -r) 2>/dev/null
```

Check if NVIDIA modules are loaded:

```bash
lsmod | grep nvidia
```

Expected:

```
nvidia_uvm          1503232  0
nvidia_drm            77824  0
nvidia_modeset       1306624  1 nvidia_drm
nvidia              57049088  2 nvidia_uvm,nvidia_modeset
```

Test driver communication:

```bash
nvidia-smi
```

Healthy `nvidia-smi` output:

```
+-----------------------------------------------------------------------------------------+
| NVIDIA-SMI 580.126.20            Driver Version: 580.126.20     CUDA Version: 12.8      |
|-------------------------------------------+------------------------+--------------------+
| GPU  Name                 Persistence-M   | Bus-Id          Disp.A | Volatile Uncorr. ECC |
|===========================================+========================+====================+
|   0  NVIDIA A100-SXM4-80GB         On     | 00000000:17:00.0  Off  |                    0 |
|   1  NVIDIA A100-SXM4-80GB         On     | 00000000:65:00.0  Off  |                    0 |
+-------------------------------------------+------------------------+--------------------+
```

Broken `nvidia-smi` output:

```
NVIDIA-SMI has failed because it couldn't communicate with the NVIDIA driver.
Make sure that the latest NVIDIA driver is installed and running.
```

---

## 6. Symptom: Container Toolkit Is Broken

The NVIDIA Container Toolkit configures the container runtime (containerd or CRI-O) so that containers can access GPUs. Without it, pods will be scheduled on GPU nodes but the container will not see any GPU device.

### Check toolkit pod

```bash
kubectl get pods -n gpu-operator -l app=nvidia-container-toolkit-daemonset
kubectl logs -n gpu-operator -l app=nvidia-container-toolkit-daemonset --tail=100
```

Healthy:

```
$ kubectl get pods -n gpu-operator -l app=nvidia-container-toolkit-daemonset
NAME                                              READY   STATUS    RESTARTS   AGE
nvidia-container-toolkit-daemonset-4p2k9           1/1     Running   0          2d
```

Healthy log output:

```
time="2026-03-08T10:15:00Z" level=info msg="Parsing arguments"
time="2026-03-08T10:15:00Z" level=info msg="Successfully configured runtime"
time="2026-03-08T10:15:01Z" level=info msg="Restarting containerd"
time="2026-03-08T10:15:03Z" level=info msg="NVIDIA Container Toolkit configured successfully"
```

### Broken log examples

```
time="2026-03-08T10:20:31Z" level=error msg="failed to configure container runtime"
time="2026-03-08T10:20:31Z" level=error msg="could not update containerd config: config file /etc/containerd/config.toml: no such file or directory"
```

```
time="2026-03-08T10:20:31Z" level=error msg="error setting up toolkit: unable to locate nvidia-cdi-hook"
```

### What you observe at workload level

The pod schedules and starts, but the application inside sees no GPU:

```python
>>> import torch
>>> torch.cuda.is_available()
False
```

Or:

```
RuntimeError: Found no NVIDIA driver on your system.
Please check that you have an NVIDIA GPU and installed a driver from
http://www.nvidia.com/Download/index.aspx
```

### Common causes

| Cause | Resolution |
|---|---|
| containerd/CRI-O config path is non-standard | Set `toolkit.env` with the correct config path in ClusterPolicy |
| Driver pod is not healthy | Fix the driver first, toolkit depends on it |
| CDI is misconfigured | Check `cdi.enabled` in ClusterPolicy and verify CDI spec files in `/var/run/cdi/` on the node |
| Custom runtime class not created | Verify `nvidia` RuntimeClass exists: `kubectl get runtimeclass nvidia` |

### Verify the RuntimeClass

```bash
kubectl get runtimeclass nvidia -o yaml
```

Expected:

```yaml
apiVersion: node.k8s.io/v1
kind: RuntimeClass
metadata:
  name: nvidia
handler: nvidia
```

---

## 7. Symptom: DCGM Exporter Metrics Are Missing

DCGM Exporter exposes GPU telemetry over a `/metrics` HTTP endpoint for Prometheus-style scraping. By default it uses an embedded DCGM hostengine (standalone DCGM is disabled by default).

### Check exporter pod

```bash
kubectl get pods -n gpu-operator -l app=nvidia-dcgm-exporter
kubectl logs -n gpu-operator -l app=nvidia-dcgm-exporter --tail=100
```

Healthy:

```
$ kubectl get pods -n gpu-operator -l app=nvidia-dcgm-exporter
NAME                           READY   STATUS    RESTARTS   AGE
nvidia-dcgm-exporter-j2p5m    1/1     Running   0          2d
nvidia-dcgm-exporter-k8m2n    1/1     Running   0          2d
```

### Test the metrics endpoint

Port-forward to the exporter service, then fetch metrics:

```bash
kubectl port-forward -n gpu-operator svc/nvidia-dcgm-exporter 9400:9400 &
curl -s localhost:9400/metrics | head -30
```

Healthy output:

```
# HELP DCGM_FI_DEV_GPU_UTIL GPU utilization (in %).
# TYPE DCGM_FI_DEV_GPU_UTIL gauge
DCGM_FI_DEV_GPU_UTIL{gpu="0",UUID="GPU-7b3f1a2c-..."} 78
DCGM_FI_DEV_GPU_UTIL{gpu="1",UUID="GPU-a2c89f31-..."} 42

# HELP DCGM_FI_DEV_FB_USED Framebuffer memory used (in MiB).
# TYPE DCGM_FI_DEV_FB_USED gauge
DCGM_FI_DEV_FB_USED{gpu="0",UUID="GPU-7b3f1a2c-..."} 28672
DCGM_FI_DEV_FB_USED{gpu="1",UUID="GPU-a2c89f31-..."} 16384

# HELP DCGM_FI_DEV_FB_FREE Framebuffer memory free (in MiB).
# TYPE DCGM_FI_DEV_FB_FREE gauge
DCGM_FI_DEV_FB_FREE{gpu="0",UUID="GPU-7b3f1a2c-..."} 52608
```

Broken — connection refused:

```
curl: (7) Failed to connect to localhost port 9400: Connection refused
```

Broken — scrape error:

```
# HELP dcgm_exporter_scrape_error 1 if there was an error while scraping DCGM
# TYPE dcgm_exporter_scrape_error gauge
dcgm_exporter_scrape_error 1
```

### Common causes

| Cause | Resolution |
|---|---|
| Exporter pod not running | Check pod events and logs |
| Driver or toolkit not healthy | Fix upstream components first |
| Prometheus ServiceMonitor not created | Enable `dcgmExporter.serviceMonitor.enabled=true` in Helm values |
| Custom metrics ConfigMap misconfigured | Verify the `dcgm-metrics.csv` key exists in the referenced ConfigMap |
| Network policy blocking scrape | Ensure Prometheus can reach port 9400 on the exporter pods |

### Verify ServiceMonitor (if using Prometheus Operator)

```bash
kubectl get servicemonitor -n gpu-operator
```

Expected:

```
NAME                    AGE
nvidia-dcgm-exporter    2d
```

---

## 8. Symptom: MIG Configuration Issues

Multi-Instance GPU (MIG) allows partitioning a single GPU into multiple isolated instances. MIG Manager handles configuration and may stop/restart operator-managed GPU clients during reconfiguration.

### Check MIG Manager

```bash
kubectl get pods -n gpu-operator -l app=nvidia-mig-manager
kubectl logs -n gpu-operator -l app=nvidia-mig-manager --tail=200
```

Healthy:

```
$ kubectl get pods -n gpu-operator -l app=nvidia-mig-manager
NAME                          READY   STATUS    RESTARTS   AGE
nvidia-mig-manager-x7k2m      1/1     Running   0          2d
```

Healthy log output:

```
Applying MIG configuration 'all-1g.10gb'
Asserting MIG mode is enabled on all GPUs
MIG mode is already enabled on GPU 0
MIG mode is already enabled on GPU 1
Successfully updated to MIG config: all-1g.10gb
Restarting operator-managed GPU clients
MIG configuration applied successfully
```

Broken — invalid configuration:

```
error applying MIG configuration
device 0 has an invalid MIG configuration
failed to apply MIG config: incompatible MIG mode on device 0
```

Broken — MIG mode not enabled:

```
MIG mode is not enabled on GPU 0
Failed to enable MIG mode: reboot required
```

### Check node MIG labels

```bash
kubectl get node <gpu-node-name> --show-labels | tr ',' '\n' | grep -i mig
```

Healthy labels:

```
nvidia.com/mig.capable=true
nvidia.com/mig.config=all-1g.10gb
nvidia.com/mig.config.state=success
nvidia.com/mig.strategy=single
nvidia.com/gpu.deploy.mig-manager=true
```

Problematic labels (stuck in pending):

```
nvidia.com/mig.capable=true
nvidia.com/mig.config=all-1g.10gb
nvidia.com/mig.config.state=pending
nvidia.com/mig.strategy=single
```

### Verify MIG devices on the node

From inside the driver pod or via node SSH:

```bash
nvidia-smi mig -lgi
```

Expected output (A100 with all-1g.10gb):

```
+-------------------------------------------------------+
| GPU instances:                                        |
| GPU   Name          Profile  Instance   Placement     |
|                       ID       ID       Start:Size    |
|=======================================================|
|   0  MIG 1g.10gb       19       1          0:1        |
|   0  MIG 1g.10gb       19       2          1:1        |
|   0  MIG 1g.10gb       19       3          2:1        |
|   0  MIG 1g.10gb       19       4          3:1        |
|   0  MIG 1g.10gb       19       5          4:1        |
|   0  MIG 1g.10gb       19       6          5:1        |
|   0  MIG 1g.10gb       19       7          6:1        |
+-------------------------------------------------------+
```

### Common causes and resolutions

| Cause | Resolution |
|---|---|
| MIG mode not enabled on the GPU | Enable MIG mode: `nvidia-smi -i 0 -mig 1` then reboot the node |
| Unsupported MIG profile for the GPU model | Verify profile support: `nvidia-smi mig -lgip` |
| Host GPU clients blocking reconfiguration | Drain GPU workloads before applying MIG config |
| MIG config state stuck in `pending` | Check MIG Manager logs; may need a node reboot |
| Pod requests `nvidia.com/gpu` instead of MIG slice | Use the correct resource name: `nvidia.com/mig-1g.10gb` (depends on MIG strategy) |

When using `mig.strategy=single`, all MIG slices on a GPU must be the same profile. With `mig.strategy=mixed`, different profiles can coexist but each is advertised as a separate resource type (e.g., `nvidia.com/mig-1g.10gb`, `nvidia.com/mig-2g.20gb`).

---

## 9. Symptom: Validator Pod Never Completes

The operator validator runs as a DaemonSet with multiple init containers, each validating one layer of the GPU stack. It is a symptom indicator, not a root cause.

### Check validator pod

```bash
kubectl get pods -n gpu-operator -l app=nvidia-operator-validator
kubectl describe pod -n gpu-operator -l app=nvidia-operator-validator
kubectl logs -n gpu-operator <validator-pod-name> --all-containers --tail=200
```

Healthy:

```
$ kubectl get pods -n gpu-operator -l app=nvidia-operator-validator
NAME                                    READY   STATUS    RESTARTS   AGE
nvidia-operator-validator-5n8qf         1/1     Running   0          2d
```

Broken:

```
$ kubectl get pods -n gpu-operator -l app=nvidia-operator-validator
NAME                                    READY   STATUS      RESTARTS   AGE
nvidia-operator-validator-7969z         0/1     Init:2/4    6          18m
```

### Interpreting init container status

```bash
kubectl get pod -n gpu-operator <validator-pod-name> -o jsonpath='{.status.initContainerStatuses[*].name}' | tr ' ' '\n'
```

Output:

```
driver-validation
toolkit-validation
cuda-validation
plugin-validation
```

The init containers run in order:

1. `driver-validation` — checks that the NVIDIA driver is loaded
2. `toolkit-validation` — checks that the container toolkit is configured
3. `cuda-validation` — runs a CUDA workload to verify end-to-end GPU functionality
4. `plugin-validation` — checks that GPUs are registered with the kubelet via the device plugin

If the validator is stuck at `Init:2/4`, the first two validations (driver, toolkit) passed but CUDA validation is failing.

### Resolution

Do not debug the validator itself. Identify which init container is failing and debug the corresponding component:

- `Init:0/4` → Debug the **driver** (Section 5)
- `Init:1/4` → Debug the **container toolkit** (Section 6)
- `Init:2/4` → Debug **CUDA / application-level** GPU access (driver + toolkit working, but CUDA test fails)
- `Init:3/4` → Debug the **device plugin** (Section 3)

---

## 10. Symptom: ClusterPolicy and Operand State Issues

The `ClusterPolicy` CR is the single source of truth for the GPU Operator's desired state.

### Check ClusterPolicy status

```bash
kubectl get clusterpolicy cluster-policy -o yaml
```

Key fields to look for:

```yaml
apiVersion: nvidia.com/v1
kind: ClusterPolicy
metadata:
  name: cluster-policy
spec:
  driver:
    enabled: true
    version: "580.126.20"
  toolkit:
    enabled: true
  devicePlugin:
    enabled: true
  dcgmExporter:
    enabled: true
status:
  state: ready               # <-- should be "ready"
  namespace: gpu-operator
```

Quick status check and full status dump:

```bash
kubectl get clusterpolicy cluster-policy -o jsonpath='{.status.state}'
kubectl get clusterpolicy cluster-policy -o json | jq '.status'
```

Healthy status:

```json
{
  "state": "ready",
  "namespace": "gpu-operator"
}
```

### Check ClusterPolicy conditions

```bash
kubectl get clusterpolicy cluster-policy -o json | jq '.status.conditions'
```

Healthy:

```json
[
  {
    "type": "Ready",
    "status": "True",
    "reason": "Reconciled",
    "message": "All GPU operands are ready",
    "lastTransitionTime": "2026-03-06T10:00:00Z"
  }
]
```

Unhealthy:

```json
[
  {
    "type": "Ready",
    "status": "False",
    "reason": "DriverNotReady",
    "message": "nvidia-driver-daemonset pods are not ready",
    "lastTransitionTime": "2026-03-08T10:20:00Z"
  }
]
```

The operator sets specific condition reasons that help narrow down the issue:

| Condition Reason | Meaning |
|---|---|
| `Reconciled` | All components reconciled successfully |
| `ReconcileFailed` | General reconciliation failure, check operator logs |
| `NFDLabelsMissing` | No NFD labels found, GPU nodes cannot be discovered. Verify NFD is running |
| `NoGPUNodes` | No nodes with `nvidia.com/gpu.present=true` found in the cluster |
| `DriverNotReady` | Driver DaemonSet pods are not ready |
| `OperandNotReady` | One or more GPU operand pods are failing |
| `NodeStatusExporterNotReady` | Node status exporter pods are not ready |
| `ConflictingNodeSelector` | NVIDIADriver node selectors conflict (v1alpha1 CRD) |

### Common issues

- **ClusterPolicy not found:** The Helm chart was not installed or CRDs are missing. Verify CRDs exist:
  ```bash
  kubectl get crd | grep nvidia
  ```
  Expected:
  ```
  clusterpolicies.nvidia.com    2026-03-01T10:00:00Z
  nvidiadrivers.nvidia.com      2026-03-01T10:00:00Z
  ```

- **ClusterPolicy state is `notReady`:** One or more components are failing. Check individual pod statuses.

- **`NFDLabelsMissing` condition:** NFD is not running or not labeling GPU nodes. Check if NFD is deployed:
  ```bash
  kubectl get pods -A -l app.kubernetes.io/name=node-feature-discovery
  ```
  Expected:
  ```
  NAMESPACE                  NAME                                     READY   STATUS    RESTARTS   AGE
  node-feature-discovery     nfd-controller-manager-7b8f5c6d4-x2k9m   1/1     Running   0          5d
  node-feature-discovery     nfd-worker-4p2k9                          1/1     Running   0          5d
  ```

  The operator checks three NFD PCI labels — at least one must be present:
  ```bash
  kubectl get node <gpu-node-name> --show-labels | tr ',' '\n' | grep "pci.*10de"
  ```
  Expected (at least one):
  ```
  feature.node.kubernetes.io/pci-10de.present=true
  feature.node.kubernetes.io/pci-0300_10de.present=true
  feature.node.kubernetes.io/pci-0302_10de.present=true
  ```

- **Multiple ClusterPolicies exist:** Only one `ClusterPolicy` should exist in the cluster. Additional instances will be set to `ignored` state.
  ```bash
  kubectl get clusterpolicy
  ```
  Expected:
  ```
  NAME             STATUS   AGE
  cluster-policy   ready    30d
  ```

---

## 11. Symptom: Driver Upgrade Failures

The GPU Operator supports automatic driver upgrades controlled by the `driver.upgradePolicy` section. Driver upgrades can be disruptive and require careful handling in production.

### Check upgrade state

Check if auto-upgrade is enabled:

```bash
kubectl get clusterpolicy cluster-policy -o jsonpath='{.spec.driver.upgradePolicy}' | jq .
```

Expected:

```json
{
  "autoUpgrade": true,
  "maxParallelUpgrades": 1,
  "maxUnavailable": "25%",
  "podDeletion": {
    "force": false,
    "timeoutSeconds": 300,
    "deleteEmptyDir": false
  },
  "drain": {
    "enable": false,
    "force": false,
    "timeoutSeconds": 300,
    "deleteEmptyDir": false
  }
}
```

Check node annotations for upgrade state:

```bash
kubectl get node <gpu-node-name> -o jsonpath='{.metadata.annotations}' | jq 'with_entries(select(.key | startswith("nvidia")))'
```

Idle (no upgrade in progress):

```json
{
  "nvidia.com/driver-upgrade.driver-version": "580.126.20",
  "nvidia.com/driver-upgrade.state": ""
}
```

During an active upgrade:

```json
{
  "nvidia.com/driver-upgrade.driver-version": "580.126.20",
  "nvidia.com/driver-upgrade.state": "upgrade-required"
}
```

Check the upgrade controller logs:

```bash
kubectl logs -n gpu-operator deployment/gpu-operator --tail=200 | grep -i upgrade
```

Healthy upgrade:

```
level=info msg="Starting driver upgrade on node gpu-node-1"
level=info msg="Draining GPU pods from node gpu-node-1"
level=info msg="Successfully drained GPU pods from node gpu-node-1"
level=info msg="Driver upgrade completed on node gpu-node-1"
```

### Common upgrade failures

Pods still running during upgrade:

```
WARNING: GPU pods are still running on node gpu-node-1
Waiting for GPU pod deletion (timeout: 300s)
ERROR: Timed out waiting for GPU pods to be deleted
```

The operator tries to delete GPU pods before reloading the driver. If pods are not deleted within the timeout, check which pods are blocking:

```bash
kubectl get pods --all-namespaces --field-selector spec.nodeName=<gpu-node-name> -o wide | grep nvidia
```

Example:

```
ai          llm-inference-0              1/1     Running   0     4h    gpu-node-1   <none>
ai          training-job-7b2k9           1/1     Running   0     12h   gpu-node-1   <none>
gpu-operator nvidia-dcgm-exporter-j2p5m  1/1     Running   0     2d    gpu-node-1   <none>
```

If safe, manually cordon and drain the node:

```bash
kubectl cordon <gpu-node-name>
kubectl drain <gpu-node-name> --ignore-daemonsets --delete-emptydir-data --timeout=300s
```

Expected:

```
node/gpu-node-1 cordoned
WARNING: ignoring DaemonSet-managed Pods
evicting pod ai/llm-inference-0
evicting pod ai/training-job-7b2k9
pod/llm-inference-0 evicted
pod/training-job-7b2k9 evicted
node/gpu-node-1 drained
```

### Upgrade policy configuration reference

```yaml
driver:
  upgradePolicy:
    autoUpgrade: true
    maxParallelUpgrades: 1        # How many nodes upgrade simultaneously
    maxUnavailable: "25%"          # Max unavailable nodes during upgrade
    podDeletion:
      force: false                 # Force-delete GPU pods
      timeoutSeconds: 300          # Timeout waiting for pod deletion
      deleteEmptyDir: false        # Delete pods using emptyDir volumes
    drain:
      enable: false                # Enable kubectl drain before driver reload
      force: false
      timeoutSeconds: 300
      deleteEmptyDir: false
```

In large clusters, set `maxParallelUpgrades` to a conservative value (e.g., 1-3) and enable `drain.enable=true` to ensure clean node evacuation before driver reload.

---

## 12. Symptom: GPU Feature Discovery Issues

GPU Feature Discovery (GFD) labels nodes with GPU properties that are used for scheduling decisions. If GFD is not working, nodes may be missing important labels.

### Check GFD pods

```bash
kubectl get pods -n gpu-operator -l app=gpu-feature-discovery
kubectl logs -n gpu-operator -l app=gpu-feature-discovery --tail=100
```

Healthy:

```
$ kubectl get pods -n gpu-operator -l app=gpu-feature-discovery
NAME                            READY   STATUS    RESTARTS   AGE
gpu-feature-discovery-7d9x2      1/1     Running   0          2d
gpu-feature-discovery-m4k8p      1/1     Running   0          2d
```

Healthy log output:

```
I0308 10:12:00.123456       1 main.go:52] Starting GPU Feature Discovery
I0308 10:12:00.234567       1 main.go:85] GPU Feature Discovery successfully started
I0308 10:12:00.345678       1 factory.go:68] Detected 8 NVIDIA GPU(s) on node
I0308 10:12:00.456789       1 labeler.go:112] Updating node labels
I0308 10:12:00.567890       1 labeler.go:145] Successfully updated node labels
```

### Verify expected node labels

```bash
kubectl get node <gpu-node-name> --show-labels | tr ',' '\n' | grep nvidia
```

Expected labels (example for A100):

```
nvidia.com/cuda.driver.major=12
nvidia.com/cuda.driver.minor=8
nvidia.com/cuda.runtime.major=12
nvidia.com/cuda.runtime.minor=8
nvidia.com/gfd.timestamp=1709884800
nvidia.com/gpu.compute.major=8
nvidia.com/gpu.compute.minor=0
nvidia.com/gpu.count=8
nvidia.com/gpu.deploy.container-toolkit=true
nvidia.com/gpu.deploy.dcgm=true
nvidia.com/gpu.deploy.dcgm-exporter=true
nvidia.com/gpu.deploy.device-plugin=true
nvidia.com/gpu.deploy.driver=true
nvidia.com/gpu.deploy.gpu-feature-discovery=true
nvidia.com/gpu.deploy.node-status-exporter=true
nvidia.com/gpu.deploy.operator-validator=true
nvidia.com/gpu.family=ampere
nvidia.com/gpu.machine=NVIDIA-DGX-A100
nvidia.com/gpu.memory=81920
nvidia.com/gpu.present=true
nvidia.com/gpu.product=NVIDIA-A100-SXM4-80GB
nvidia.com/gpu.replicas=1
nvidia.com/mig.capable=true
nvidia.com/mig.strategy=single
```

### Missing labels

If labels are missing, check:

1. GFD pod is running and healthy
2. The driver pod is healthy (GFD depends on the driver)
3. NFD is running and has labeled the node with the PCI labels the operator needs

Check if NFD is running:

```bash
kubectl get pods -n node-feature-discovery
```

Or if NFD is deployed by the GPU operator:

```bash
kubectl get pods -n gpu-operator -l app=nfd
```

Expected:

```
$ kubectl get pods -n node-feature-discovery
NAME                                     READY   STATUS    RESTARTS   AGE
nfd-controller-manager-7b8f5c6d4-x2k9m   1/1     Running   0          5d
nfd-worker-4p2k9                          1/1     Running   0          5d
nfd-worker-j8m3n                          1/1     Running   0          5d
```

---

## 13. Symptom: Operator Pods Fail with Context Deadline Exceeded on MIG-Enabled Nodes

When MIG is configured on GPUs with many instances (e.g., `1g.23gb` on B200, which creates up to 8 instances per GPU), operator pods such as `gpu-feature-discovery`, `nvidia-device-plugin`, or `nvidia-operator-validator` may fail with `context deadline exceeded` errors during startup.

### Root cause

The NVIDIA kernel driver uses internal locks to serialize access when processes query GPU state through NVML or `nvidia-smi`. With MIG enabled, each MIG instance is a separate device handle, multiplying the amount of work per query.

When containerd is running, the `nvidia-container-runtime` plugin holds NVML handles to all GPU devices. This creates lock contention at the kernel driver level: any concurrent `nvidia-smi` or NVML call must wait for the lock.

On a node with many MIG instances, this causes `nvidia-smi` execution time to increase significantly. For example, on B200 GPUs with `1g.23gb` MIG profile:

- With containerd **stopped**: `nvidia-smi` completes in ~5 seconds
- With containerd **running**: `nvidia-smi` takes ~44 seconds

When the node starts and all GPU operator pods are scheduled simultaneously, they all query the driver at the same time — creating a "query storm" that pushes response times beyond the configured timeouts.

### Diagnose the issue

Measure `nvidia-smi` execution time on the affected node:

```bash
time nvidia-smi
```

If this takes more than 30 seconds, the node is likely affected by driver lock contention.

Check for pods in `CrashLoopBackOff` or with `context deadline exceeded` in their logs:

```bash
kubectl get pods -n gpu-operator -o wide | grep -v Running
kubectl logs -n gpu-operator -l app=gpu-feature-discovery --tail=50
kubectl logs -n gpu-operator -l app=nvidia-device-plugin-daemonset --tail=50
```

Look for messages like:

```
context deadline exceeded
nvidia-smi failed
GPU resources are not discovered by the node
```

### Where the timeouts are defined

The driver container startup probe runs `nvidia-smi` directly. The probe script is defined in `assets/state-driver/0400_configmap.yaml`:

```sh
if ! nvidia-smi; then
  echo "nvidia-smi failed"
  exit 1
fi
```

The probe timeout defaults are set in `internal/state/driver.go` (`getDefaultStartupProbe()`):

- `TimeoutSeconds: 60` — each probe attempt must complete within 60 seconds
- `PeriodSeconds: 10` — probe runs every 10 seconds
- `FailureThreshold: 120` — pod is killed after 120 consecutive failures

When the startup probe succeeds, it writes `/run/nvidia/validations/.driver-ctr-ready`. Other components (GFD, device-plugin, DCGM, MIG manager) have init containers that poll for this file every 5 seconds with no upper timeout:

```yaml
args: ["until [ -f /run/nvidia/validations/toolkit-ready ]; do echo waiting for nvidia container stack to be setup; sleep 5; done"]
```

The operator validator has a hard-coded GPU resource discovery timeout of 150 seconds (30 retries x 5 seconds), defined in `cmd/nvidia-validator/main.go`:

```go
gpuResourceDiscoveryWaitRetries     = 30
gpuResourceDiscoveryIntervalSeconds = 5
```

If the device-plugin hasn't registered MIG resources within 2.5 minutes (because it is also waiting on slow NVML calls), the validator fails.

### Workarounds

**Increase startup probe timeouts via ClusterPolicy:**

The `ClusterPolicy` CRD exposes probe configuration on the driver spec (`api/nvidia/v1/clusterpolicy_types.go`, `ContainerProbeSpec`):

```yaml
apiVersion: nvidia.com/v1
kind: ClusterPolicy
metadata:
  name: cluster-policy
spec:
  driver:
    startupProbe:
      initialDelaySeconds: 120
      timeoutSeconds: 120
      periodSeconds: 15
      failureThreshold: 180
```

**Stagger operator component startup:**

Temporarily disable components on the node, let the driver/toolkit initialize first, then enable the rest:

```bash
# Disable components initially
kubectl label node <gpu-node> nvidia.com/gpu.deploy.gpu-feature-discovery=false --overwrite
kubectl label node <gpu-node> nvidia.com/gpu.deploy.device-plugin=false --overwrite

# Wait for driver and toolkit pods to be Running
kubectl get pods -n gpu-operator -l app=nvidia-driver-daemonset -w

# Then enable components one at a time
kubectl label node <gpu-node> nvidia.com/gpu.deploy.device-plugin=true --overwrite
# Wait for device-plugin to be Running, then:
kubectl label node <gpu-node> nvidia.com/gpu.deploy.gpu-feature-discovery=true --overwrite
```

**Staged MIG application (if MIG is managed outside the operator):**

1. `kubectl cordon <node>`
2. `systemctl stop kubelet && systemctl stop containerd`
3. Apply MIG configuration
4. `systemctl start containerd && systemctl start kubelet`
5. `kubectl uncordon <node>`

This avoids the concurrent pod startup storm since pods come up sequentially after the node rejoins.

### Common causes and resolutions

| Cause | Resolution |
|---|---|
| Many MIG instances causing slow `nvidia-smi` | Increase startup probe `timeoutSeconds` in ClusterPolicy |
| All operator pods starting simultaneously | Stagger component startup using node labels |
| Hard-coded 150s validator timeout too short | Apply MIG config before starting kubelet (staged approach) |
| containerd + MIG lock contention | Cordon node, stop services, configure MIG, restart |

---

## 14. Using the GPU Operator with Host-Installed Drivers

The GPU Operator does not require managing the NVIDIA driver. If you already have the NVIDIA driver installed directly on your nodes (e.g., via package manager, Base Command Manager, or a pre-built machine image), you can still use the operator for all the other components: container toolkit, device plugin, GPU feature discovery, DCGM, DCGM exporter, MIG manager, and the operator validator.

### Configuration

Disable the driver component in the `ClusterPolicy`:

```yaml
apiVersion: nvidia.com/v1
kind: ClusterPolicy
metadata:
  name: cluster-policy
spec:
  driver:
    enabled: false
  # All other components remain enabled by default
  toolkit:
    enabled: true
  devicePlugin:
    enabled: true
  dcgm:
    enabled: true
  dcgmExporter:
    enabled: true
  migManager:
    enabled: true
  gfd:
    enabled: true
```

Or via Helm:

```bash
helm install gpu-operator nvidia/gpu-operator \
  --set driver.enabled=false
```

### What you get

With `driver.enabled=false`, the operator skips the driver DaemonSet but still deploys:

- **nvidia-container-toolkit** — configures the container runtime (containerd/CRI-O) to expose GPUs inside containers
- **nvidia-device-plugin** — registers GPU resources with the Kubernetes scheduler (`nvidia.com/gpu` or `nvidia.com/mig-*`)
- **gpu-feature-discovery** — labels nodes with GPU properties (model, memory, compute capability, MIG status)
- **dcgm / dcgm-exporter** — GPU health monitoring and Prometheus metrics
- **nvidia-mig-manager** — manages MIG partition lifecycle
- **nvidia-operator-validator** — validates the full stack is functional

### Prerequisites

When using host-installed drivers, ensure:

1. The NVIDIA kernel module is loaded (`lsmod | grep nvidia`)
2. `nvidia-smi` works on the host
3. The driver version is compatible with the operator version (check the [GPU Operator compatibility matrix](https://docs.nvidia.com/datacenter/cloud-native/gpu-operator/latest/platform-support.html))
4. The NVIDIA device files exist under `/dev` (`/dev/nvidia0`, `/dev/nvidiactl`, `/dev/nvidia-uvm`, etc.)

### Verify the setup

```bash
# Check that the driver is detected even though it's not managed by the operator
kubectl get pods -n gpu-operator -l app=nvidia-driver-daemonset
# Expected: no pods (driver DaemonSet is not deployed)

# Verify the toolkit detects the host driver
kubectl logs -n gpu-operator -l app=nvidia-container-toolkit-daemonset --tail=20

# Confirm device plugin registered GPU resources
kubectl get node <gpu-node> -o json | jq '.status.capacity | with_entries(select(.key | startswith("nvidia.com")))'
```

Expected output:

```json
{
  "nvidia.com/gpu": "8"
}
```

Or with MIG enabled:

```json
{
  "nvidia.com/mig-1g.23gb": "8"
}
```

### Troubleshooting

If operator components fail with host-installed drivers, check:

| Symptom | Check |
|---|---|
| Toolkit pod stuck or failing | `nvidia-smi` works on the host; driver device files exist under `/dev` |
| Device plugin shows 0 GPUs | Toolkit pod is running; runtime is correctly configured (`/etc/nvidia-container-runtime/config.toml`) |
| Validator init container stuck on `driver-validation` | Host driver is loaded and functional; `/run/nvidia/driver` is accessible |

---

## 15. High-Scale and Multi-Node Debugging

In clusters with hundreds of GPU nodes, targeted debugging is essential.

### Get a cluster-wide GPU health overview

Count GPU nodes:

```bash
kubectl get nodes -l nvidia.com/gpu.present=true --no-headers | wc -l
```

Find nodes where operands are not deployed:

```bash
kubectl get nodes -l nvidia.com/gpu.present=true,nvidia.com/gpu.deploy.operands!=true --no-headers
```

Example (one problem node):

```
gpu-node-04   Ready   <none>   30d   v1.31.2
```

If all nodes are healthy, this returns no output.

Find nodes where the driver is not healthy:

```bash
kubectl get pods -n gpu-operator -l app=nvidia-driver-daemonset --field-selector status.phase!=Running -o wide
```

Example:

```
NAME                                READY   STATUS            RESTARTS   AGE    NODE
nvidia-driver-daemonset-b3k4n       0/1     CrashLoopBackOff  8          22m    gpu-node-04
```

If all driver pods are healthy, this returns no output.

Find all nodes with failing operator pods:

```bash
kubectl get pods -n gpu-operator --field-selector status.phase=Failed -o wide
kubectl get pods -n gpu-operator --field-selector status.phase!=Running,status.phase!=Succeeded -o wide
```

Example:

```
NAME                                                    READY   STATUS            RESTARTS   AGE    NODE
nvidia-driver-daemonset-b3k4n                           0/1     CrashLoopBackOff  8          22m    gpu-node-04
nvidia-container-toolkit-daemonset-r7k2m                0/1     Init:Error        3          22m    gpu-node-04
nvidia-device-plugin-daemonset-p9j4n                    0/1     Init:0/1          0          22m    gpu-node-04
```

### Check GPU allocation across the cluster

GPU capacity vs allocatable per node:

```bash
kubectl get nodes -l nvidia.com/gpu.present=true -o custom-columns=\
"NODE:.metadata.name,\
GPU_CAPACITY:.status.capacity.nvidia\.com/gpu,\
GPU_ALLOCATABLE:.status.allocatable.nvidia\.com/gpu"
```

Example:

```
NODE          GPU_CAPACITY   GPU_ALLOCATABLE
gpu-node-01   8              8
gpu-node-02   8              8
gpu-node-03   8              8
gpu-node-04   <none>         <none>        # <-- Problem node
gpu-node-05   8              8
```

### Collect logs from all failing pods at once

```bash
for pod in $(kubectl get pods -n gpu-operator --field-selector status.phase!=Running -o jsonpath='{.items[*].metadata.name}'); do
  echo "=== $pod ==="
  kubectl logs -n gpu-operator "$pod" --all-containers --tail=50 2>&1
  echo ""
done
```

Example:

```
=== nvidia-driver-daemonset-b3k4n ===
ERROR: Failed to find Linux kernel headers for kernel 6.8.0-1012-aws

=== nvidia-container-toolkit-daemonset-r7k2m ===
time="2026-03-08T10:20:31Z" level=error msg="failed to configure container runtime"

=== nvidia-device-plugin-daemonset-p9j4n ===
(waiting for init container to complete)
```

### Check for heterogeneous GPU configurations

In clusters with mixed GPU models, ensure the right driver and configuration targets the right nodes:

```bash
kubectl get nodes -l nvidia.com/gpu.present=true -o custom-columns=\
"NODE:.metadata.name,\
GPU_PRODUCT:.metadata.labels.nvidia\.com/gpu\.product"
```

Example:

```
NODE           GPU_PRODUCT
gpu-node-01    NVIDIA-A100-SXM4-80GB
gpu-node-02    NVIDIA-A100-SXM4-80GB
gpu-node-03    NVIDIA-H100-SXM5-80GB
gpu-node-04    NVIDIA-L4
```

---

## 16. Recommended Debugging Flow

```
GPU workload failing or pending
        │
        ▼
Check node allocatable GPUs ──────────────────────────────────────────┐
        │                                                             │
        ├── No nvidia.com/gpu resource                                │
        │         │                                                   │
        │         ▼                                                   │
        │   Is NFD running? Node labeled nvidia.com/gpu.present?      │
        │         │                                                   │
        │         ▼                                                   │
        │   Check driver pod → Check toolkit pod → Check device plugin│
        │                                                             │
        ├── nvidia.com/gpu exists but pod still Pending               │
        │         │                                                   │
        │         ▼                                                   │
        │   Check scheduler events, taints, tolerations,              │
        │   nodeSelector, free GPU capacity, MIG resource names       │
        │                                                             │
        ├── Pod runs but app sees no GPU                              │
        │         │                                                   │
        │         ▼                                                   │
        │   Check container toolkit pod + RuntimeClass                │
        │   Check CDI configuration on the node                       │
        │                                                             │
        ├── Metrics missing                                           │
        │         │                                                   │
        │         ▼                                                   │
        │   Check dcgm-exporter pod and /metrics endpoint             │
        │   Check ServiceMonitor and network policies                 │
        │                                                             │
        └── MIG-specific resource issues                              │
                  │                                                   │
                  ▼                                                   │
            Check mig-manager logs, node MIG labels,                  │
            nvidia-smi mig -lgi on the node                           │
──────────────────────────────────────────────────────────────────────┘
```

---

## 17. Minimal Command Bundle for Incident Triage

Copy and run these commands in the first 5 minutes of any GPU incident. They cover operator pod overview, ClusterPolicy state, per-node GPU resources, workload events, component logs in dependency order, and recent events.

```bash
kubectl get pods -n gpu-operator -o wide

kubectl get clusterpolicy cluster-policy -o jsonpath='{.status.state}' && echo

kubectl get nodes -l nvidia.com/gpu.present=true -o custom-columns="NODE:.metadata.name,GPU_CAP:.status.capacity.nvidia\.com/gpu,GPU_ALLOC:.status.allocatable.nvidia\.com/gpu"

kubectl describe node <gpu-node-name> | grep -A 20 "Capacity\|Conditions\|Taints"

kubectl describe pod <gpu-workload-pod> -n <namespace>

kubectl logs -n gpu-operator -l app=nvidia-driver-daemonset --tail=100
kubectl logs -n gpu-operator -l app=nvidia-container-toolkit-daemonset --tail=100
kubectl logs -n gpu-operator -l app=nvidia-operator-validator --all-containers --tail=100
kubectl logs -n gpu-operator -l app=nvidia-device-plugin-daemonset --tail=100
kubectl logs -n gpu-operator -l app=nvidia-dcgm-exporter --tail=100

kubectl logs -n gpu-operator -l app=nvidia-mig-manager --tail=100

kubectl get events -n gpu-operator --sort-by=.lastTimestamp | tail -30
```

---

## 18. Useful Node Labels Reference

Labels automatically managed by the GPU Operator and GPU Feature Discovery:

| Label | Description | Example Value |
|---|---|---|
| `nvidia.com/gpu.present` | GPU detected on node (set by the operator based on NFD PCI labels) | `true` |
| `nvidia.com/gpu.product` | GPU product name | `NVIDIA-A100-SXM4-80GB` |
| `nvidia.com/gpu.count` | Number of GPUs on the node | `8` |
| `nvidia.com/gpu.memory` | GPU memory in MiB | `81920` |
| `nvidia.com/gpu.family` | GPU architecture family | `ampere`, `hopper` |
| `nvidia.com/gpu.compute.major` | Compute capability major version | `8` |
| `nvidia.com/gpu.deploy.driver` | Whether the driver should be deployed | `true` |
| `nvidia.com/gpu.deploy.device-plugin` | Whether the device plugin should be deployed | `true` |
| `nvidia.com/gpu.deploy.operands` | Master switch for all operands on this node | `true` |
| `nvidia.com/mig.capable` | GPU supports MIG | `true` |
| `nvidia.com/mig.config` | Desired MIG configuration | `all-1g.10gb` |
| `nvidia.com/mig.config.state` | Current MIG config state | `success`, `pending`, `failed` |
| `nvidia.com/mig.strategy` | MIG strategy | `single`, `mixed` |
| `nvidia.com/gpu.workload.config` | Workload type for the node | `container`, `vm-passthrough`, `vm-vgpu` |

You can override which components are deployed on a node by manually setting the `nvidia.com/gpu.deploy.<component>` labels. This is useful for debugging or for nodes with special configurations.

