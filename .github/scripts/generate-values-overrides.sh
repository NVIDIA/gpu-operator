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

if [[ $# -lt 2 ]]; then
    echo "Usage: $0 OUTPUT_FILE VALUES_PATH=IMAGE [VALUES_PATH=IMAGE ...]" >&2
    echo "" >&2
    echo "Example:" >&2
    echo "  $0 values-overrides.yaml \\" >&2
    echo "    toolkit=ghcr.io/nvidia/container-toolkit:3e112b4a \\" >&2
    echo "    driver.manager=ghcr.io/nvidia/k8s-driver-manager:357e757f" >&2
    exit 1
fi

output_file="$1"
shift

: > "${output_file}"

for image_override in "$@"; do
    if [[ "${image_override}" != *=* ]]; then
        echo "Error: '${image_override}' is not in VALUES_PATH=IMAGE form" >&2
        exit 1
    fi

    values_path="${image_override%%=*}"
    image_reference="${image_override#*=}"

    if [[ ! "${values_path}" =~ ^[A-Za-z][A-Za-z0-9]*(\.[A-Za-z][A-Za-z0-9]*)*$ ]]; then
        echo "Error: invalid values path '${values_path}'" >&2
        exit 1
    fi

    if [[ ! "${image_reference}" =~ ^(.+)/([^/:@]+):([^/:@]+)$ ]]; then
        echo "Error: image '${image_reference}' for '${values_path}' is not in REPOSITORY/IMAGE:TAG form" >&2
        exit 1
    fi

    VALUES_PATH=".${values_path}" \
    IMAGE_REPOSITORY="${BASH_REMATCH[1]}" \
    IMAGE_NAME="${BASH_REMATCH[2]}" \
    IMAGE_VERSION="${BASH_REMATCH[3]}" \
    yq -i '
        eval(strenv(VALUES_PATH) + ".repository") = strenv(IMAGE_REPOSITORY) |
        eval(strenv(VALUES_PATH) + ".image") = strenv(IMAGE_NAME) |
        eval(strenv(VALUES_PATH) + ".version") = strenv(IMAGE_VERSION) |
        eval(strenv(VALUES_PATH) + ".version") style="double"
    ' "${output_file}"
done

echo "Generated values override file: ${output_file}"
echo ""
echo "=== File Contents ==="
cat "${output_file}"
