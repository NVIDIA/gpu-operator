# !/bin/bash

if [[ "${SKIP_INSTALL}" == "true" ]]; then
    echo "Skipping install: SKIP_INSTALL=${SKIP_INSTALL}"
    exit 0
fi

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source ${SCRIPT_DIR}/.definitions.sh

OPERATOR_REPOSITORY=$(dirname ${OPERATOR_IMAGE})

: ${OPERATOR_OPTIONS:=""}
OPERATOR_OPTIONS="${OPERATOR_OPTIONS} --set operator.repository=${OPERATOR_REPOSITORY}"

if [[ -n "${OPERATOR_VERSION}" ]]; then
OPERATOR_OPTIONS="${OPERATOR_OPTIONS} --set operator.version=${OPERATOR_VERSION}"
fi

# We set up the options for the toolkit container
: ${TOOLKIT_CONTAINER_OPTIONS:=""}

if [[ -n "${TOOLKIT_CONTAINER_IMAGE}" ]]; then
TOOLKIT_CONTAINER_OPTIONS="${TOOLKIT_CONTAINER_OPTIONS} --set toolkit.repository=\"\" --set toolkit.version=\"\" --set toolkit.image=\"${TOOLKIT_CONTAINER_IMAGE}\""
fi

# Create the test namespace
kubectl create namespace "${TEST_NAMESPACE}"

# Run the helm install command
${HELM} install ${PROJECT_DIR}/deployments/gpu-operator --generate-name \
	-n "${TEST_NAMESPACE}" \
	${OPERATOR_OPTIONS} \
	${TOOLKIT_CONTAINER_OPTIONS} \
		--wait
