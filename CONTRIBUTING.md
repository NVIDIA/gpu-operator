# Contribute to the NVIDIA GPU Operator

## Introduction
Kubernetes provides access to special hardware resources such as NVIDIA GPUs, NICs, Infiniband adapters and other devices through the device plugin [framework](https://kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/device-plugins/).
However, managing special hardware resources in Kubernetes is difficult and prone to errors. GPUs and NICs require special drivers, plugins, runtimes, libraries to ensure fully high-performance functionality.

![nvidia-gpu-operator](https://www.nvidia.com/content/dam/en-zz/Solutions/Data-Center/egx/nvidia-egx-platform-gold-image-full-2c50-d@2x.jpg)

Moreover, NVIDIA software components such as drivers have been traditionally deployed as part of the base operating system image. This meant that there was a different image for CPU vs. GPU nodes that infrastructure teams would have to manage as part of the software lifecycle. This in turn requires sophisticated automation as part of the provisioning phase for GPU nodes in Kubernetes.

The NVIDIA GPU Operator was primarily built to address these challenges. It leverages the standard [Operator Framework](https://cloud.redhat.com/blog/introducing-the-operator-framework) within Kubernetes to automate the management of all NVIDIA software components needed to provision GPUs within Kubernetes.

The NVIDIA GPU Operator is an open-source product built and maintained by NVIDIA. It is currently validated on a set of platforms (including specific NVIDIA GPUs, operating systems and deployment configurations). The purpose of this document is to briefly describe the architecture of the GPU Operator, so that partners can extend the GPU Operator to support other platforms.

## Architecture
The GPU Operator is made up of the following software components - each of the components runs as a container, including NVIDIA drivers. The associated code is linked to each of the components below:

* [gpu-operator](https://github.com/NVIDIA/gpu-operator)
* [k8s-device-plugin](https://github.com/NVIDIA/k8s-device-plugin)
* [driver](https://github.com/NVIDIA/gpu-driver-container)
* [container-toolkit](https://github.com/NVIDIA/nvidia-container-toolkit)
* [dcgm-exporter](https://github.com/NVIDIA/dcgm-exporter)
* [gpu-feature-discovery](https://github.com/NVIDIA/k8s-device-plugin)
* [mig-manager](https://github.com/NVIDIA/mig-parted)
* [sandbox-device-plugin](https://github.com/NVIDIA/kubevirt-gpu-device-plugin)
* [vgpu-device-manager](https://github.com/NVIDIA/vgpu-device-manager)
* [kata-manager](https://github.com/NVIDIA/k8s-kata-manager)
* [samples](https://github.com/NVIDIA/k8s-samples)

```
github.com/
├── NVIDIA/
│   ├── gpu-operator                 (CRD and controller logic that implements the reconciliation)
│   ├── k8s-device-plugin            (NVIDIA Device Plugin for Kubernetes)
│   ├── gpu-driver-container         (NVIDIA Driver qualified for data center GPUs)
│   ├── nvidia-container-toolkit     (NVIDIA Container Toolkit, runtime for Docker)
│   ├── dcgm-exporter                (NVIDIA DCGM for monitoring and telemetry)
│   ├── gpu-feature-discovery        (NVIDIA GPU Feature Discovery for Kubernetes)
│   ├── mig-manager                  (NVIDIA Multi-Instance GPU Manager for Kubernetes)
│   ├── sandbox-device-plugin        (NVIDIA Device Plugin for sandboxed environments)
│   ├── vgpu-device-manager          (NVIDIA vGPU Device Manager for Kubernetes)
│   ├── kata-manager                 (NVIDIA Kata Manager for Kubernetes)
│   ├── samples                      (CUDA VectorAdd sample used for validation steps)
```

## License
The NVIDIA GPU Operator is open-source and its components are licensed under the permissive Apache 2.0 license.

## Artifacts
The NVIDIA GPU Operator has the following artifacts as part of the product release:
1. [Source Code](#source-code)
1. [Documentation](#documentation)
1. [Container Images](#container-images)
1. [Helm Charts](#helm-charts)

The GPU Operator releases follow [calendar versioning](https://calver.org/).

### <a name="source-code"></a> Source Code

The NVIDIA GPU Operator source code is available on GitHub at https://github.com/NVIDIA/gpu-operator

### <a name="source-code"></a> Documentation

The official NVIDIA GPU Operator documentation is available at https://docs.nvidia.com/datacenter/cloud-native/gpu-operator/latest/index.html

### <a name="container-images"></a> Container Images

Releases of the GPU Operator include container images that are currently available on [NVIDIA NGC Catalog](https://ngc.nvidia.com/).

### <a name="helm-charts"></a> Helm Charts
To simplify the deployment, the Operator can be installed using a Helm chart (note only Helm v3 is supported). The documentation for helm installation
can be viewed [here](https://docs.nvidia.com/datacenter/cloud-native/gpu-operator/getting-started.html#install-helm).


## Contributions
NVIDIA is willing to work with partners for adding platform support for the GPU Operator. The GPU Operator is open-source and permissively licensed under the Apache 2.0 license with only minimal requirements for source code [contributions](#signing).

To file feature requests, bugs, or questions, submit an issue at https://github.com/NVIDIA/gpu-operator/issues

To contribute to the project, file a Pull Request at https://github.com/NVIDIA/gpu-operator/pulls. Contributions do not require explicit contributor license agreements (CLA), but we expect contributors to sign their work.

## <a name="signing"></a>Signing your work

Want to hack on the NVIDIA GPU Operator? Awesome!
We only require you to sign your work, the below section describes this!

The sign-off is a simple line at the end of the explanation for the patch. Your
signature certifies that you wrote the patch or otherwise have the right to pass
it on as an open-source patch. The rules are pretty simple: if you can certify
the below (from [developercertificate.org](http://developercertificate.org/)):

```
Developer Certificate of Origin
Version 1.1

Copyright (C) 2004, 2006 The Linux Foundation and its contributors.
1 Letterman Drive
Suite D4700
San Francisco, CA, 94129

Everyone is permitted to copy and distribute verbatim copies of this
license document, but changing it is not allowed.

Developer's Certificate of Origin 1.1

By making a contribution to this project, I certify that:

(a) The contribution was created in whole or in part by me and I
    have the right to submit it under the open source license
    indicated in the file; or

(b) The contribution is based upon previous work that, to the best
    of my knowledge, is covered under an appropriate open source
    license and I have the right under that license to submit that
    work with modifications, whether created in whole or in part
    by me, under the same open source license (unless I am
    permitted to submit under a different license), as indicated
    in the file; or

(c) The contribution was provided directly to me by some other
    person who certified (a), (b) or (c) and I have not modified
    it.

(d) I understand and agree that this project and the contribution
    are public and that a record of the contribution (including all
    personal information I submit with it, including my sign-off) is
    maintained indefinitely and may be redistributed consistent with
    this project or the open source license(s) involved.
```

Then you just add a line to every git commit message:

    Signed-off-by: Joe Smith <joe.smith@email.com>

Use your real name (sorry, no pseudonyms or anonymous contributions.)

If you set your `user.name` and `user.email` git configs, you can sign your
commit automatically with `git commit -s`.
