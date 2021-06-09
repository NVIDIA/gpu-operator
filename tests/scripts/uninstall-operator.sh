# !/bin/bash

if [[ "${SKIP_UNINSTALL}" == "true" ]]; then
    echo "Skipping uninstall: SKIP_UNINSTALL=${SKIP_UNINSTALL}"
    exit 0
fi

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source ${SCRIPT_DIR}/.definitions.sh

OPERATOR_NAME=$(${HELM} list -n ${TEST_NAMESPACE} | grep gpu-operator | awk '{print $1}')

# Run the helm install command
[[ -z ${OPERATOR_NAME} ]] || ${HELM} uninstall -n ${TEST_NAMESPACE} ${OPERATOR_NAME}

# Remove the namespace
kubectl delete namespace ${TEST_NAMESPACE} || true
