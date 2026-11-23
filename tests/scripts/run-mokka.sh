#!/bin/bash
set -euo pipefail

# Runs the Ginkgo e2e suite on a kind cluster whose nodes Mokka (nvml-mock) makes
# appear to have NVIDIA GPUs, using an operator image built from this checkout.
# Specs that need a driver container or CUDA must carry the driver-container or
# cuda-workload label so the default filter excludes them.

: "${MOKKA_CLUSTER_NAME:=gpu-operator-mokka}"
: "${MOKKA_KUBECONFIG:=${TMPDIR:-/tmp}/${MOKKA_CLUSTER_NAME}.kubeconfig}"
: "${KIND_NODE_IMAGE:=}"
# Must not overlap the service CIDR of any Kubernetes cluster running on the host (kind and
# kubeadm both default to 10.96.0.0/12): the host's kube-proxy rules would capture the kind
# pods' traffic to the API server.
: "${MOKKA_SERVICE_SUBNET:=10.200.0.0/16}"
: "${MOKKA_CHART:=oci://ghcr.io/nvidia/k8s-test-infra/chart/nvml-mock}"
: "${MOKKA_VERSION:=0.6.0}"
: "${MOKKA_NAMESPACE:=mokka}"
: "${MOKKA_GPU_PROFILE:=t4}"
: "${MOKKA_IMAGE_NAME:=localhost/gpu-operator}"
: "${MOKKA_IMAGE_TAG:=mokka-dev}"
: "${MOKKA_SKIP_BUILD:=false}"
: "${MOKKA_LABEL_FILTER:=(clusterpolicy || nvidia-driver) && !cuda-workload && !driver-container}"

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_DIR="$( cd "${SCRIPT_DIR}/../.." && pwd )"
VALUES_FILE="${PROJECT_DIR}/tests/e2e/mokka/gpu-operator-values.yaml"
: "${HELM_CHART:=${PROJECT_DIR}/deployments/gpu-operator}"
: "${HELM_CHART_VERSION:=}"

missing_tools=()
for tool in docker kind helm kubectl go; do
    command -v "${tool}" >/dev/null || missing_tools+=("${tool}")
done
if [[ ${#missing_tools[@]} -gt 0 ]]; then
    echo "Missing required tools: ${missing_tools[*]}" >&2
    exit 1
fi
if ! docker info >/dev/null 2>&1; then
    echo "Cannot reach the Docker daemon; add ${USER:-your user} to the docker group or start Docker" >&2
    exit 1
fi

# kube-proxy in the kind node fails with "too many open files" below this, leaving pods
# unable to reach the API server.
if [[ "$(cat /proc/sys/fs/inotify/max_user_instances 2>/dev/null || echo 8192)" -lt 512 ]]; then
    echo "fs.inotify.max_user_instances is too low for kind; run:" >&2
    echo "  sudo sysctl fs.inotify.max_user_instances=8192 fs.inotify.max_user_watches=524288" >&2
    exit 1
fi

if kind get clusters | grep -qx "${MOKKA_CLUSTER_NAME}"; then
    echo "Reusing kind cluster ${MOKKA_CLUSTER_NAME}"
    kind export kubeconfig --name "${MOKKA_CLUSTER_NAME}" --kubeconfig "${MOKKA_KUBECONFIG}"
else
    kind create cluster --name "${MOKKA_CLUSTER_NAME}" --kubeconfig "${MOKKA_KUBECONFIG}" \
        ${KIND_NODE_IMAGE:+--image "${KIND_NODE_IMAGE}"} --config - <<EOF
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
networking:
  serviceSubnet: "${MOKKA_SERVICE_SUBNET}"
EOF
fi
export KUBECONFIG="${MOKKA_KUBECONFIG}"

OPERATOR_IMAGE="${MOKKA_IMAGE_NAME}:${MOKKA_IMAGE_TAG}"
if [[ "${MOKKA_SKIP_BUILD}" != "true" ]]; then
    NODE_ARCH="$(kubectl get nodes -o jsonpath='{.items[0].status.nodeInfo.architecture}')"
    make -C "${PROJECT_DIR}" build-image IMAGE_NAME="${MOKKA_IMAGE_NAME}" IMAGE_TAG="${MOKKA_IMAGE_TAG}" \
        DOCKER_BUILD_PLATFORM_OPTIONS="--platform=linux/${NODE_ARCH}"
fi
kind load docker-image --name "${MOKKA_CLUSTER_NAME}" "${OPERATOR_IMAGE}"

helm upgrade --install nvml-mock "${MOKKA_CHART}" \
    --version "${MOKKA_VERSION}" \
    --namespace "${MOKKA_NAMESPACE}" --create-namespace \
    --set gpu.profile="${MOKKA_GPU_PROFILE}" \
    --wait --timeout 300s

helm show crds "${HELM_CHART}" ${HELM_CHART_VERSION:+--version "${HELM_CHART_VERSION}"} |
    kubectl apply --server-side --force-conflicts -f -

make -C "${PROJECT_DIR}/tests/e2e" e2e-test \
    KUBECONFIG="${KUBECONFIG}" \
    HELM_CHART="${HELM_CHART}" \
    HELM_CHART_VERSION="${HELM_CHART_VERSION}" \
    OPERATOR_REPOSITORY="$(dirname "${MOKKA_IMAGE_NAME}")" \
    OPERATOR_IMAGE="$(basename "${MOKKA_IMAGE_NAME}")" \
    OPERATOR_VERSION="${MOKKA_IMAGE_TAG}" \
    VALUES_FILE="${VALUES_FILE}" \
    GINKGO_LABEL_FILTER="${MOKKA_LABEL_FILTER}"
