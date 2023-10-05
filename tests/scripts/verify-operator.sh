#!/bin/bash

if [[ "${SKIP_VERIFY}" == "true" ]]; then
    echo "Skipping verify: SKIP_VERIFY=${SKIP_VERIFY}"
    exit 0
fi

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source ${SCRIPT_DIR}/.definitions.sh

# Import the check definitions
source ${SCRIPT_DIR}/checks.sh

# We verify that the pods of the operator have come up
if [[ "${USE_NVIDIA_DRIVER_CR}" == "true" ]]; then
    check_nvidia_driver_pods_ready
else
    check_pod_ready "nvidia-driver-daemonset"
fi
check_pod_ready "nvidia-container-toolkit-daemonset"
check_pod_ready "nvidia-device-plugin-daemonset"
check_pod_ready "nvidia-dcgm-exporter"
check_pod_ready "gpu-feature-discovery"
check_pod_ready "nvidia-operator-validator"
