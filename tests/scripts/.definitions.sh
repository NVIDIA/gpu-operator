# !/bin/bash
set -e

[[ -z "${DEBUG}" ]] || set -x

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
TEST_DIR="$( cd "${SCRIPT_DIR}/.." && pwd )"
PROJECT_DIR="$( cd "${TEST_DIR}/.." && pwd )"
CASES_DIR="$( cd "${TEST_DIR}/cases" && pwd )"

# The terraform command is executed from the TERRAFORM_DIR
TERRAFORM_DIR=${PROJECT_DIR}/aws-kube-ci
TERRAFORM="terraform -chdir=${TERRAFORM_DIR}"

# Set default values if not defined
: ${HELM:="helm"}
: ${LOG_DIR:="/tmp/logs"}
: ${PROJECT:="$(basename "${PROJECT_DIR}")"}
: ${TEST_NAMESPACE:="test-operator"}

: ${OPERATOR_IMAGE:="nvcr.io/nvidia/gpu-operator"}

function remote() {
    ${SCRIPT_DIR}/remote.sh "cd ${PROJECT} && $@"
}
