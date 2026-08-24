#!/usr/bin/env bash
# Copyright (c) NVIDIA CORPORATION.  All rights reserved.
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
#
# Resolves and verifies the upstream URL of every license file the notices
# document links, writing tools/license-urls.tsv.
#
# Needs network; run via 'make third-party-notices-urls'. A URL is written ONLY
# if the bytes it serves hash to the same sha256 as the vendored copy, so no
# entry can be a dead link or point at the wrong licence.
#
# Scope is the shipped set: this sources the notices generator and runs its
# collection stages, so it verifies exactly the packages go-licenses attributes
# to ./cmd/..., never the build- and test-only modules vendor/ also contains.

set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=tools/license-url-lib.sh
source "${HERE}/license-url-lib.sh"
# shellcheck source=tools/generate-third-party-notices.sh
source "${HERE}/generate-third-party-notices.sh"

REPOS_MAP="${REPOS_MAP:-tools/module-repos.tsv}"
URLS_OUTPUT="${URLS_OUTPUT:-tools/license-urls.tsv}"
PROXY="${PROXY:-https://proxy.golang.org}"

sha256_of_file() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | cut -d' ' -f1
    else
        shasum -a 256 "$1" | cut -d' ' -f1
    fi
}

sha256_of_stdin() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum | cut -d' ' -f1
    else
        shasum -a 256 | cut -d' ' -f1
    fi
}

remote_sha() {
    local blob="$1" raw
    raw="$(raw_url_for "${blob}")"
    [[ -n "${raw}" ]] || return 1
    if raw_is_base64 "${blob}"; then
        curl -sfL --max-time 30 "${raw}" 2>/dev/null | base64_decode 2>/dev/null | sha256_of_stdin
    else
        curl -sfL --max-time 30 "${raw}" 2>/dev/null | sha256_of_stdin
    fi
}

repo_field() {
    LC_ALL=C awk -F'\t' -v m="$1" -v want="$2" \
        '$1 == m { print (want == "repo" ? $2 : $3); found = 1; exit }
         END { exit !found }' "${REPOS_MAP}"
}

# Version-specific provenance. Fetched here rather than stored in the repos map,
# which is deliberately version-independent. Origin.Hash is the only pinned ref
# left when upstream deletes or rewrites a tag.
origin_ref_and_hash() {
    local module="$1" version="$2" info
    info="$(curl -sfL --max-time 30 \
        "${PROXY}/$(proxy_escape "${module}")/@v/${version}.info" 2>/dev/null)" || return 0
    printf '%s' "${info}" | python3 -c '
import json, sys
try:
    origin = json.load(sys.stdin).get("Origin") or {}
except Exception:
    origin = {}
ref = origin.get("Ref", "")
# Only a tag pins a release. Every golang.org/x module reports
# refs/heads/master, and a branch ref would float.
print(ref[len("refs/tags/"):] if ref.startswith("refs/tags/") else "")
print(origin.get("Hash", ""))
' 2>/dev/null || printf '\n\n'
}

main() {
    command -v curl >/dev/null 2>&1 || die "curl is not installed."
    command -v python3 >/dev/null 2>&1 || die "python3 is not installed."
    [[ -f "${REPOS_MAP}" ]] \
        || die "${REPOS_MAP} not found — run 'make third-party-notices-repos' first."

    check_prerequisites
    verify_platform_matrix
    prepare_workspace
    collect_licenses
    build_indexes

    local tmp failures=0
    tmp="$(mktemp "${TMPDIR:-/tmp}/gpu-operator-urls.XXXXXX")"

    local package _ license module version repo subdir relative
    local origin_tag origin_hash plain pseudo lf name path_in_module want_sha found
    while IFS=, read -r package _ license module version; do
        [[ -z "${package}" ]] && continue

        repo="$(repo_field "${module}" repo)" \
            || die "${REPOS_MAP} has no entry for ${module}." \
                   "Run 'make third-party-notices-repos' and commit the result."
        subdir="$(repo_field "${module}" subdir)" || subdir=""

        origin_tag="$(origin_ref_and_hash "${module}" "${version}" | sed -n 1p)"
        origin_hash="$(origin_ref_and_hash "${module}" "${version}" | sed -n 2p)"

        # Ref candidates, most specific first. Never a branch ref. Commit
        # hashes (pseudo-version hash, Origin.Hash) are tracked apart from tag
        # names: go.googlesource.com serves a tag under refs/tags/ but a raw
        # commit only under its bare hash, so qualifying a hash the same way
        # 404s a pseudo-versioned module such as google.golang.org/protobuf.
        local tag_refs=() hash_refs=()
        plain="$(normalize_version "${version}")"
        pseudo="$(pseudo_version_hash "${version}")"
        [[ -n "${origin_tag}" ]] && tag_refs+=( "${origin_tag}" )
        if [[ -n "${pseudo}" ]]; then
            hash_refs+=( "${pseudo}" )
        else
            [[ -n "${subdir}" ]] && tag_refs+=( "${subdir}/${plain}" )
            tag_refs+=( "${plain}" )
        fi
        [[ -n "${origin_hash}" ]] && hash_refs+=( "${origin_hash}" )

        local refs=() r
        if (( ${#tag_refs[@]} > 0 )); then
            case "${repo}" in
                https://go.googlesource.com/*)
                    for r in "${tag_refs[@]}"; do refs+=( "refs/tags/${r}" ); done
                    ;;
                *)
                    refs+=( "${tag_refs[@]}" )
                    ;;
            esac
        fi
        (( ${#hash_refs[@]} > 0 )) && refs+=( "${hash_refs[@]}" )
        (( ${#refs[@]} > 0 )) || die "no ref candidates for ${module}@${version}."

        relative="$(license_dir_within_module "${package}" "${module}")" \
            || die "no license file found for ${package} under ${VENDOR_DIR}/${module}."

        while IFS= read -r lf; do
            [[ -z "${lf}" ]] && continue
            name="$(basename "${lf}")"
            path_in_module="${relative:+${relative}/}${name}"
            [[ -f "${VENDOR_DIR}/${module}/${path_in_module}" ]] \
                || die "${VENDOR_DIR}/${module}/${path_in_module} does not exist."
            want_sha="$(sha256_of_file "${VENDOR_DIR}/${module}/${path_in_module}")"

            # Both layouts: a submodule may ship its own licence or inherit the
            # repository root's. Content decides which is real. Built as an
            # array rather than an unquoted ${x:+...} expansion, which would
            # word-split a path containing whitespace or a glob character.
            local paths=()
            [[ -n "${subdir}" ]] && paths+=( "${subdir}/${path_in_module}" )
            paths+=( "${path_in_module}" )

            found=""
            local try_ref try_path candidate
            for try_ref in "${refs[@]}"; do
                for try_path in "${paths[@]}"; do
                    candidate="$(blob_url "${repo}" "${try_ref}" "${try_path}")"
                    if [[ "$(remote_sha "${candidate}")" == "${want_sha}" ]]; then
                        found="${candidate}"
                        break 2
                    fi
                done
            done

            if [[ -z "${found}" ]]; then
                log "UNVERIFIED ${module}@${version} ${path_in_module}"
                failures=$(( failures + 1 ))
                continue
            fi
            printf '%s\t%s\t%s\t%s\n' "${module}" "${version}" "${path_in_module}" "${found}" >> "${tmp}"
        done < <(license_files_for "${LICENSES_DIR}/${package}")
    done < "${INDEX_FILE}"

    (( failures == 0 )) || die \
        "${failures} license file(s) could not be matched to a verified upstream URL." \
        "Every URL must serve bytes identical to the vendored copy; none of the" \
        "candidates did. The repository mapping may be stale — re-run" \
        "'make third-party-notices-repos' before investigating further."

    {
        printf '# Verified upstream URL for every license file the notices document links.\n'
        printf '# Generated by tools/verify-license-urls.sh. Each URL was fetched and its\n'
        printf '# sha256 matched against the vendored copy, so no entry is a dead or wrong link.\n'
        printf '# Covers the shipped set only: build- and test-only dependencies are excluded.\n'
        printf '# module\tversion\tlicense-path\turl\n'
        LC_ALL=C sort -u "${tmp}"
    } > "${URLS_OUTPUT}"
    rm -f "${tmp}"

    log "Wrote ${URLS_OUTPUT} ($(LC_ALL=C grep -vc '^#' "${URLS_OUTPUT}") verified URLs)"
    exit 0
}

if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    main "$@"
fi
