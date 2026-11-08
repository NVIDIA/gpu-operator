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

readonly IMAGE_REFERENCE_EXPRESSION='eval(strenv(VALUES_PATH)) | (.repository // "") + "/" + (.image // "") + ":" + (.version // "")'

if [[ $# -ne 2 ]]; then
    echo "Usage: $0 CHART_DIRECTORY VALUES_OVERRIDES_FILE" >&2
    exit 1
fi

chart_directory="$1"
values_overrides_file="$2"
chart_values_file="${chart_directory}/values.yaml"
script_directory=$(dirname "${BASH_SOURCE[0]}")

override_path_list=$(yq '.. | select(tag == "!!map" and has("image")) | path | join(".")' "${values_overrides_file}")
if [[ -z "${override_path_list}" ]]; then
    echo "Error: ${values_overrides_file} has no image overrides" >&2
    exit 1
fi
mapfile -t override_paths <<< "${override_path_list}"

missing_path_count=0
for override_path in "${override_paths[@]}"; do
    if ! VALUES_PATH=".${override_path}" yq -e 'eval(strenv(VALUES_PATH)) | has("image")' "${chart_values_file}" &> /dev/null; then
        echo "Error: ${override_path}.image is not a value in ${chart_values_file}" >&2
        missing_path_count=$((missing_path_count + 1))
    fi
done
if [[ ${missing_path_count} -gt 0 ]]; then
    exit 1
fi

python3 "${script_directory}/merge-yaml.py" \
    --base "${chart_values_file}" \
    --override "${values_overrides_file}" \
    --output "${chart_values_file}"

rendered_cluster_policy_spec=$(helm template gpu-operator "${chart_directory}" | yq 'select(.kind == "ClusterPolicy") | .spec')

mismatched_path_count=0
for override_path in "${override_paths[@]}"; do
    expected_image_reference=$(VALUES_PATH=".${override_path}" yq "${IMAGE_REFERENCE_EXPRESSION}" "${values_overrides_file}")
    rendered_image_reference=$(VALUES_PATH=".${override_path}" yq "${IMAGE_REFERENCE_EXPRESSION}" <<< "${rendered_cluster_policy_spec}")
    if [[ "${rendered_image_reference}" != "${expected_image_reference}" ]]; then
        echo "Error: ${override_path} renders as '${rendered_image_reference}' in the ClusterPolicy, expected '${expected_image_reference}'" >&2
        mismatched_path_count=$((mismatched_path_count + 1))
    fi
done
if [[ ${mismatched_path_count} -gt 0 ]]; then
    exit 1
fi

echo "Applied ${#override_paths[@]} image overrides to ${chart_values_file}"
