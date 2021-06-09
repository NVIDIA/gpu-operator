#! /bin/bash

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source ${SCRIPT_DIR}/.definitions.sh

# Install the operator and ensure that this works as expected
${SCRIPT_DIR}/install-operator.sh
${SCRIPT_DIR}/verify-operator.sh

# Verify the installation
${SCRIPT_DIR}/verify-operator.sh

# Install a workload and verify that this works as expected
${SCRIPT_DIR}/install-workload.sh
${SCRIPT_DIR}/verify-workload.sh

# TODO: This should be reusable
source ${SCRIPT_DIR}/checks.sh
test_restart_operator ${TEST_NAMESPACE}

# Uninstall the workload and operator
${SCRIPT_DIR}/uninstall-workload.sh
${SCRIPT_DIR}/uninstall-operator.sh
