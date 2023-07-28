---
name: GPU Operator Bug report
about: Create a report to help us improve
title: ''
labels: ''
assignees: cdesiniotis, shivamerla

---

_The template below is mostly useful for bug reports and support questions. Feel free to remove anything which doesn't apply to you and add more information where it makes sense._

_**Important Note:  NVIDIA AI Enterprise customers can get support from NVIDIA Enterprise support. Please open a case [here](https://enterprise-support.nvidia.com/s/create-case)**._


### 1. Quick Debug Information
* OS/Version(e.g. RHEL8.6, Ubuntu22.04):
* Kernel Version:
* Container Runtime Type/Version(e.g. Containerd, CRI-O, Docker):
* K8s Flavor/Version(e.g. K8s, OCP, Rancher, GKE, EKS):
* GPU Operator Version:


### 2. Issue or feature description
_Briefly explain the issue in terms of expected behavior and current behavior._

### 3. Steps to reproduce the issue
_Detailed steps to reproduce the issue._

### 4. Information to [attach](https://help.github.com/articles/file-attachments-on-issues-and-pull-requests/) (optional if deemed irrelevant)

 - [ ] kubernetes pods status: `kubectl get pods -n OPERATOR_NAMESPACE`
 - [ ] kubernetes daemonset status: `kubectl get ds -n OPERATOR_NAMESPACE`
 - [ ] If a pod/ds is in an error state or pending state `kubectl describe pod -n OPERATOR_NAMESPACE POD_NAME`
 - [ ] If a pod/ds is in an error state or pending state `kubectl logs -n OPERATOR_NAMESPACE POD_NAME --all-containers`
 - [ ] Output from running `nvidia-smi` from the driver container: `kubectl exec DRIVER_POD_NAME -n OPERATOR_NAMESPACE -c nvidia-driver-ctr -- nvidia-smi`
 - [ ] containerd logs `journalctl -u containerd > containerd.log`


Collecting full debug bundle (optional):

```
curl -o must-gather.sh -L https://raw.githubusercontent.com/NVIDIA/gpu-operator/master/hack/must-gather.sh 
chmod +x must-gather.sh
./must-gather.sh
```
**NOTE**: please refer to the [must-gather](https://raw.githubusercontent.com/NVIDIA/gpu-operator/master/hack/must-gather.sh) script for debug data collected.

This bundle can be submitted to us via email: **operator_feedback@nvidia.com**
