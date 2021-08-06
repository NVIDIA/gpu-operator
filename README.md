[![GitHub license](https://img.shields.io/github/license/NVIDIA/gpu-operator?style=flat-square)](https://raw.githubusercontent.com/NVIDIA/gpu-operator/master/LICENSE)

# NVIDIA GPU Operator

![nvidia-gpu-operator](https://www.nvidia.com/content/dam/en-zz/Solutions/Data-Center/egx/nvidia-egx-platform-gold-image-full-2c50-d@2x.jpg)

Kubernetes provides access to special hardware resources such as NVIDIA GPUs, NICs, Infiniband adapters and other devices through the [device plugin framework](https://kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/device-plugins/). However, configuring and managing nodes with these hardware resources requires configuration of multiple software components such as drivers, container runtimes or other libraries which  are difficult and prone to errors.
The NVIDIA GPU Operator uses the [operator framework](https://coreos.com/blog/introducing-operator-framework) within Kubernetes to automate the management of all NVIDIA software components needed to provision GPU. These components include the NVIDIA drivers (to enable CUDA), Kubernetes device plugin for GPUs, the NVIDIA Container Runtime, automatic node labelling, [DCGM](https://developer.nvidia.com/dcgm) based monitoring and others.

## Audience and Use-Cases
The GPU Operator allows administrators of Kubernetes clusters to manage GPU nodes just like CPU nodes in the cluster. Instead of provisioning a special OS image for GPU nodes, administrators can rely on a standard OS image for both CPU and GPU nodes and then rely on the GPU Operator to provision the required software components for GPUs.

Note that the GPU Operator is specifically useful for scenarios where the Kubernetes cluster needs to scale quickly - for example provisioning additional GPU nodes on the cloud or on-prem and managing the lifecycle of the underlying software components. Since the GPU Operator runs everything as containers including NVIDIA drivers, the administrators can easily swap various components - simply by starting or stopping containers.

## Product Documentation
For information on platform support and getting started, visit the official documentation [repository](https://docs.nvidia.com/datacenter/cloud-native/gpu-operator/overview.html). 

## Contributions
[Read the document on contributions](https://github.com/NVIDIA/gpu-operator/blob/master/CONTRIBUTING.md). You can contribute by opening a [pull request](https://help.github.com/en/articles/about-pull-requests).

## Support and Getting Help
Please open [an issue on the GitHub project](https://github.com/NVIDIA/gpu-operator/issues/new) for any questions. Your feedback is appreciated.
