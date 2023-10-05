#!/bin/bash

if [[ "${SKIP_VERIFY}" == "true" ]]; then
    echo "Skipping verify: SKIP_VERIFY=${SKIP_VERIFY}"
    exit 0
fi

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source ${SCRIPT_DIR}/.definitions.sh

# Import the check definitions
source ${SCRIPT_DIR}/checks.sh

# Check that Daemonset pods are not constantly being restarted by operator.
# We verify that the pods of the operator have come up
if [[ "${USE_NVIDIA_DRIVER_CR}" == "true" ]]; then
    check_no_driver_pod_restarts
else
    check_no_restarts "nvidia-driver-daemonset"
fi
check_no_restarts "nvidia-container-toolkit-daemonset"
check_no_restarts "nvidia-device-plugin-daemonset"
check_no_restarts "gpu-feature-discovery"
