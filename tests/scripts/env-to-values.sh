#!/usr/bin/env bash

# Copyright NVIDIA CORPORATION
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -euo pipefail

# Usage: env-to-values.sh OUTPUT_FILE
#
# Converts environment variables to GPU Operator Helm values YAML format.
# This script reads common test environment variables and generates a
# values file that can be used with `helm install -f values.yaml`.
#
# Supported environment variables:
#   - OPERATOR_IMAGE: operator image path (repository will be extracted)
#   - OPERATOR_VERSION: operator version
#   - TOOLKIT_CONTAINER_IMAGE: container-toolkit image override
#   - DEVICE_PLUGIN_IMAGE: device-plugin image override
#   - MIG_MANAGER_IMAGE: mig-manager image override
#   - CONTAINER_RUNTIME: default runtime (docker, containerd, crio)

if [[ $# -ne 1 ]]; then
    echo "Usage: $0 OUTPUT_FILE" >&2
    echo "" >&2
    echo "Converts environment variables to GPU Operator Helm values format." >&2
    exit 1
fi

OUTPUT_FILE="$1"

# Start with header
cat > "${OUTPUT_FILE}" <<EOF
# Generated from environment variables by env-to-values.sh
# Date: $(date -u +"%Y-%m-%d %H:%M:%S UTC")
#
# This file contains GPU Operator configuration derived from test
# environment variables.

EOF

HAS_VALUES=false

# Build operator configuration block
OPERATOR_CONFIG=""
VALIDATOR_CONFIG=""

# Extract repository from OPERATOR_IMAGE if provided
if [[ -n "${OPERATOR_IMAGE:-}" ]]; then
    OPERATOR_REPOSITORY=$(dirname "${OPERATOR_IMAGE}")
    OPERATOR_CONFIG="${OPERATOR_CONFIG}  repository: \"${OPERATOR_REPOSITORY}\"\n"
    VALIDATOR_CONFIG="${VALIDATOR_CONFIG}  repository: \"${OPERATOR_REPOSITORY}\"\n"
    echo "Added operator.repository: ${OPERATOR_REPOSITORY}"
    HAS_VALUES=true
fi

if [[ -n "${OPERATOR_VERSION:-}" ]]; then
    OPERATOR_CONFIG="${OPERATOR_CONFIG}  version: \"${OPERATOR_VERSION}\"\n"
    VALIDATOR_CONFIG="${VALIDATOR_CONFIG}  version: \"${OPERATOR_VERSION}\"\n"
    echo "Added operator.version: ${OPERATOR_VERSION}"
    HAS_VALUES=true
fi

if [[ -n "${CONTAINER_RUNTIME:-}" ]]; then
    OPERATOR_CONFIG="${OPERATOR_CONFIG}  defaultRuntime: \"${CONTAINER_RUNTIME}\"\n"
    echo "Added operator.defaultRuntime: ${CONTAINER_RUNTIME}"
    HAS_VALUES=true
fi

# Write operator configuration if any
if [[ -n "${OPERATOR_CONFIG}" ]]; then
    echo "operator:" >> "${OUTPUT_FILE}"
    echo -e "${OPERATOR_CONFIG}" >> "${OUTPUT_FILE}"
fi

# Write validator configuration if any
if [[ -n "${VALIDATOR_CONFIG}" ]]; then
    echo "validator:" >> "${OUTPUT_FILE}"
    echo -e "${VALIDATOR_CONFIG}" >> "${OUTPUT_FILE}"
fi

# Container Toolkit configuration
if [[ -n "${TOOLKIT_CONTAINER_IMAGE:-}" ]]; then
    cat >> "${OUTPUT_FILE}" <<EOF
toolkit:
  repository: ""
  version: ""
  image: "${TOOLKIT_CONTAINER_IMAGE}"

EOF
    HAS_VALUES=true
    echo "Added toolkit.image: ${TOOLKIT_CONTAINER_IMAGE}"
fi

# Device Plugin configuration
if [[ -n "${DEVICE_PLUGIN_IMAGE:-}" ]]; then
    cat >> "${OUTPUT_FILE}" <<EOF
devicePlugin:
  repository: ""
  version: ""
  image: "${DEVICE_PLUGIN_IMAGE}"

EOF
    HAS_VALUES=true
    echo "Added devicePlugin.image: ${DEVICE_PLUGIN_IMAGE}"
fi

# MIG Manager configuration
if [[ -n "${MIG_MANAGER_IMAGE:-}" ]]; then
    cat >> "${OUTPUT_FILE}" <<EOF
migManager:
  repository: ""
  version: ""
  image: "${MIG_MANAGER_IMAGE}"

EOF
    HAS_VALUES=true
    echo "Added migManager.image: ${MIG_MANAGER_IMAGE}"
fi

# vGPU Driver configuration
if [[ -n "${GPU_MODE:-}" && "${GPU_MODE}" == "vgpu" ]]; then
    cat >> "${OUTPUT_FILE}" <<EOF
driver:
  repository: "${PRIVATE_REGISTRY:-nvcr.io/ea-cnt/nv_only}"
  image: "vgpu-guest-driver"
  version: "${TARGET_DRIVER_VERSION:-}"
  imagePullSecrets:
    - "${REGISTRY_SECRET_NAME:-nvcrio-registry}"

EOF
    HAS_VALUES=true
    echo "Added driver (vGPU) configuration"
fi

if [[ "${HAS_VALUES}" == "false" ]]; then
    echo "Warning: No environment variables found to convert to values" >&2
    echo "# No values to override" >> "${OUTPUT_FILE}"
fi

echo ""
echo "Generated values file: ${OUTPUT_FILE}"
echo ""
echo "=== File Contents ==="
cat "${OUTPUT_FILE}"
