#!/bin/bash

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "${SCRIPT_DIR}"/.definitions.sh

# Install the operator and ensure that this works as expected
echo ""
echo ""
echo "--------------Installing the GPU Operator------------------------------------------"
echo "-----------------------------------------------------------------------------------"
"${SCRIPT_DIR}"/install-operator.sh
"${SCRIPT_DIR}"/verify-operator.sh
"${SCRIPT_DIR}"/verify-operand-restarts.sh

echo ""
echo ""
echo "--------------Install GPU Test Workload--------------------------------------------"
echo "-----------------------------------------------------------------------------------"
# Install a workload and verify that this works as expected
"${SCRIPT_DIR}"/install-workload.sh
"${SCRIPT_DIR}"/verify-workload.sh

echo ""
echo ""
echo "--------------Clusterpolicy Update Tests--------------------------------------------"
echo "------------------------------------------------------------------------------------"
# Test updates through ClusterPolicy
"${SCRIPT_DIR}"/update-clusterpolicy.sh

echo ""
echo ""
echo "--------------GPU Operator Restart Test---------------------------------------------"
echo "------------------------------------------------------------------------------------"
# TODO: This should be reusable
source "${SCRIPT_DIR}"/checks.sh
test_restart_operator ${TEST_NAMESPACE} ${CONTAINER_RUNTIME}

# Disable operands and verify that this works as expected
"${SCRIPT_DIR}"/disable-operands.sh
"${SCRIPT_DIR}"/verify-disable-operands.sh

# Enable operands and verify that this works as expected
"${SCRIPT_DIR}"/enable-operands.sh
"${SCRIPT_DIR}"/verify-operator.sh

# Uninstall the workload and operator
"${SCRIPT_DIR}"/uninstall-workload.sh
"${SCRIPT_DIR}"/uninstall-operator.sh

echo ""
echo ""
echo "--------------Sandbox Workload Test-------------------------------------------------"
echo "------------------------------------------------------------------------------------"
echo ""
echo "NOTE: Reinstalling the GPU Operator Helm chart with Sandbox Workloads enabled"
# Install the operator with sandboxed functionality enabled and confirm container workloads operate as expected
OPERATOR_OPTIONS="--set sandboxWorkloads.enabled=true --set sandboxWorkloads.defaultWorkload=container" "${SCRIPT_DIR}"/install-operator.sh
"${SCRIPT_DIR}"/verify-operator.sh

# Install a workload and verify that this works as expected
"${SCRIPT_DIR}"/install-workload.sh
"${SCRIPT_DIR}"/verify-workload.sh

# Uninstall the workload and operator
"${SCRIPT_DIR}"/uninstall-workload.sh
"${SCRIPT_DIR}"/uninstall-operator.sh
