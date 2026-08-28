# Release Process

The NVIDIA GPU Operator consists of the following artifacts:
- The NVIDIA GPU Operator container image
- The NVIDIA GPU Operator Helm chart
- The NVIDIA GPU Operator OLM bundle

The container image and Helm chart are published to nvcr.io.
The OLM bundle is published to the [Red Hat certified operators production catalog](https://github.com/redhat-openshift-ecosystem/certified-operators).

The NVIDIA GPU Operator is versioned following the [calendar versioning](https://calver.org/) convention.
To learn more about the project's versioning and lifecycle, refer to the [official documentation](https://docs.nvidia.com/datacenter/cloud-native/gpu-operator/latest/platform-support.html#nvidia-gpu-operator-versioning).

# Release Process Checklist:
- [ ] Create a release PR:
    - [ ] Create a new `bump-release-{{ .VERSION }}` branch
    - [ ] Bump the project version in `versions.mk` and in the OLM bundle manifests in `bundle/manifests/`
    - [ ] Create a PR from the created `bump-release-{{ .VERSION }}` branch
- [ ] Merge the release PR
- [ ] Tag the release and push the tag to the `internal` mirror
- [ ] Wait for the container image to be published to nvcr.io
- [ ] Publish the Helm chart to nvcr.io
- [ ] Publish the Helm chart to the `gh-pages` branch
- [ ] Push the tag to the upstream GitHub repo
- [ ] Publish the OLM bundle to the certified operators catalog
    - [ ] Create a PR in the [certified-operators](https://github.com/redhat-openshift-ecosystem/certified-operators) GitHub repository
- [ ] Publish the [release](https://github.com/NVIDIA/gpu-operator/releases)
