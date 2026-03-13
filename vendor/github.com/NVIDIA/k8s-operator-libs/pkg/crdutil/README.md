# CRD Management Library

A Go library for deploying and managing Custom Resource Definitions (CRDs) in Kubernetes clusters.
It supports both applying (creating/updating) and deleting CRDs from individual files or directories (searched recursively).

This package is intended to be used programmatically in Go applications. An example CLI implementation is provided in [`examples/apply-crds/`](../../examples/apply-crds/) to demonstrate usage.

## Motivation

While Helm is commonly used for managing Kubernetes resources, it has certain restrictions with CRDs:

- CRDs placed in a Helm chart's top-level `crds/` directory are installed once but not updated on upgrades or deleted on uninstalls.
- Placing CRDs in a Helm chart's `templates/` directory allows updates but can be risky since CRDs are deleted on uninstall unless protected with the `helm.sh/resource-policy: keep` annotation.

This library offers a more reliable way to manage CRDs, ensuring they are created, updated, or deleted as needed.

## Features

- **Apply and Delete CRDs**: Supports both applying (creating/updating) and deleting CRDs.
- **Flexible input**: Accepts individual YAML files or directories (or a mix of both).
- **Recursive directory search**: Automatically walks through directories to find and process all YAML files.
- **Safe update mechanism**: Checks if a CRD already exists; if so, it updates it with retry-on-conflict logic.
- **Idempotent operations**: Both apply and delete operations can be run multiple times safely.
- **Handles multiple YAML documents**: Supports files containing multiple CRD documents separated by YAML document delimiters.

## Quick Start

For usage examples, see [`examples/apply-crds/`](../../examples/apply-crds/).

## Integration Examples

### Using in Helm Hooks

You can build a custom binary using this library and deploy it as a Helm hook. Here's an example:

#### Pre-install/Pre-upgrade Hook

Apply CRDs before installation or upgrade:

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: upgrade-crd
  annotations:
    "helm.sh/hook": pre-install,pre-upgrade
    "helm.sh/hook-weight": "1"
    "helm.sh/hook-delete-policy": hook-succeeded,before-hook-creation
spec:
  template:
    metadata:
      name: upgrade-crd
    spec:
      containers:
        - name: upgrade-crd
          image: path-to-your/crd-apply-image
          imagePullPolicy: IfNotPresent
          command:
            - /apply-crds
          args:
            - --crds-path=/opt/config/crds
            - --operation=apply
```

#### Pre-delete Hook

By default, Helm does not delete CRDs when a chart is uninstalled. Use caution when deleting CRDs in a pre-delete Helm hook. Deleting a CRD also removes all associated Custom Resources (CRs), which can lead to data loss if users upgrade by uninstalling and reinstalling the chart.
