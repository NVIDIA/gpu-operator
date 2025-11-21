# Developing GPU Operator with Tilt

This repository includes a `Tiltfile` to automate the development workflow using [Tilt](https://tilt.dev/). This setup handles building the GPU Operator image, pushing it to a registry, and deploying it to a Kubernetes cluster (local or remote).

## Prerequisites

*   [Tilt](https://docs.tilt.dev/install.html) (v0.30.0+)
*   [Docker](https://docs.docker.com/get-docker/)
*   [Kubectl](https://kubernetes.io/docs/tasks/tools/) configured for your target cluster.
*   [Helm](https://helm.sh/docs/intro/install/) (v3+)

## Configuration

You need to configure the container registry where images will be pushed. You can do this via a `tilt_config.json` file or command-line arguments.

### Option 1: `tilt_config.json` (Recommended)

Create a file named `tilt_config.json` in the root of the repository:

```json
{
  "registry": "docker.io/yourusername",
  "registry_username": "yourusername",
  "registry_password": "yourpassword",
  "namespace": "gpu-operator",
  "container_runtime": "docker"
}
```

*   **registry**: The registry URL (e.g., `docker.io/user`, `quay.io/user`).
*   **registry_username/password**: (Optional) Credentials for private registries. If provided, a Kubernetes secret `registry-secret` will be created and used.
*   **namespace**: Target namespace (default: `gpu-operator`).
*   **container_runtime**: Runtime for the operator (default: `docker`).

### Option 2: Command Line Arguments

You can pass these values directly via `make tilt`:

```bash
make tilt REGISTRY=docker.io/yourusername REGISTRY_USERNAME=... REGISTRY_PASSWORD=...
```

## Usage

To start Tilt (deploy):

```bash
make tilt
# OR
make tilt-up
```

To tear down (delete resources):

```bash
make tilt-down
```

You can pass arguments to both:

```bash
make tilt-up REGISTRY=docker.io/yourusername
make tilt-down NAMESPACE=my-namespace
```

This will:
1.  **Sync CRDs**: Run `make sync-crds` to ensure CRDs are up-to-date.
2.  **Create Namespace**: Create the target namespace if it doesn't exist.
3.  **Create Secrets**: Create `registry-secret` if credentials are provided.
4.  **Build & Push**: Build the GPU Operator image and push it to your registry.
5.  **Deploy**: Install the Helm chart with the new image.

## How it Works

### Dual Tagging Strategy

The GPU Operator deployment consists of two main parts that need the image:
1.  **The Operator Deployment**: Managed by Tilt. Tilt builds the image, tags it with a unique hash (e.g., `tilt-12345`), and updates the Deployment manifest to use this tag. This ensures fast, immutable updates.
2.  **The Validator Pod**: Managed by the Operator itself (via `ClusterPolicy`). The Operator creates this Pod dynamically.

**The Challenge**: Tilt cannot easily update the image tag inside the `ClusterPolicy` Custom Resource because it's a static configuration value, not a standard Kubernetes image field.

**The Solution**:
*   We use a `custom_build` script in the `Tiltfile`.
*   It builds the image once.
*   It pushes **two tags**:
    1.  The **Tilt Hash** (e.g., `repo/image:tilt-hash`): Used by the Deployment.
    2.  The **Git Commit** (e.g., `repo/image:a1b2c3d`): Used by the Validator.
*   We configure Helm to set `validator.version` to the git commit hash.

This ensures that when the Operator tries to spawn the Validator using `repo/image:git_commit`, the image is guaranteed to exist in the registry, matching the code you just built.

## Troubleshooting

### "push access denied"
Ensure you are logged in to your registry locally (`docker login`) or provide `REGISTRY_USERNAME` and `REGISTRY_PASSWORD` in `tilt_config.json`.

### Validator ImagePullBackOff
Check if the `git_commit` tag exists in your registry. The `custom_build` step should have pushed it. Ensure your `REGISTRY` variable is correct (e.g., `docker.io/user`, not `docker.io/user/repo`).
