#!/bin/bash

if [[ "${SKIP_VERIFY}" == "true" ]]; then
    echo "Skipping verify: SKIP_VERIFY=${SKIP_VERIFY}"
    exit 0
fi

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source ${SCRIPT_DIR}/.definitions.sh

# Import the check definitions
source ${SCRIPT_DIR}/checks.sh

# We verify that all GPU Operator operands have been deleted
check_pod_deleted "nvidia-driver-daemonset"
check_pod_deleted "nvidia-container-toolkit-daemonset"
check_pod_deleted "nvidia-device-plugin-daemonset"
check_pod_deleted "nvidia-dcgm-exporter"
check_pod_deleted "gpu-feature-discovery"
check_pod_deleted "nvidia-operator-validator"

