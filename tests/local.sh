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

if [[ -n "$CLEANUP" ]]; then
    echo "Running cleanup"
    ${SCRIPT_DIR}/sync.sh
    remote ./tests/scripts/uninstall.sh
    exit 0
fi

# Launch a remote instance
${SCRIPT_DIR}/launch.sh

# Sync the project folder to the remote
${SCRIPT_DIR}/sync.sh

# We trigger the installation of prerequisites on the remote instance
remote ./tests/scripts/prerequisites.sh

# We trigger the specified test case on the remote instance
remote ${TEST_CASE}
