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

if [[ $# -ne 4 ]]; then
    echo "Usage: $0 CHART_DIRECTORY VALUES_OVERRIDES_FILE OPERATOR_IMAGE OPERATOR_VERSION" >&2
    exit 1
fi

chart_directory="$1"
values_overrides_file="$2"
operator_image="$3"
operator_version="$4"

script_directory=$(dirname "${BASH_SOURCE[0]}")

if ! command -v regctl &> /dev/null; then
    echo "Error: regctl not found. Please install regctl first." >&2
    exit 1
fi

if ! command -v jq &> /dev/null; then
    echo "Error: jq not found. Please install jq first." >&2
    exit 1
fi

if ! command -v yq &> /dev/null; then
    echo "Error: yq not found. Please install yq first." >&2
    exit 1
fi

if ! command -v helm &> /dev/null; then
    echo "Error: helm not found. Please install helm first." >&2
    exit 1
fi

latest_image_lines=$("${script_directory}/get-latest-images.sh" \
    toolkit device-plugin mig-manager driver-manager vgpu-device-manager \
    kata-sandbox-device-plugin kubevirt-gpu-device-plugin cc-manager)

declare -A latest_images
while IFS='=' read -r component image; do
    latest_images[${component}]=${image}
done <<< "${latest_image_lines}"

toolkit_image=${latest_images[toolkit]}
device_plugin_image=${latest_images[device-plugin]}
mig_manager_image=${latest_images[mig-manager]}
driver_manager_image=${latest_images[driver-manager]}
vgpu_device_manager_image=${latest_images[vgpu-device-manager]}
kata_sandbox_device_plugin_image=${latest_images[kata-sandbox-device-plugin]}
kubevirt_gpu_device_plugin_image=${latest_images[kubevirt-gpu-device-plugin]}
cc_manager_image=${latest_images[cc-manager]}

"${script_directory}/generate-values-overrides.sh" "${values_overrides_file}" \
    "toolkit=${toolkit_image}" \
    "devicePlugin=${device_plugin_image}" \
    "gfd=${device_plugin_image}" \
    "migManager=${mig_manager_image}" \
    "driver.manager=${driver_manager_image}" \
    "vgpuManager.driverManager=${driver_manager_image}" \
    "vfioManager=${driver_manager_image}" \
    "vfioManager.driverManager=${driver_manager_image}" \
    "vgpuDeviceManager=${vgpu_device_manager_image}" \
    "kataSandboxDevicePlugin=${kata_sandbox_device_plugin_image}" \
    "sandboxDevicePlugin=${kubevirt_gpu_device_plugin_image}" \
    "ccManager=${cc_manager_image}" \
    "nodeStatusExporter=${operator_image}:${operator_version}" >&2

"${script_directory}/apply-values-overrides.sh" "${chart_directory}" "${values_overrides_file}" >&2

OPERATOR_REPOSITORY="${operator_image%/*}" \
OPERATOR_IMAGE_NAME="${operator_image##*/}" \
OPERATOR_VERSION="${operator_version}" \
yq -i '
    (.operator, .validator) |= (
        .repository = strenv(OPERATOR_REPOSITORY) |
        .image = strenv(OPERATOR_IMAGE_NAME) |
        .version = strenv(OPERATOR_VERSION)
    )
' "${chart_directory}/values.yaml"

printf '%s\n' \
    "${toolkit_image}" \
    "${device_plugin_image}" \
    "${mig_manager_image}" \
    "${driver_manager_image}" \
    "${vgpu_device_manager_image}" \
    "${kata_sandbox_device_plugin_image}" \
    "${kubevirt_gpu_device_plugin_image}" \
    "${cc_manager_image}"
