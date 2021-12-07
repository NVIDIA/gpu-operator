[![GitHub license](https://img.shields.io/github/license/NVIDIA/gpu-operator?style=flat-square)](https://raw.githubusercontent.com/NVIDIA/gpu-operator/master/LICENSE)

# NVIDIA GPU Operator

## Visual Studio Code Debugger

This section describes a workflow that can be used to run the GPU Operator via VS Code debugger directly against a Kubernetes cluster of your choice.

### Prerequisites

* Cluster with GPUs
* Kubeconfig with cluster admin credentials placed in `${GIT_WORK_TREE}/hack/kubeconfig`

### First setup

* Copy `launch.json` and `tasks.json` from `${GIT_WORK_TREE}/hack/vscode/` into `${GIT_WORK_TREE}/.vscode/`
* If needed, adjust namespace in the `${GIT_WORK_TREE}/hack/prepare-env.sh` script (the default one is `nvidia-gpu-operator`)

### Run

In order to run the operator locally against the remote cluster, use the "Run" or "Start Debugging" option in the VS Code.

Use breakpoints, watches and any other feature like you would debug any other Golang application.
