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

## Development build and deploy

The following steps describe how to build a development image of the operator and deploy it to a Kubernetes cluster either via kustomize (Makefile) or Helm.

### Build a development image

Set your desired image name and tag, then build the container image using the repository Makefile.

```bash
export IMAGE_NAME=<registry>/<repo>/gpu-operator   
export VERSION=<tag>                              

# Build the image locally
make build-image IMAGE_NAME="$IMAGE_NAME" VERSION="$VERSION"

# Optional: push as part of the build
# PUSH_ON_BUILD=true make build-image IMAGE_NAME="$IMAGE_NAME" VERSION="$VERSION"
```

### Deploy using Helm

The Helm chart is located at `deployments/gpu-operator`. To use a custom operator image, set `operator.repository` and `operator.version`. The validator uses the same image by default; set its repository/version as well if you publish under a different repository or tag.

```bash
helm upgrade --install gpu-operator deployments/gpu-operator \
  --namespace gpu-operator --create-namespace \
  --set operator.repository=$(dirname "$IMAGE_NAME") \
  --set operator.image=$(basename "$IMAGE_NAME") \
  --set operator.version="$VERSION" \
  --set validator.repository=$(dirname "$IMAGE_NAME") \
  --set validator.image=$(basename "$IMAGE_NAME") \
  --set validator.version="$VERSION"

# Verify
kubectl logs -n gpu-operator deployment/gpu-operator -f

# Cleanup
helm uninstall gpu-operator -n gpu-operator
```

### Deploy using kustomize (Makefile)

This deploys the operator manifests from `config/default` and injects your built image.

```bash
# Creates/updates resources in the current kube-context
make deploy IMAGE_NAME="$IMAGE_NAME" VERSION="$VERSION"

# View operator logs
kubectl logs -n gpu-operator deployment/gpu-operator -f

# Cleanup
make undeploy
```

Notes:
- `operator.image` and `validator.image` default to `gpu-operator`; override only if you changed the image name itself. If you kept the image name as `gpu-operator`, you can omit the `...image=` flags and set only `...repository` and `...version`.
- To update CRDs during Helm upgrades, see `deployments/gpu-operator/templates/upgrade_crd.yaml` and related values in `values.yaml`.


## Webinar
[How to easily use GPUs on Kubernetes](https://info.nvidia.com/how-to-use-gpus-on-kubernetes-webinar.html)

## Contributions
[Read the document on contributions](https://github.com/NVIDIA/gpu-operator/blob/master/CONTRIBUTING.md). You can contribute by opening a [pull request](https://help.github.com/en/articles/about-pull-requests).

## Support and Getting Help
Please open [an issue on the GitHub project](https://github.com/NVIDIA/gpu-operator/issues/new) for any questions. Your feedback is appreciated.

