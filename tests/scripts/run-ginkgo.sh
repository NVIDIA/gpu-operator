#!/bin/bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
    cat >&2 <<EOF
Usage: $0 LABEL_FILTER

Runs the Ginkgo e2e specs whose labels match LABEL_FILTER.

Examples:
  $0 clusterpolicy
  $0 nvidia-driver
EOF
    exit 1
fi
LABEL_FILTER="${1}"

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}"/.definitions.sh

if [[ "${GPU_MODE}" == "vgpu" ]]; then
    echo "The Ginkgo e2e suite does not set up the vGPU guest driver; run the bash test cases for GPU_MODE=vgpu" >&2
    exit 1
fi

E2E_TEST_BINARY="${TEST_DIR}/e2e/gpu-operator-e2e.test"
if [[ ! -x "${E2E_TEST_BINARY}" ]]; then
    echo "${E2E_TEST_BINARY} not found; build it with: go test -c -o ${E2E_TEST_BINARY} ./tests/e2e" >&2
    exit 1
fi

mkdir -p "${LOG_DIR}"

OPERATOR_REPOSITORY=$(dirname "${OPERATOR_IMAGE}")
OPERATOR_IMAGE_NAME=$(basename "${OPERATOR_IMAGE}")

"${E2E_TEST_BINARY}" \
    -test.timeout=0 \
    -ginkgo.timeout=65m \
    -ginkgo.fail-on-empty \
    -ginkgo.v \
    -ginkgo.label-filter="${LABEL_FILTER}" \
    -ginkgo.junit-report="${LOG_DIR}/ginkgo-${LABEL_FILTER}-junit.xml" \
    -kubeconfig="${KUBECONFIG:-${HOME}/.kube/config}" \
    -namespace="${TEST_NAMESPACE}" \
    -helm-chart="${CHART_REFERENCE}" \
    -helm-chart-version="${HELM_CHART_VERSION:-}" \
    -values-file="${VALUES_FILE:-}" \
    -operator-repository="${OPERATOR_REPOSITORY}" \
    -operator-image="${OPERATOR_IMAGE_NAME}" \
    -operator-version="${OPERATOR_VERSION}" \
    -container-runtime="${CONTAINER_RUNTIME}" \
    -target-driver-version="${TARGET_DRIVER_VERSION}" \
    -log-dir="${LOG_DIR}"
