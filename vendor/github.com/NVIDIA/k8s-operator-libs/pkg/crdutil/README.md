# CRD Apply Tool

This tool is designed to help deploy and manage Custom Resource Definitions (CRDs) in a Kubernetes cluster.
It applies all CRDs found in specified directories, providing a solution to some of the limitations of Helm when it comes to managing CRDs.

## Motivation

While Helm is commonly used for managing Kubernetes resources, it has certain restrictions with CRDs:

- CRDs placed in Helm's top-level `crds/` directory are not updated on upgrades or rollbacks.
- Placing CRDs in Helm’s `templates/` directory is not entirely safe, as deletions and upgrades of CRDs are not always handled properly.

This tool offers a more reliable way to apply CRDs, ensuring they are created or updated as needed.

## Features

- **Apply CRDs from multiple directories**: Allows specifying multiple directories containing CRD YAML manifests.
- **Recursive directory search**: Walks through each specified directory to find and apply all YAML files.
- **Safe update mechanism**: Checks if a CRD already exists; if so, it updates it with the latest version.
- **Handles multiple YAML documents**: Supports files containing multiple CRD documents separated by YAML document delimiters.

## Usage

Compile and run the tool by providing the `-crds-dir` flag with paths to the directories containing the CRD YAML files:

```bash
go build -o crd-apply-tool
./crd-apply-tool -crds-dir /path/to/crds1 -crds-dir /path/to/crds2
```

In a Helm pre-install hook it can look like:

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
              - --crds-dir=/crds/operator
```

> Note: the image must contain all your CRDs in e.g. the `/crds/operator` directory.

## Flags

- `-crds-dir` (required): Specifies a directory path that contains the CRD manifests in YAML format. This flag can be provided multiple times to apply CRDs from multiple directories.
