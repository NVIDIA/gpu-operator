#!/bin/bash

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "${SCRIPT_DIR}"/.definitions.sh

echo ""
echo ""
echo "--------------Installing the GPU Operator with NvidiaDriverCR Enabled--------------"
echo "-----------------------------------------------------------------------------------"
# Install the operator with nvidiaDriver mode set to true
OPERATOR_OPTIONS="--set driver.nvidiaDriverCRD.enabled=true" ${SCRIPT_DIR}/install-operator.sh
USE_NVIDIA_DRIVER_CR="true" "${SCRIPT_DIR}"/verify-operator.sh
USE_NVIDIA_DRIVER_CR="true" "${SCRIPT_DIR}"/verify-operand-restarts.sh

# Install a workload and verify that this works as expected
"${SCRIPT_DIR}"/install-workload.sh
"${SCRIPT_DIR}"/verify-workload.sh

echo ""
echo ""
echo "----------------------------Updating the NvidiaDriverCR----------------------------"
echo "-----------------------------------------------------------------------------------"
# Test updates of the NvidiaDriver custom resource
"${SCRIPT_DIR}"/update-nvidiadriver.sh

echo ""
echo ""
echo "--------------------------------------Teardown--------------------------------------"
echo "------------------------------------------------------------------------------------"
# Uninstall the workload and operator
"${SCRIPT_DIR}"/uninstall-workload.sh
"${SCRIPT_DIR}"/uninstall-operator.sh
