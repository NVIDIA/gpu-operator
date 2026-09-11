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

export LC_ALL=C

ROOT_MODFILE="${1:-go.mod}"
API_MODFILE="${2:-api/go.mod}"

for modfile in "${ROOT_MODFILE}" "${API_MODFILE}"; do
    if [[ ! -f "${modfile}" ]]; then
        printf 'ERROR: module file %s does not exist\n' "${modfile}" >&2
        exit 1
    fi
done

tmpdir="$(mktemp -d)"
trap 'rm -rf "${tmpdir}"' EXIT

direct_requirements() {
    awk '
        $1 == "require" && $2 == "(" {
            in_require = 1
            next
        }
        in_require && $1 == ")" {
            in_require = 0
            next
        }
        in_require && $0 !~ /\/\/ indirect/ {
            print $1, $2
            next
        }
        $1 == "require" && $2 != "(" && $0 !~ /\/\/ indirect/ {
            print $2, $3
        }
    ' "$1" | LC_ALL=C sort
}

direct_requirements "${ROOT_MODFILE}" > "${tmpdir}/root"
direct_requirements "${API_MODFILE}" > "${tmpdir}/api"
join "${tmpdir}/root" "${tmpdir}/api" > "${tmpdir}/shared"

shared_count=0
mismatch_count=0
while read -r module root_version api_version; do
    [[ -z "${module}" ]] && continue
    shared_count=$((shared_count + 1))
    if [[ "${root_version}" != "${api_version}" ]]; then
        printf 'ERROR: %s has different direct dependency versions: root=%s api=%s\n' \
            "${module}" "${root_version}" "${api_version}" >&2
        mismatch_count=$((mismatch_count + 1))
    fi
done < "${tmpdir}/shared"

if (( shared_count == 0 )); then
    printf 'ERROR: root and API modules have no shared direct dependencies\n' >&2
    exit 1
fi

if (( mismatch_count > 0 )); then
    printf 'ERROR: %d shared direct dependency version(s) are out of sync\n' \
        "${mismatch_count}" >&2
    exit 1
fi

printf 'All %d shared direct dependencies are in sync\n' "${shared_count}"
