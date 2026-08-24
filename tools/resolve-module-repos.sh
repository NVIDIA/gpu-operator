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

# Resolves every module in vendor/modules.txt to its upstream repository and
# writes tools/module-repos.tsv.
#
# Needs network; run via 'make third-party-notices-repos'. Keyed by module and
# not by version: a repository normally does not move when a dependency is
# bumped, so this file survives bumps and changes only when a new module enters
# the tree. That is a convenience, not a guarantee — Task 5's content
# verification is what actually enforces correctness, and it fails loudly if a
# mapping has gone stale.

set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=tools/license-url-lib.sh
source "${HERE}/license-url-lib.sh"

MODULES_TXT="${MODULES_TXT:-vendor/modules.txt}"
OUTPUT="${OUTPUT:-tools/module-repos.tsv}"
PROXY="${PROXY:-https://proxy.golang.org}"

die() {
    printf 'ERROR: %s\n' "$1" >&2
    shift
    (( $# > 0 )) && printf '%s\n' "$@" >&2
    exit 1
}
log() { printf '%s\n' "$*" >&2; }

# Retries cover genuine network flakiness only. Absence of Origin is a real,
# permanent property of older proxy cache entries, not a transient failure.
fetch_retry() {
    local url="$1" attempt body
    for attempt in 1 2 3; do
        body="$(curl -sfL --max-time 30 "${url}" 2>/dev/null)" || body=""
        [[ -n "${body}" ]] && { printf '%s' "${body}"; return 0; }
        sleep $(( attempt * 2 ))
    done
    return 1
}

origin_field() {
    printf '%s' "$1" | python3 -c '
import json, sys
try:
    origin = json.load(sys.stdin).get("Origin") or {}
except Exception:
    origin = {}
print(origin.get(sys.argv[1], ""))
' "$2" 2>/dev/null || printf ''
}

# go-import content is "<import-prefix> <vcs> <repo-root>". The meta tag is
# frequently split across lines, so newlines are folded before matching.
go_import_meta() {
    fetch_retry "https://$1?go-get=1" 2>/dev/null \
        | tr '\n' ' ' | tr -s ' ' \
        | LC_ALL=C grep -oE 'name="go-import"[^>]*content="[^"]*"' \
        | head -1 \
        | LC_ALL=C sed -E 's/.*content="([^"]*)".*/\1/'
}

main() {
    command -v curl >/dev/null 2>&1 || die "curl is not installed."
    command -v python3 >/dev/null 2>&1 || die "python3 is not installed."
    [[ -f "${MODULES_TXT}" ]] \
        || die "${MODULES_TXT} not found — run 'make third-party-notices-repos' from the repo root."

    local tmp unresolved=0
    tmp="$(mktemp "${TMPDIR:-/tmp}/gpu-operator-repos.XXXXXX")"
    trap 'rm -f "${tmp}"' EXIT

    local module version info repo prefix subdir meta converted
    while read -r module version; do
        [[ -z "${module}" ]] && continue

        repo=""; prefix=""; subdir=""; info=""

        if info="$(fetch_retry "${PROXY}/$(proxy_escape "${module}")/@v/${version}.info")"; then
            repo="$(origin_field "${info}" URL)"
            subdir="$(origin_field "${info}" Subdir)"
        fi

        if [[ -z "${repo}" ]]; then
            meta="$(go_import_meta "${module}")" || meta=""
            if [[ -n "${meta}" ]]; then
                prefix="$(printf '%s' "${meta}" | awk '{print $1}')"
                repo="$(printf '%s' "${meta}" | awk '{print $3}')"
            fi
        fi

        [[ -z "${repo}" ]] && repo="$(github_repo_from_path "${module}")"
        repo="$(normalize_repo_url "${repo}")"

        # gopkg.in points at itself and serves no blobs.
        case "${repo}" in
            https://gopkg.in/*|"")
                converted="$(gopkg_in_repo "${module}")"
                [[ -n "${converted}" ]] && repo="${converted}"
                ;;
        esac

        if [[ -z "${repo}" ]]; then
            log "UNRESOLVED ${module}: no repository could be determined"
            unresolved=$(( unresolved + 1 ))
            continue
        fi

        # Subdir precedence: proxy Origin, then the go-import prefix, then the
        # github path shape. The last matters because GitHub serves no
        # go-import, so a github submodule with no Origin would otherwise lose
        # its tag prefix and never verify.
        if [[ -z "${subdir}" && -n "${prefix}" ]]; then
            subdir="$(derived_subdir "${module}" "${prefix}")"
        fi
        if [[ -z "${subdir}" ]]; then
            subdir="$(github_subdir_from_path "${module}")"
        fi

        printf '%s\t%s\t%s\n' "${module}" "${repo}" "${subdir}" >> "${tmp}"
    done < <(LC_ALL=C grep '^# ' "${MODULES_TXT}" | awk '{print $2, $3}')

    (( unresolved == 0 )) || die \
        "${unresolved} module(s) could not be resolved to a repository." \
        "Re-run; if the failure persists the module's vanity host is unreachable."

    {
        printf '# Upstream repository for each vendored module.\n'
        printf '# Generated by tools/resolve-module-repos.sh from the module proxy Origin,\n'
        printf '# the go-import meta tag, and the github.com path shape. Not hand-edited.\n'
        printf '# module\trepo-url\tsubdir\n'
        LC_ALL=C sort "${tmp}"
    } > "${OUTPUT}"

    log "Wrote ${OUTPUT} ($(LC_ALL=C grep -vc '^#' "${OUTPUT}") modules)"

    # Exit here, not by falling off the end: the EXIT trap above references
    # tmp, a variable local to this function. If main merely returns, the
    # process's implicit exit fires that trap after tmp has gone out of
    # scope, and 'set -u' turns the cleanup itself into an unbound-variable
    # failure that clobbers this function's success with exit 1.
    exit 0
}

if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    main "$@"
fi
