#!/bin/bash
set -euo pipefail

if [[ "${SKIP_INSTALL}" == "true" ]]; then
    echo "Skipping install: SKIP_INSTALL=${SKIP_INSTALL}"
    exit 0
fi

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source "${SCRIPT_DIR}/.definitions.sh"

OPERATOR_REPOSITORY=$(dirname "${OPERATOR_IMAGE}")

# Determine if we should use values file approach or --set flags
USE_VALUES_FILE=false
if [[ -n "${VALUES_FILE:-}" ]]; then
	USE_VALUES_FILE=true
fi

# Build operator options conditionally
: ${OPERATOR_OPTIONS:=""}
if [[ "${USE_VALUES_FILE}" == "false" ]]; then
	# Traditional approach: build --set flags
	OPERATOR_OPTIONS="${OPERATOR_OPTIONS} --set operator.repository=${OPERATOR_REPOSITORY} --set validator.repository=${OPERATOR_REPOSITORY}"
	
	if [[ -n "${OPERATOR_VERSION}" ]]; then
		OPERATOR_OPTIONS="${OPERATOR_OPTIONS} --set operator.version=${OPERATOR_VERSION} --set validator.version=${OPERATOR_VERSION}"
	fi
	
	OPERATOR_OPTIONS="${OPERATOR_OPTIONS} --set operator.defaultRuntime=${CONTAINER_RUNTIME}"
fi

if [[ "${USE_VALUES_FILE}" == "true" ]]; then
	# Generate a temporary values file from environment variables
	# and merge it with the provided VALUES_FILE
	TEMP_ENV_VALUES=$(mktemp)
	trap 'rm -f "${TEMP_ENV_VALUES:-}" "${COMBINED_VALUES:-}"' EXIT
	${SCRIPT_DIR}/env-to-values.sh "${TEMP_ENV_VALUES}"

	# If VALUES_FILE exists, merge it with env-generated values using yq
	# Otherwise just use the env-generated values
	if [[ -f "${VALUES_FILE}" ]]; then
		echo ""
		echo "Using provided values file: ${VALUES_FILE}"
		cat "${VALUES_FILE}"
		echo ""
		echo "Merged with environment-based values:"
		cat "${TEMP_ENV_VALUES}"

		# Create a combined values file using yq for proper YAML merging
		COMBINED_VALUES=$(mktemp)
		if ! command -v yq >/dev/null 2>&1; then
			echo "Error: yq is required to merge YAML values files but was not found in PATH." >&2
			echo "Install yq: https://github.com/mikefarah/yq" >&2
			exit 1
		fi
		# yq merges YAML properly, with later files taking precedence
		yq ea '. as $item ireduce ({}; . * $item )' "${VALUES_FILE}" "${TEMP_ENV_VALUES}" > "${COMBINED_VALUES}"
		VALUES_FILE="${COMBINED_VALUES}"
	else
		VALUES_FILE="${TEMP_ENV_VALUES}"
	fi
	
	# Clear individual options since we're using values file
	TOOLKIT_CONTAINER_OPTIONS=""
	DEVICE_PLUGIN_OPTIONS=""
	MIG_MANAGER_OPTIONS=""
else
	# Traditional approach: use --set flags for backward compatibility
	: ${TOOLKIT_CONTAINER_OPTIONS:=""}
	if [[ -n "${TOOLKIT_CONTAINER_IMAGE:-}" ]]; then
		TOOLKIT_CONTAINER_OPTIONS="${TOOLKIT_CONTAINER_OPTIONS} --set toolkit.repository=\"\" --set toolkit.version=\"\" --set toolkit.image=\"${TOOLKIT_CONTAINER_IMAGE}\""
	fi

	: ${DEVICE_PLUGIN_OPTIONS:=""}
	if [[ -n "${DEVICE_PLUGIN_IMAGE:-}" ]]; then
		DEVICE_PLUGIN_OPTIONS="${DEVICE_PLUGIN_OPTIONS} --set devicePlugin.repository=\"\" --set devicePlugin.version=\"\" --set devicePlugin.image=\"${DEVICE_PLUGIN_IMAGE}\""
	fi

	: ${MIG_MANAGER_OPTIONS:=""}
	if [[ -n "${MIG_MANAGER_IMAGE:-}" ]]; then
		MIG_MANAGER_OPTIONS="${MIG_MANAGER_OPTIONS} --set migManager.repository=\"\" --set migManager.version=\"\" --set migManager.image=\"${MIG_MANAGER_IMAGE}\""
	fi
fi

# Create the test namespace
kubectl create namespace "${TEST_NAMESPACE}"

# Create k8s secret for pulling vgpu images from nvcr.io
if [[ "${GPU_MODE}" == "vgpu" ]]; then
	: ${REGISTRY_SECRET_NAME:="nvcrio-registry"}
	: ${PRIVATE_REGISTRY:="nvcr.io/ea-cnt/nv_only"}

	# Only add to OPERATOR_OPTIONS if not using values file approach
	if [[ "${USE_VALUES_FILE}" == "false" ]]; then
		OPERATOR_OPTIONS="${OPERATOR_OPTIONS} --set driver.repository=${PRIVATE_REGISTRY} --set driver.image=vgpu-guest-driver --set driver.version=${TARGET_DRIVER_VERSION} --set driver.imagePullSecrets={${REGISTRY_SECRET_NAME}}"
	fi
	# Note: When USE_VALUES_FILE=true, vGPU config is handled by env-to-values.sh

	kubectl create secret docker-registry ${REGISTRY_SECRET_NAME} \
		--docker-server=${PRIVATE_REGISTRY} \
		--docker-username='$oauthtoken' \
		--docker-password=${NGC_API_KEY} \
		-n "${TEST_NAMESPACE}"
fi

# Run the helm install command
echo ""
echo "Installing GPU Operator with Helm..."
echo "Operator image: ${OPERATOR_IMAGE}:${OPERATOR_VERSION}"

if [[ "${USE_VALUES_FILE}" == "true" ]]; then
	echo "Using values file approach: ${VALUES_FILE}"
	${HELM} install ${PROJECT_DIR}/deployments/gpu-operator --generate-name \
		-n "${TEST_NAMESPACE}" \
		-f "${VALUES_FILE}" \
		--wait
else
	echo "Using --set flags approach"
	${HELM} install ${PROJECT_DIR}/deployments/gpu-operator --generate-name \
		-n "${TEST_NAMESPACE}" \
		${OPERATOR_OPTIONS} \
		${TOOLKIT_CONTAINER_OPTIONS} \
		${DEVICE_PLUGIN_OPTIONS} \
		${MIG_MANAGER_OPTIONS} \
		--wait
fi
