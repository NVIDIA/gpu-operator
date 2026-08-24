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
# Writes THIRD_PARTY_NOTICES.md for the Go modules linked into ./cmd/... .

set -euo pipefail

OUTPUT="${OUTPUT:-THIRD_PARTY_NOTICES.md}"
LICENSES_DIR="${LICENSES_DIR:-.licenses-cache}"
MULTI_ARCH_MK="${MULTI_ARCH_MK:-multi-arch.mk}"
MODULES_TXT="${MODULES_TXT:-vendor/modules.txt}"

PACKAGES=("./cmd/...")

PLATFORMS=(
    "linux/amd64"
    "linux/arm64"
)

# LC_ALL=C on every sort and grep below: collation order and case folding are
# locale-dependent, so without it the generated document varies by host.

die() {
    printf 'ERROR: %s\n' "$1" >&2
    shift
    if (( $# > 0 )); then
        printf '%s\n' "$@" >&2
    fi
    exit 1
}

log() {
    printf '%s\n' "$*" >&2
}

# Licenses that are themselves Markdown close a fixed ``` fence early and invert
# every block after it, so open with one backtick more than the file's longest run.
fence_for() {
    local file="$1" longest width
    # -a: a license holding a NUL byte would otherwise print "Binary file ...
    # matches" instead of the matches, on stdout or stderr depending on the grep.
    longest=$(LC_ALL=C grep -oaE '`+' "${file}" 2>/dev/null \
        | awk '{ if (length($0) > m) m = length($0) } END { print m+0 }' || true)
    width=$(( longest + 1 ))
    (( width < 3 )) && width=3
    printf '%*s' "${width}" '' | tr ' ' '`'
}

check_prerequisites() {
    command -v go >/dev/null 2>&1 || die "go is not installed."

    # Probe by running it, not with -x: a host-built binary bind-mounted into a
    # Linux container passes -x but cannot exec.
    if ./bin/go-licenses --help >/dev/null 2>&1; then
        GO_LICENSES="${PWD}/bin/go-licenses"
    elif command -v go-licenses >/dev/null 2>&1; then
        GO_LICENSES="$(command -v go-licenses)"
    else
        die "go-licenses is not installed." "Install it with 'make install-tools'."
    fi

    local f
    for f in "${MULTI_ARCH_MK}" "${MODULES_TXT}"; do
        [[ -f "${f}" ]] \
            || die "${f} not found — run 'make third-party-notices' from the repo root."
    done

    LOCAL_MODULE=$(go list -m 2>/dev/null || true)
    [[ -n "${LOCAL_MODULE}" ]] || die "could not determine local module path via 'go list -m'."

    # vendor/ keeps this offline. CGO off matches the released binaries and lets
    # go-licenses cross-list without a C toolchain.
    export GOFLAGS="-mod=vendor"
    export CGO_ENABLED=0
}

verify_platform_matrix() {
    local expected actual
    expected=$(LC_ALL=C sed -n \
        's/^DOCKER_BUILD_PLATFORM_OPTIONS[[:space:]]*?*=[[:space:]]*--platform=//p' \
        "${MULTI_ARCH_MK}" | tr ',' '\n' | LC_ALL=C sed '/^$/d' | LC_ALL=C sort -u)
    [[ -n "${expected}" ]] \
        || die "could not read DOCKER_BUILD_PLATFORM_OPTIONS from ${MULTI_ARCH_MK}."

    actual=$(printf '%s\n' "${PLATFORMS[@]}" | LC_ALL=C sort -u)
    [[ "${expected}" == "${actual}" ]] || die \
        "the PLATFORMS matrix is out of sync with ${MULTI_ARCH_MK}." \
        "Update the PLATFORMS array in tools/generate-third-party-notices.sh to match the released targets." \
        "  matrix (PLATFORMS): $(echo "${actual}" | paste -sd ' ' -)" \
        "  image platforms:    $(echo "${expected}" | paste -sd ' ' -)"
}

prepare_workspace() {
    # An override of '', '/', '.' or '..' would make the rm -rf below fatal.
    case "${LICENSES_DIR}" in
        ""|"/"|"."|"..")
            die "refusing to 'rm -rf' unsafe LICENSES_DIR='${LICENSES_DIR}'."
            ;;
    esac
    rm -rf "${LICENSES_DIR}"
    mkdir -p "${LICENSES_DIR}"

    # Explicit templates: macOS mktemp ignores TMPDIR without one.
    local t="${TMPDIR:-/tmp}/gpu-operator-notices"
    SAVE_ROOT="$(mktemp -d "${t}.XXXXXX")"
    COMBINED_CSV="$(mktemp "${t}-csv.XXXXXX")"
    INDEX_FILE="$(mktemp "${t}-idx.XXXXXX")"

    # Composed beside its destination, not under TMPDIR, so the last step is a
    # same-filesystem rename(2) rather than a copy-then-unlink.
    local out_dir
    out_dir="$(dirname "${OUTPUT}")"
    mkdir -p "${out_dir}"
    OUT_TMP="$(mktemp "${out_dir}/.$(basename "${OUTPUT}").XXXXXX")"

    trap 'rm -rf "${SAVE_ROOT}"; rm -f "${COMBINED_CSV}" "${INDEX_FILE}" "${OUT_TMP}"' EXIT
}

collect_licenses() {
    local platform goos goarch save_dir

    for platform in "${PLATFORMS[@]}"; do
        goos="${platform%/*}"
        goarch="${platform#*/}"
        log "Collecting licenses for ${goos}/${goarch}..."

        save_dir="${SAVE_ROOT}/${goos}_${goarch}"

        # Only the local module: --ignore matches plain string prefixes, not
        # path segments, so a stdlib list's bare "go" would silently drop
        # golang.org/x/*, google.golang.org/* and gopkg.in/*.
        GOOS="${goos}" GOARCH="${goarch}" "${GO_LICENSES}" save "${PACKAGES[@]}" \
            --save_path="${save_dir}" \
            --force \
            --ignore="${LOCAL_MODULE}"

        GOOS="${goos}" GOARCH="${goarch}" "${GO_LICENSES}" csv "${PACKAGES[@]}" \
            --ignore="${LOCAL_MODULE}" \
            >> "${COMBINED_CSV}"

        merge_licenses "${save_dir}" "${LICENSES_DIR}"
    done
}

# Module cache files are 0444 and cp preserves that, so restore write
# permission or the next platform's copy fails.
merge_licenses() {
    cp -R "$1/." "$2/"
    chmod -R u+w "$2"
}

# One row per package, joining licenses rather than picking one: go-licenses
# emits a row per recognized license, so key-only dedup would hide
# filepath-securejoin's MPL-2.0 behind its BSD-3-Clause.
collapse_index() {
    LC_ALL=C sort -u "$1" | awk -F, '
        {
            pkg = $1
            if (!(pkg in url)) { url[pkg] = $2; order[++n] = pkg }
            if (!((pkg SUBSEP $3) in seen)) {
                seen[pkg SUBSEP $3] = 1
                # Count, do not test "pkg in lic": mawk instantiates the
                # assignment target before evaluating the right-hand side.
                lic[pkg] = (cnt[pkg]++ ? lic[pkg] " / " : "") $3
            }
        }
        END { for (i = 1; i <= n; i++) print order[i] "," url[order[i]] "," lic[order[i]] }
    '
}

# Rows carry the module path and version from modules.txt rather than a URL:
# in vendor mode go-licenses reports a URL into this repo at HEAD, which stops
# describing released content once main moves and names our copy, not
# upstream. The verified upstream location comes from tools/license-urls.tsv.
# Longest prefix wins: a license may sit below the module root.
annotate_modules() {
    awk -v modfile="${MODULES_TXT}" '
        BEGIN {
            FS = OFS = ","
            while ((getline line < modfile) > 0) {
                if (line !~ /^# /) continue
                split(line, f, " ")
                # A "=>" replacement is what is actually vendored, so it is what
                # the file must name; a filesystem replace has no version.
                if (f[4] == "=>" || f[3] == "=>") {
                    r = (f[4] == "=>") ? 5 : 4
                    if (f[r + 1] == "") {
                        print "ERROR: " modfile " replaces " f[2] " with a local path;" > "/dev/stderr"
                        print "teach tools/generate-third-party-notices.sh how to attribute it." > "/dev/stderr"
                        exit 1
                    }
                    mods[++m] = f[2]
                    disp[f[2]] = f[r]
                    ver[f[2]] = f[r + 1]
                } else {
                    mods[++m] = f[2]
                    disp[f[2]] = f[2]
                    ver[f[2]] = f[3]
                }
            }
            close(modfile)
            # A read error makes getline return -1 and the loop never run.
            if (m == 0) {
                print "ERROR: no module lines read from " modfile > "/dev/stderr"
                exit 1
            }
        }
        {
            best = ""
            for (i = 1; i <= m; i++) {
                mp = mods[i]
                if (($1 == mp || index($1, mp "/") == 1) && length(mp) > length(best)) best = mp
            }
            print $0, (best == "" ? "unknown" : disp[best]), (best == "" ? "unknown" : ver[best])
        }
    '
}

build_indexes() {
    log "Generating dependency index..."
    collapse_index "${COMBINED_CSV}" | annotate_modules > "${INDEX_FILE}"

    [[ -s "${INDEX_FILE}" ]] \
        || die "go-licenses produced no entries for ${PACKAGES[*]} — refusing to write empty notices file."

    if cut -d, -f4 "${INDEX_FILE}" | LC_ALL=C grep -qx 'unknown'; then
        die "could not resolve module@version for some packages from ${MODULES_TXT}." \
            "Run 'go mod vendor' and re-run, rather than committing a file with unattributed entries."
    fi

    if cut -d, -f5 "${INDEX_FILE}" | LC_ALL=C grep -qx 'unknown'; then
        die "could not resolve a version for some packages from ${MODULES_TXT}." \
            "Run 'go mod vendor' and re-run, rather than committing a file with unattributed entries."
    fi

    # go-licenses reports a license it cannot classify as "Unknown" and exits 0.
    # Anchored both sides: licenses are joined with " / ", and an identifier
    # merely starting with "Unknown" must not match.
    # An empty field would also render as "Unknown" via the :- fallback below, so
    # catch it here rather than letting it reach the table.
    if cut -d, -f3 "${INDEX_FILE}" | LC_ALL=C grep -qE '^$|(^| / )Unknown( / |$)'; then
        die "go-licenses could not identify a license for some dependencies." \
            "Check the entries reported as Unknown before committing the file."
    fi
}

# License-bearing files, sorted. Filter by name: for restricted licenses
# 'go-licenses save' copies the whole module source, which does not belong here.
license_files_for() {
    local dir="$1" f
    [[ -d "${dir}" ]] || return 0
    while IFS= read -r -d '' f; do
        if printf '%s' "$(basename "${f}")" \
            | LC_ALL=C grep -qiE '^(licen[cs]e|notice|copying|copyright|authors|patents)([-._].*)?$'; then
            printf '%s\n' "${f}"
        fi
    done < <(find "${dir}" -maxdepth 1 -type f -print0 2>/dev/null | LC_ALL=C sort -z)
}

LICENSE_URLS="${LICENSE_URLS:-tools/license-urls.tsv}"
VENDOR_DIR="${VENDOR_DIR:-vendor}"

# Separate from check_prerequisites: tools/verify-license-urls.sh reuses the
# collection stages to discover which license files the document will link, and
# it is the command that produces this map, so it must run without it.
require_url_map() {
    [[ -f "${LICENSE_URLS}" ]] \
        || die "${LICENSE_URLS} not found." \
               "Run 'make third-party-notices-urls' (needs network) and commit the result."
}

# The directory whose license files govern PACKAGE, relative to MODULE. Walks
# up from the package to the module root and takes the first directory holding
# a license file, which is how go-licenses attributes them.
license_dir_within_module() {
    local module="$2" dir="$1" relative
    while :; do
        if [[ -n "$(license_files_for "${VENDOR_DIR}/${dir}")" ]]; then
            relative="${dir#"${module}"}"
            printf '%s' "${relative#/}"
            return 0
        fi
        [[ "${dir}" == "${module}" ]] && return 1
        [[ "${dir}" != */* ]] && return 1
        dir="${dir%/*}"
    done
}

location_for() {
    local url
    url="$(LC_ALL=C awk -F'\t' -v m="$1" -v v="$2" -v p="$3" \
        '$1 == m && $2 == v && $3 == p { print $4; found = 1; exit }
         END { exit !found }' "${LICENSE_URLS}")" || return 1
    [[ -n "${url}" ]] || return 1
    printf '%s' "${url}"
}

# ' / '-joined [filename](url) links, one per license file, mirroring how the
# License column joins identifiers.
location_cell() {
    local package="$1" module="$2" version="$3"
    local relative name license_path url cell="" lf
    relative="$(license_dir_within_module "${package}" "${module}")" \
        || die "no license file found for ${package} under ${VENDOR_DIR}/${module}." \
               "Run 'go mod vendor' and re-run."
    while IFS= read -r lf; do
        [[ -z "${lf}" ]] && continue
        name="$(basename "${lf}")"
        license_path="${relative:+${relative}/}${name}"
        url="$(location_for "${module}" "${version}" "${license_path}")" \
            || die "${LICENSE_URLS} has no verified URL for ${module}@${version} ${license_path}." \
                   "Run 'make third-party-notices-urls' (needs network) and commit the result."
        cell="${cell:+${cell} / }[${name}](${url})"
    done < <(license_files_for "${LICENSES_DIR}/${package}")
    [[ -n "${cell}" ]] || die "no license file for ${package} under ${LICENSES_DIR}." \
                              "Run 'make third-party-notices' and re-run."
    printf '%s' "${cell}"
}

emit_index_table() {
    local index="$1" package _ license module version location
    printf '| Package | Module | Version | License | Location |\n'
    printf '|---------|--------|---------|---------|----------|\n'

    while IFS=, read -r package _ license module version; do
        [[ -z "${package}" ]] && continue
        location="$(location_cell "${package}" "${module}" "${version}")"
        # shellcheck disable=SC2016  # backticks are literal markdown here.
        printf '| `%s` | `%s` | %s | %s | %s |\n' \
            "${package}" "${module:-unknown}" "${version:-unknown}" \
            "${license:-Unknown}" "${location}"
    done < "${index}"
}

emit_sections() {
    local index="$1" root="$2"
    local package _ license module version files lf fence relative name url

    while IFS=, read -r package _ license module version; do
        [[ -z "${package}" ]] && continue

        printf '### %s\n\n' "${package}"
        printf '* Module: %s\n' "${module:-unknown}"
        printf '* Version: %s\n' "${version:-unknown}"
        printf '* License: %s\n\n' "${license:-Unknown}"

        files=()
        while IFS= read -r lf; do
            [[ -n "${lf}" ]] && files+=("${lf}")
        done < <(license_files_for "${root}/${package}")

        if (( ${#files[@]} == 0 )); then
            printf 'License text unavailable. See upstream source for the full license.\n'
        else
            relative="$(license_dir_within_module "${package}" "${module}")" \
                || die "no license file found for ${package} under ${VENDOR_DIR}/${module}." \
                       "Run 'go mod vendor' and re-run."
            for lf in "${files[@]}"; do
                name="$(basename "${lf}")"
                url="$(location_for "${module}" "${version}" "${relative:+${relative}/}${name}")" \
                    || die "${LICENSE_URLS} has no verified URL for ${module}@${version} ${relative:+${relative}/}${name}." \
                           "Run 'make third-party-notices-urls' (needs network) and commit the result."
                fence="$(fence_for "${lf}")"
                printf '#### %s\n\n' "${name}"
                printf '<%s>\n\n' "${url}"
                printf '%stext\n' "${fence}"
                cat "${lf}"
                echo
                printf '%s\n' "${fence}"
                echo
            done
        fi
        echo
    done < "${index}"
}

compose_document() {
    require_url_map
    log "Composing ${OUTPUT}..."
    {
        cat <<'EOF'
# Third-Party Notices

NVIDIA GPU Operator

This file lists every third-party dependency that GPU Operator redistributes,
along with the verbatim text of each dependency's license. In particular, this
covers all **Go modules** statically linked into the commands under `cmd/`,
resolved as the union across every released image platform. The `gpu-operator`,
`manage-crds`, `cleanup-gpuclusters` and `nvidia-validator` commands ship in the
`gpu-operator` image. The `gpuop-cfg` command is a build-time helper that is not
shipped; its dependencies are listed here as well rather than excluded. Go
standard library packages are excluded; they are covered by the license of the
Go distribution itself.

Each dependency is listed with the module that owns it, the version
redistributed, and a link to the license file in that version's upstream
source. Every link was verified by fetching it and comparing its contents
against the copy vendored here, so each one resolves to the same license text
reproduced below. Modules that no command under `cmd/` links — those vendored
only for this module's own tests and build tooling — are not listed.

The `gpu-operator` image uses `nvcr.io/nvidia/distroless/cc` as a base image.
All of the OSS packages and source included in this image can be found at
<https://developer.nvidia.com/w/distroless-oss/index.html>. A statically
compiled busybox binary is added to the image, which is licensed under GPLv2.
The image also carries a CUDA sample and the CUDA compatibility libraries, which
are handled separately, including any source-distribution obligations they
carry.

## Dependency Index

EOF
        emit_index_table "${INDEX_FILE}"

        cat <<'EOF'

## License Texts

EOF
        emit_sections "${INDEX_FILE}" "${LICENSES_DIR}"
    } > "${OUT_TMP}"
    # mktemp creates 0600, so fix the mode before the rename. mv, not cp: the
    # rename is atomic, so a failed run leaves the previous document intact.
    chmod 644 "${OUT_TMP}"
    mv "${OUT_TMP}" "${OUTPUT}"
}

main() {
    check_prerequisites
    verify_platform_matrix
    prepare_workspace

    collect_licenses
    build_indexes
    compose_document

    local count
    count=$(wc -l < "${INDEX_FILE}" | tr -d ' ')
    log "Wrote ${OUTPUT} (${count} Go packages)"
}

# Sourced by the tests and by tools/verify-license-urls.sh, which reuse these
# functions without the side effects of a full run.
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    main "$@"
fi
