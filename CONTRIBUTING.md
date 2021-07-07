# Contribute to the NVIDIA GPU Operator

## Introduction
Kubernetes provides access to special hardware resources such as NVIDIA GPUs, NICs, Infiniband adapters and other devices through the device plugin [framework](https://kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/device-plugins/).
However, managing special hardware resources in Kubernetes is difficult and prone to errors. GPUs and NICs require special drivers, plugins, runtimes, libraries to ensure fully high-performance functionality.

![nvidia-gpu-operator](https://www.nvidia.com/content/dam/en-zz/Solutions/Data-Center/egx/nvidia-egx-platform-gold-image-full-2c50-d@2x.jpg)

Moreover, NVIDIA software components such as drivers have been traditionally deployed as part of the base operating system image. This meant that there was a different image for CPU vs. GPU nodes that infrastructure teams would have to manage as part of the software lifecycle. This in turn requires sophisticated automation as part of the provisioning phase for GPU nodes in Kubernetes.

The NVIDIA GPU Operator was primarily built to address these challenges. It leverages the standard [Operator Framework](https://coreos.com/blog/introducing-operator-framework) within Kubernetes to automate the management of all NVIDIA software components needed to provision GPUs within Kubernetes.

The NVIDIA GPU Operator is an open-source product built and maintained by NVIDIA. It is currently validated on a set of platforms (including specific NVIDIA GPUs, operating systems and deployment configurations). The purpose of this document is to briefly describe the architecture of the GPU Operator, so that partners can extend the GPU Operator to support other platforms.

## Architecture
The GPU Operator is made up of the following software components - each of the components runs as a container, including NVIDIA drivers. The associated code is linked to each of the components below:

* [gpu-operator](https://gitlab.com/nvidia/kubernetes/gpu-operator)
* [k8s-device-plugin](https://github.com/NVIDIA/k8s-device-plugin)
* [driver](https://gitlab.com/nvidia/container-images/driver)
* [container-toolkit](https://gitlab.com/nvidia/container-toolkit/container-config)
* [dcgm-exporter](https://gitlab.com/nvidia/container-toolkit/gpu-monitoring-tools)
* [samples](https://gitlab.com/nvidia/container-images/samples/-/tree/master/cuda/rhel-ubi8/vector-add)

```
gitlab.com/
├── nvidia/
│   ├── gpu-operator		(CRD and controller logic that implements the reconciliation)
│   ├── k8s-device-plugin	(NVIDIA Device Plugin for Kubernetes)
│   ├── driver              (NVIDIA Driver qualified for data center GPUs)
│   ├── container-toolkit   (NVIDIA Container Toolkit, runtime for Docker)
│   ├── dcgm-exporter    	(NVIDIA DCGM for monitoring and telemetry)
│   ├── samples		        (CUDA VectorAdd sample used for validation steps)
```

## License
The NVIDIA GPU Operator is open-source and its components are licensed under the permissive Apache 2.0 license.

## Artifacts
The NVIDIA GPU Operator has three artifacts as part of the product release:
1. [Source Code](#source-code)
1. [Container Images](#container-images)
1. [Helm Charts](#helm-charts)

The GPU Operator releases follow semantic versioning.

### <a name="source-code"></a> Source Code

The NVIDIA GPU Operator is available on two external source code repositories:
* GitHub: https://github.com/NVIDIA/gpu-operator
* GitLab: https://gitlab.com/nvidia/kubernetes/gpu-operator

The product page of the GPU Operator is available on NVIDIA’s official repository on GitHub. GitHub is where we interact primarily with users for issues related to the operator. GitHub is a mirror of the source code repository on GitLab - no development happens on GitHub.

GitLab is where the GPU Operator is actively developed - we leverage GitLab’s CI/CD infrastructure for build, test, package and release of the Operator. GitLab is where we expect users and partners to contribute patches (“Merge Requests” or “MRs”) against the source code repository. MRs do not require explicit contributor license agreements (CLA), but we expect contributors to sign their work.

### <a name="container-images"></a> Container Images

Releases of the GPU Operator include container images that are currently available on NVIDIA’s Docker Hub [repository](https://hub.docker.com/u/nvidia). In the future, the operator will be available on [NVIDIA NGC Catalog](https://ngc.nvidia.com/).

The following are the container images (and tag format) that are released:
```
├── nvidia/
│   ├── gpu-operator		(<version-number>)
│   ├── k8s-device-plugin	(<version-number><os--base-image>)
│   ├── driver          (<driver-branch><version-number><kernel-version><operating-system>)
│   ├── container-toolkit	(<version-number><os-base-image>)
│   ├── dcgm-exporter	    (<dcgm-version><version-number><os-base-image>)
│   ├── samples		        (<version-number><sample-name>)
```

### <a name="helm-charts"></a> Helm Charts
To simplify the deployment, the Operator can be installed using a Helm chart (note only Helm v3 is supported). A Helm chart repository is maintained at the following URL: https://nvidia.github.io/gpu-operator (which in turn is maintained at the corresponding ‘gh-pages’ directory under https://github.com/NVIDIA/gpu-operator/tree/gh-pages)

Continuous (‘nightly’) releases of the operator are available. Release milestones are available under ‘stable’.
```
├── nightly/index.yaml
├── stable/index.yaml	(default when installing the operator)
```
## Contributions
NVIDIA is willing to work with partners for adding platform support for the GPU Operator. The GPU Operator is open-source and permissively licensed under the Apache 2.0 license with only minimal requirements for source code [contributions](#signing).

To get started with building the GPU Operator, follow these steps:

```shell
$ git clone https://gitlab.com/nvidia/kubernetes/gpu-operator.git
$ cd gpu-operator
$ make .build-image
```
We also use a CI infrastructure on AWS for nightly and per-change testing on the GPU Operator. This infrastructure is available here: https://gitlab.com/nvidia/container-infrastructure/aws-kube-ci

To ensure that the GPU Operator releases can be effectively validated on new platforms, it would be ideal for contributions to make available CI infrastructure (e.g. runners) and associated changes to the CI scripts.

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
