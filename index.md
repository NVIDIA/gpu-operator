# NVIDIA GPU Operator

Welcome to the NVIDIA GPU Operator Github Helm Chart repository.
For more information, refer to our [official documentation](https://docs.nvidia.com/datacenter/cloud-native/gpu-operator/overview.html).

## Quickstart
```sh
# Install helm
$ curl -fsSL -o get_helm.sh https://raw.githubusercontent.com/helm/helm/master/scripts/get-helm-3 \
   && chmod 700 get_helm.sh \
   && ./get_helm.sh

# Add the NVIDIA helm repository
$ helm repo add nvidia https://nvidia.github.io/gpu-operator
$ helm repo update

$ helm install --wait --generate-name \
     -n gpu-operator --create-namespace \
     nvidia/gpu-operator
```
