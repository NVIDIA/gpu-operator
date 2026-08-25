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
OPERATOR_TAG="${1:-${GITHUB_REF_NAME:-}}"
UPSTREAM_REPOSITORY="${2:-https://github.com/NVIDIA/gpu-operator.git}"

if [[ ! "${OPERATOR_TAG}" =~ ^v([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
    printf 'ERROR: operator tag %q does not match vX.Y.Z\n' "${OPERATOR_TAG}" >&2
    exit 1
fi

operator_major="${BASH_REMATCH[1]}"
operator_minor="$((10#${BASH_REMATCH[2]}))"
operator_patch="${BASH_REMATCH[3]}"
printf -v expected_api_version 'v0.%s%02d.%s' \
    "${operator_major}" "${operator_minor}" "${operator_patch}"

required_api_version="$(go list -m -f '{{.Version}}' "${API_MODULE}")"
if [[ "${required_api_version}" != "${expected_api_version}" ]]; then
    printf 'ERROR: %s maps to %s, but go.mod requires %s\n' \
        "${OPERATOR_TAG}" "${expected_api_version}" "${required_api_version}" >&2
    exit 1
fi

operator_commit="$(git rev-parse HEAD)"
operator_tag_commit="$(git rev-parse "${OPERATOR_TAG}^{commit}")"
if [[ "${operator_tag_commit}" != "${operator_commit}" ]]; then
    printf 'ERROR: %s points to %s, but HEAD is %s\n' \
        "${OPERATOR_TAG}" "${operator_tag_commit}" "${operator_commit}" >&2
    exit 1
fi

api_tag="api/${expected_api_version}"
tag_refs="$(git ls-remote --tags "${UPSTREAM_REPOSITORY}" \
    "refs/tags/${api_tag}" "refs/tags/${api_tag}^{}")"
if [[ -z "${tag_refs}" ]]; then
    printf 'ERROR: API tag %s does not exist in %s\n' \
        "${api_tag}" "${UPSTREAM_REPOSITORY}" >&2
    exit 1
fi

api_commit="$(awk '
    /\^\{\}$/ {
        print $1
        found = 1
        exit
    }
    !found {
        commit = $1
    }
    END {
        if (!found) print commit
    }
' <<< "${tag_refs}")"
if [[ "${api_commit}" != "${operator_commit}" ]]; then
    printf 'ERROR: %s points to %s, but %s points to %s\n' \
        "${api_tag}" "${api_commit}" "${OPERATOR_TAG}" "${operator_commit}" >&2
    exit 1
fi

tmpdir="$(mktemp -d)"
trap 'rm -rf "${tmpdir}"' EXIT

(
    cd "${tmpdir}"
    go mod init module-validation >/dev/null
    resolved_version="$(
        GOPROXY=direct GONOSUMDB="${API_MODULE}" \
            go list -m -f '{{.Version}}' "${API_MODULE}@${expected_api_version}"
    )"
    if [[ "${resolved_version}" != "${expected_api_version}" ]]; then
        printf 'ERROR: downloaded API module resolved to %s instead of %s\n' \
            "${resolved_version}" "${expected_api_version}" >&2
        exit 1
    fi
)

binary="${tmpdir}/gpu-operator"
CGO_ENABLED=0 GOOS=linux go build -o "${binary}" ./cmd/gpu-operator
metadata="$(go version -m "${binary}")"

if ! awk -v module="${API_MODULE}" -v version="${expected_api_version}" '
    $1 == "dep" && $2 == module && $3 == version {
        found = 1
    }
    END {
        exit !found
    }
' <<< "${metadata}"; then
    printf 'ERROR: operator binary does not record %s %s\n%s\n' \
        "${API_MODULE}" "${expected_api_version}" "${metadata}" >&2
    exit 1
fi

if ! grep -Fq "vcs.revision=${operator_commit}" <<< "${metadata}"; then
    printf 'ERROR: operator binary does not record release commit %s\n%s\n' \
        "${operator_commit}" "${metadata}" >&2
    exit 1
fi

printf '%s and %s are published from %s with valid module metadata\n' \
    "${OPERATOR_TAG}" "${api_tag}" "${operator_commit}"
