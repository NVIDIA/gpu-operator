# !/bin/bash

if [[ "${SKIP_VERIY}" == "true" ]]; then
    echo "Skipping verify: SKIP_VERIY=${SKIP_VERIY}"
    exit 0
fi

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source ${SCRIPT_DIR}/.definitions.sh

# Import the check definitions
source ${SCRIPT_DIR}/checks.sh

# Check that Daemonset pods are not constantly being restarted by operator.
check_no_restarts "nvidia-driver-daemonset"
check_no_restarts "nvidia-container-toolkit-daemonset"
check_no_restarts "nvidia-device-plugin-daemonset"
check_no_restarts "gpu-feature-discovery"
