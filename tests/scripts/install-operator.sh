#!/bin/bash

if [[ "${SKIP_INSTALL}" == "true" ]]; then
    echo "Skipping install: SKIP_INSTALL=${SKIP_INSTALL}"
    exit 0
fi

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source ${SCRIPT_DIR}/.definitions.sh

OPERATOR_REPOSITORY=$(dirname ${OPERATOR_IMAGE})

: ${OPERATOR_OPTIONS:=""}
OPERATOR_OPTIONS="${OPERATOR_OPTIONS} --set operator.repository=${OPERATOR_REPOSITORY} --set validator.repository=${OPERATOR_REPOSITORY}"

if [[ -n "${OPERATOR_VERSION}" ]]; then
OPERATOR_OPTIONS="${OPERATOR_OPTIONS} --set operator.version=${OPERATOR_VERSION} --set validator.version=${OPERATOR_VERSION}"
fi

OPERATOR_OPTIONS="${OPERATOR_OPTIONS} --set operator.defaultRuntime=${CONTAINER_RUNTIME}"

# We set up the options for the toolkit container
: ${TOOLKIT_CONTAINER_OPTIONS:=""}

if [[ -n "${TOOLKIT_CONTAINER_IMAGE}" ]]; then
TOOLKIT_CONTAINER_OPTIONS="${TOOLKIT_CONTAINER_OPTIONS} --set toolkit.repository=\"\" --set toolkit.version=\"\" --set toolkit.image=\"${TOOLKIT_CONTAINER_IMAGE}\""
fi

# Create the test namespace
kubectl create namespace "${TEST_NAMESPACE}"

# Create k8s secret for pulling vgpu images from nvcr.io 
if [[ "${GPU_MODE}" == "vgpu" ]]; then

	: ${REGISTRY_SECRET_NAME:="nvcrio-registry"}
	: ${PRIVATE_REGISTRY:="nvcr.io/ea-cnt/nv_only"}
	OPERATOR_OPTIONS="${OPERATOR_OPTIONS} --set driver.repository=${PRIVATE_REGISTRY} --set driver.image=vgpu-guest-driver --set driver.version=${TARGET_DRIVER_VERSION} --set driver.imagePullSecrets={${REGISTRY_SECRET_NAME}}"
	
	kubectl create secret docker-registry ${REGISTRY_SECRET_NAME} \
		--docker-server=${PRIVATE_REGISTRY} \
		--docker-username='$oauthtoken' \
		--docker-password=${NGC_API_KEY} \
		-n "${TEST_NAMESPACE}"
fi

# Run the helm install command
${HELM} install ${PROJECT_DIR}/deployments/gpu-operator --generate-name \
	-n "${TEST_NAMESPACE}" \
	${OPERATOR_OPTIONS} \
	${TOOLKIT_CONTAINER_OPTIONS} \
		--wait
