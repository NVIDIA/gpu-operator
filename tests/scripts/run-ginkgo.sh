#!/bin/bash
set -euo pipefail

# Usage: run-ginkgo.sh LABEL_FILTER
#
# Runs the Ginkgo e2e suite in tests/e2e against the cluster on this host.
# The test binary is built on the CI runner with
#   go test -c -o tests/e2e/gpu-operator-e2e.test ./tests/e2e
# and synced here with the rest of the project, because the test host has no Go toolchain.

if [[ $# -ne 1 ]]; then
    echo "Usage: $0 LABEL_FILTER" >&2
    exit 1
fi
LABEL_FILTER="${1}"

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}"/.definitions.sh

E2E_TEST_BINARY="${TEST_DIR}/e2e/gpu-operator-e2e.test"
if [[ ! -x "${E2E_TEST_BINARY}" ]]; then
    echo "${E2E_TEST_BINARY} not found; build it with: go test -c -o ${E2E_TEST_BINARY} ./tests/e2e" >&2
    exit 1
fi

mkdir -p "${LOG_DIR}"

OPERATOR_REPOSITORY=$(dirname "${OPERATOR_IMAGE}")
OPERATOR_IMAGE_NAME=$(basename "${OPERATOR_IMAGE}")

# Ginkgo's own timeout bounds the run; the go test default of 10 minutes would cut it short.
"${E2E_TEST_BINARY}" \
    -test.timeout=0 \
    -ginkgo.timeout=80m \
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
