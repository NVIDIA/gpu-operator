# !/bin/bash

if [[ "${SKIP_UNINSTALL}" == "true" ]]; then
    echo "Skipping uninstall: SKIP_UNINSTALL=${SKIP_UNINSTALL}"
    exit 0
fi

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source ${SCRIPT_DIR}/.definitions.sh

# Remove the test pod
kubectl delete pod gpu-operator-test || true