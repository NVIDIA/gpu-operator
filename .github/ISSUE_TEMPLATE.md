_The template below is mostly useful for bug reports and support questions. Feel free to remove anything which doesn't apply to you and add more information where it makes sense._

### 1. Quick Debug Checklist
- [ ] Are you running on an Ubuntu 18.04 node?
- [ ] Are you running Kubernetes v1.13+?
- [ ] Are you running Docker (>= 18.06) or CRIO (>= 1.13+)?
- [ ] Do you have `i2c_core` and `ipmi_msghandler` loaded on the nodes?
- [ ] Did you apply the CRD (`kubectl describe specialresources --all-namespaces`)

### 1. Issue or feature description

### 2. Steps to reproduce the issue

### 3. Information to [attach](https://help.github.com/articles/file-attachments-on-issues-and-pull-requests/) (optional if deemed irrelevant)

 - [ ] kubernetes pods status: `kubectl get pods --all-namespaces`
 - [ ] kubernetes daemonset status: `kubectl get ds --all-namespaces`
 - [ ] If a pod/ds is in an error state or pending state `kubectl describe pod -n NAMESPACE POD_NAME`
 - [ ] If a pod/ds is in an error state or pending state `kubectl logs -n NAMESPACE POD_NAME`

 - [ ] Output of running a container on the GPU machine: `docker run -it alpine echo foo`
 - [ ] Docker configuration file: `cat /etc/docker/daemon.json`
 - [ ] Docker runtime configuration: `docker info | grep runtime`

 - [ ] NVIDIA shared directory: `ls -la /run/nvidia`
 - [ ] NVIDIA packages directory: `ls -la /run/nvidia/toolkit`
 - [ ] NVIDIA driver directory: `ls -la /run/nvidia/driver`
 - [ ] kubelet logs `journalctl -u kubelet > kubelet.logs`
