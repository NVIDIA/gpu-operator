#! /bin/bash

: ${TEST_CASE:="./tests/cases/defaults.sh"}
if [[ $# -ge 1 ]]; then
    TEST_CASE=${1}
    test -n "${TEST_CASE}"
fi
test -f ${PROJECT_DIR}/${TEST_CASE}

export PROJECT="gpu-operator"

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )"/scripts && pwd )"
source ${SCRIPT_DIR}/.definitions.sh
source ${SCRIPT_DIR}/.local.sh

if [[ -n "$CLEANUP" ]]; then
    echo "Running cleanup"
    ${SCRIPT_DIR}/push.sh
    remote ./tests/scripts/uninstall.sh
    exit 0
fi

# Launch a remote instance
${SCRIPT_DIR}/launch.sh

# Sync the project folder to the remote
${SCRIPT_DIR}/push.sh

# We trigger the installation of prerequisites on the remote instance
remote SKIP_PREREQUISITES="${SKIP_PREREQUISITES}" ./tests/scripts/prerequisites.sh

# We trigger the specified test case on the remote instance.
# Note: We need to ensure that the required environment variables
# are forwarded to the remote shell.
remote \
    CONTAINER_RUNTIME="${CONTAINER_RUNTIME}" \
    PROJECT="${PROJECT}" \
    OPERATOR_IMAGE="${OPERATOR_IMAGE}" \
    OPERATOR_VERSION="${OPERATOR_VERSION}" \
    VALIDATOR_IMAGE="${VALIDATOR_IMAGE}" \
    VALIDATOR_VERSION="${VALIDATOR_VERSION}" \
        ${TEST_CASE}
