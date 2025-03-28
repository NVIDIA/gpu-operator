---
name: Bug report
about: Create a report to help us improve
title: ''
labels: ''
assignees: ''

---

_**Important Note:  NVIDIA AI Enterprise customers can get support from NVIDIA Enterprise support. Please open a case [here](https://enterprise-support.nvidia.com/s/create-case)**._

**Describe the bug**
A clear and concise description of what the bug is.

**To Reproduce**
Detailed steps to reproduce the issue.

**Expected behavior**
A clear and concise description of what you expected to happen.

**Environment (please provide the following information):**
 - GPU Operator Version: [e.g. v25.3.0]
 - OS: [e.g. Ubuntu24.04]
 - Kernel Version: [e.g. 6.8.0-generic]
 - Container Runtime Version: [e.g. containerd 2.0.0]
 - Kubernetes Distro and Version: [e.g. K8s, OpenShift, Rancher, GKE, EKS]



**Information to [attach](https://help.github.com/articles/file-attachments-on-issues-and-pull-requests/)** (optional if deemed irrelevant)

 - [ ] kubernetes pods status: `kubectl get pods -n OPERATOR_NAMESPACE`
 - [ ] kubernetes daemonset status: `kubectl get ds -n OPERATOR_NAMESPACE`
 - [ ] If a pod/ds is in an error state or pending state `kubectl describe pod -n OPERATOR_NAMESPACE POD_NAME`
 - [ ] If a pod/ds is in an error state or pending state `kubectl logs -n OPERATOR_NAMESPACE POD_NAME --all-containers`
 - [ ] Output from running `nvidia-smi` from the driver container: `kubectl exec DRIVER_POD_NAME -n OPERATOR_NAMESPACE -c nvidia-driver-ctr -- nvidia-smi`
 - [ ] containerd logs `journalctl -u containerd > containerd.log`


Collecting full debug bundle (optional):

```
curl -o must-gather.sh -L https://raw.githubusercontent.com/NVIDIA/gpu-operator/main/hack/must-gather.sh
chmod +x must-gather.sh
./must-gather.sh
```
**NOTE**: please refer to the [must-gather](https://raw.githubusercontent.com/NVIDIA/gpu-operator/main/hack/must-gather.sh) script for debug data collected.

This bundle can be submitted to us via email: **operator_feedback@nvidia.com**
