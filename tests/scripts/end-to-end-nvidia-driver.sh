#!/bin/bash

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "${SCRIPT_DIR}"/.definitions.sh

test_nvidiadriver_helm_render_options() {
    local render_file
    render_file=$(mktemp)

    ${HELM} template gpu-operator "${PROJECT_DIR}/deployments/gpu-operator" \
        -n "${TEST_NAMESPACE}" \
        --set driver.nvidiaDriverCRD.enabled=true \
        --set driver.nvidiaDriverCRD.deployDefaultCR=true > "${render_file}"
    grep -q "kind: NVIDIADriver" "${render_file}"
    grep -q "default: true" "${render_file}"

    ${HELM} template gpu-operator "${PROJECT_DIR}/deployments/gpu-operator" \
        -n "${TEST_NAMESPACE}" \
        --set driver.nvidiaDriverCRD.enabled=true \
        --set driver.nvidiaDriverCRD.deployDefaultCR=false > "${render_file}"
    if grep -q "kind: NVIDIADriver" "${render_file}"; then
        echo "NVIDIADriver rendered when driver.nvidiaDriverCRD.deployDefaultCR=false"
        exit 1
    fi

    ${HELM} template gpu-operator "${PROJECT_DIR}/deployments/gpu-operator" \
        -n "${TEST_NAMESPACE}" \
        --set driver.nvidiaDriverCRD.enabled=false \
        --set driver.nvidiaDriverCRD.deployDefaultCR=true > "${render_file}"
    if grep -q "kind: NVIDIADriver" "${render_file}"; then
        echo "NVIDIADriver rendered when driver.nvidiaDriverCRD.enabled=false"
        exit 1
    fi

    rm -f "${render_file}"
}

echo ""
echo ""
echo "------------------------Checking NvidiaDriver Helm Rendering------------------------"
echo "-----------------------------------------------------------------------------------"
test_nvidiadriver_helm_render_options

echo ""
echo ""
echo "--------------Installing the GPU Operator with NvidiaDriverCR Enabled--------------"
echo "-----------------------------------------------------------------------------------"
# Install the operator with nvidiaDriver mode set to true
OPERATOR_OPTIONS="${OPERATOR_OPTIONS:-} --set driver.nvidiaDriverCRD.enabled=true --set driver.nvidiaDriverCRD.deployDefaultCR=true" ${SCRIPT_DIR}/install-operator.sh
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
