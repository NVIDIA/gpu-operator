#!/bin/bash

if [[ "${SKIP_VERIFY}" == "true" ]]; then
    echo "Skipping verify: SKIP_VERIFY=${SKIP_VERIFY}"
    exit 0
fi

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source ${SCRIPT_DIR}/.definitions.sh

# Import the check definitions
source ${SCRIPT_DIR}/checks.sh

check_gpu_pod_ready ${LOG_DIR}
