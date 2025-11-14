[![license](https://img.shields.io/github/license/NVIDIA/gpu-operator?style=flat-square)](https://raw.githubusercontent.com/NVIDIA/gpu-operator/master/LICENSE)
[![pipeline status](https://gitlab.com/nvidia/kubernetes/gpu-operator/badges/master/pipeline.svg)](https://gitlab.com/nvidia/kubernetes/gpu-operator/-/pipelines)
[![coverage report](https://gitlab.com/nvidia/kubernetes/gpu-operator/badges/master/coverage.svg)](https://gitlab.com/nvidia/kubernetes/gpu-operator/-/pipelines)

# NVIDIA GPU Operator

![nvidia-gpu-operator](https://www.nvidia.com/content/dam/en-zz/Solutions/Data-Center/egx/nvidia-egx-platform-gold-image-full-2c50-d@2x.jpg)

Kubernetes provides access to special hardware resources such as NVIDIA GPUs, NICs, Infiniband adapters and other devices through the [device plugin framework](https://kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/device-plugins/). However, configuring and managing nodes with these hardware resources requires configuration of multiple software components such as drivers, container runtimes or other libraries which  are difficult and prone to errors.
The NVIDIA GPU Operator uses the [operator framework](https://cloud.redhat.com/blog/introducing-the-operator-framework) within Kubernetes to automate the management of all NVIDIA software components needed to provision GPU. These components include the NVIDIA drivers (to enable CUDA), Kubernetes device plugin for GPUs, the NVIDIA Container Runtime, automatic node labelling, [DCGM](https://developer.nvidia.com/dcgm) based monitoring and others.

## Audience and Use-Cases
The GPU Operator allows administrators of Kubernetes clusters to manage GPU nodes just like CPU nodes in the cluster. Instead of provisioning a special OS image for GPU nodes, administrators can rely on a standard OS image for both CPU and GPU nodes and then rely on the GPU Operator to provision the required software components for GPUs.

Note that the GPU Operator is specifically useful for scenarios where the Kubernetes cluster needs to scale quickly - for example provisioning additional GPU nodes on the cloud or on-prem and managing the lifecycle of the underlying software components. Since the GPU Operator runs everything as containers including NVIDIA drivers, the administrators can easily swap various components - simply by starting or stopping containers.

## Product Documentation
For information on platform support and getting started, visit the official documentation [repository](https://docs.nvidia.com/datacenter/cloud-native/gpu-operator/overview.html).

## Quick Start

Make sure your k8s cluster meets the pre-requisites as listed in the platform support page:

https://docs.nvidia.com/datacenter/cloud-native/gpu-operator/latest/platform-support.html


Step1: Install Helm locally:
```
curl -fsSL -o get_helm.sh https://raw.githubusercontent.com/helm/helm/master/scripts/get-helm-3 \
    && chmod 700 get_helm.sh \
    && ./get_helm.sh
```


Step2: Deploy GPU operator:
```
helm install --wait --generate-name \
    -n gpu-operator --create-namespace \
    nvidia/gpu-operator \
    --version=v25.10.0
```

That's all.

GPU Operator and its operands should be up and running as shown below:
```
gpu-operator        gpu-feature-discovery-98x9m                                       1/1     Running     0          22h
gpu-operator        gpu-operator-1762903711-node-feature-discovery-gc-5c458899bbwpk   1/1     Running     0          22h
gpu-operator        gpu-operator-1762903711-node-feature-discovery-master-856b8tvqs   1/1     Running     0          22h
gpu-operator        gpu-operator-1762903711-node-feature-discovery-worker-m5jdr       1/1     Running     0          22h
gpu-operator        gpu-operator-5b685fc9c9-wntlj                                     1/1     Running     0          22h
gpu-operator        nvidia-container-toolkit-daemonset-c7c6f                          1/1     Running     0          22h
gpu-operator        nvidia-cuda-validator-zt45l                                       0/1     Completed   0          22h
gpu-operator        nvidia-dcgm-exporter-px9hw                                        1/1     Running     0          22h
gpu-operator        nvidia-device-plugin-daemonset-cd4hp                              1/1     Running     0          22h
gpu-operator        nvidia-driver-daemonset-xkqnp                                     1/1     Running     0          22h
gpu-operator        nvidia-mig-manager-jrthj                                          1/1     Running     0          22h
gpu-operator        nvidia-operator-validator-5kq7z                                   1/1     Running     0          22h
```

## Roadmap
### High-level overview of the main priorities for 2026
* Latest data center drivers
* Latest NVIDIA GPU platforms
* Heterogenous cluster (NVIDIADriver CR) from Tech Preview to GA mode
* DRA integration
* GPU Health Check
* Confidential Containers
* Scalability

## Webinar
[How to easily use GPUs on Kubernetes](https://info.nvidia.com/how-to-use-gpus-on-kubernetes-webinar.html)

## Contributions
[Read the document on contributions](https://github.com/NVIDIA/gpu-operator/blob/master/CONTRIBUTING.md). You can contribute by opening a [pull request](https://help.github.com/en/articles/about-pull-requests).

## Support and Getting Help
Please open [an issue on the GitHub project](https://github.com/NVIDIA/gpu-operator/issues/new) for any questions. Your feedback is appreciated.
