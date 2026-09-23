#!/usr/bin/env bash
# Copyright NVIDIA CORPORATION
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -euo pipefail

API_MODULE="github.com/NVIDIA/gpu-operator/api"
UPSTREAM_REPOSITORY="${1:-https://github.com/NVIDIA/gpu-operator.git}"

api_version="$(go list -m -f '{{.Version}}' "${API_MODULE}")"
if [[ ! "${api_version}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-].*)?$ ]]; then
    printf 'ERROR: %s has invalid version %q in go.mod\n' \
        "${API_MODULE}" "${api_version}" >&2
    exit 1
fi

api_tag="api/${api_version}"
if output="$(git ls-remote --exit-code --tags "${UPSTREAM_REPOSITORY}" "refs/tags/${api_tag}" 2>&1)"; then
    printf 'ERROR: release PR references existing API tag %s\n' "${api_tag}" >&2
    [[ -n "${output}" ]] && printf '%s\n' "${output}" >&2
    exit 1
else
    status=$?
    if (( status != 2 )); then
        printf 'ERROR: could not query %s for tag %s\n%s\n' \
            "${UPSTREAM_REPOSITORY}" "${api_tag}" "${output}" >&2
        exit "${status}"
    fi
fi

printf 'API version %s is available for release\n' "${api_version}"
