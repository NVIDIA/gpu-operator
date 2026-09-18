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

set -euo pipefail

OUTPUT="${OUTPUT:-THIRD_PARTY_NOTICES.md}"
MULTI_ARCH_MK="${MULTI_ARCH_MK:-multi-arch.mk}"
MODULES_TXT="${MODULES_TXT:-vendor/modules.txt}"
VENDOR_DIR="${VENDOR_DIR:-vendor}"

REPO_URL="${REPO_URL:-https://github.com/NVIDIA/gpu-operator}"

RELEASE_VERSION=""
LINK_REF=""
LICENSE_OVERRIDES="${LICENSE_OVERRIDES:-tools/license-overrides.tsv}"
DOCKERFILE="${DOCKERFILE:-docker/Dockerfile}"
BUNDLED_COMPONENTS="${BUNDLED_COMPONENTS:-tools/bundled-components.tsv}"
LICENSE_TEXTS_DIR="${LICENSE_TEXTS_DIR:-tools/licenses}"

PACKAGES=("./cmd/...")

PLATFORMS=(
    "linux/amd64"
    "linux/arm64"
)

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

fence_for() {
    local file="$1" longest width
    longest=$(LC_ALL=C grep -oaE '`+' "${file}" 2>/dev/null \
        | awk '{ if (length($0) > m) m = length($0) } END { print m+0 }') || true
    width=$(( longest + 1 ))
    (( width < 3 )) && width=3
    printf '%*s' "${width}" '' | tr ' ' '`'
}

check_prerequisites() {
    command -v go >/dev/null 2>&1 || die "go is not installed."

    if ./bin/go-licenses --help >/dev/null 2>&1; then
        GO_LICENSES="${PWD}/bin/go-licenses"
    elif command -v go-licenses >/dev/null 2>&1; then
        GO_LICENSES="$(command -v go-licenses)"
    else
        die "go-licenses is not installed." "Install it with 'make install-tools'."
    fi

    local required_file
    local required_files=("${MULTI_ARCH_MK}" "${MODULES_TXT}" "${LICENSE_OVERRIDES}")
    [[ "${MODE}" == release ]] && required_files+=("${DOCKERFILE}" "${BUNDLED_COMPONENTS}")
    for required_file in "${required_files[@]}"; do
        [[ -f "${required_file}" ]] \
            || die "${required_file} not found — run 'make third-party-notices' from the repo root."
    done
    [[ -d "${VENDOR_DIR}" ]] \
        || die "${VENDOR_DIR} not found — run 'go mod vendor' and re-run."

    LOCAL_MODULE=$(go list -m 2>/dev/null || true)
    [[ -n "${LOCAL_MODULE}" ]] || die "could not determine local module path via 'go list -m'."

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
    COMBINED_CSV="" INDEX_FILE="" RESOLVED_ROWS="" BUNDLED_ROWS="" OUT_TMP=""
    trap 'rm -f "${COMBINED_CSV}" "${INDEX_FILE}" "${RESOLVED_ROWS}" "${BUNDLED_ROWS}" "${OUT_TMP}"' EXIT

    local workspace_template="${TMPDIR:-/tmp}/gpu-operator-notices"
    COMBINED_CSV="$(mktemp "${workspace_template}-csv.XXXXXX")"
    INDEX_FILE="$(mktemp "${workspace_template}-idx.XXXXXX")"
    RESOLVED_ROWS="$(mktemp "${workspace_template}-rows.XXXXXX")"
    BUNDLED_ROWS="$(mktemp "${workspace_template}-bundled.XXXXXX")"

    local out_dir
    out_dir="$(dirname "${OUTPUT}")"
    mkdir -p "${out_dir}"
    OUT_TMP="$(mktemp "${out_dir}/.$(basename "${OUTPUT}").XXXXXX")"

}

collect_licenses() {
    local platform goos goarch

    for platform in "${PLATFORMS[@]}"; do
        goos="${platform%/*}"
        goarch="${platform#*/}"
        log "Collecting licenses for ${goos}/${goarch}..."

        GOOS="${goos}" GOARCH="${goarch}" "${GO_LICENSES}" csv "${PACKAGES[@]}" \
            --ignore="${LOCAL_MODULE}" \
            >> "${COMBINED_CSV}"
    done
}

collapse_index() {
    LC_ALL=C sort -u "$1" | awk -F, '
        {
            pkg = $1
            if (!(pkg in url)) { url[pkg] = $2; order[++n] = pkg }
            if (!((pkg SUBSEP $3) in seen)) {
                seen[pkg SUBSEP $3] = 1
                lic[pkg] = (cnt[pkg]++ ? lic[pkg] " / " : "") $3
            }
        }
        END { for (i = 1; i <= n; i++) print order[i] "," url[order[i]] "," lic[order[i]] }
    '
}

annotate_modules() {
    awk -v modfile="${MODULES_TXT}" '
        BEGIN {
            FS = OFS = ","
            while ((getline line < modfile) > 0) {
                if (line !~ /^# /) continue
                split(line, f, " ")
                if (f[4] == "=>" || f[3] == "=>") {
                    replacement_field = (f[4] == "=>") ? 5 : 4
                    if (f[replacement_field + 1] == "") {
                        print "ERROR: " modfile " replaces " f[2] " with a local path;" > "/dev/stderr"
                        print "teach tools/generate-third-party-notices.sh how to attribute it." > "/dev/stderr"
                        exit 1
                    }
                    module_paths[++module_count] = f[2]
                    disp[f[2]] = f[replacement_field]
                    ver[f[2]] = f[replacement_field + 1]
                } else {
                    module_paths[++module_count] = f[2]
                    disp[f[2]] = f[2]
                    ver[f[2]] = f[3]
                }
            }
            close(modfile)
            if (module_count == 0) {
                print "ERROR: no module lines read from " modfile > "/dev/stderr"
                exit 1
            }
        }
        {
            best = ""
            for (i = 1; i <= module_count; i++) {
                candidate = module_paths[i]
                if (($1 == candidate || index($1, candidate "/") == 1) && length(candidate) > length(best)) best = candidate
            }
            print $0, (best == "" ? "unknown" : disp[best]), (best == "" ? "unknown" : ver[best])
        }
    '
}

build_index() {
    log "Generating dependency index..."
    collapse_index "${COMBINED_CSV}" | annotate_modules > "${INDEX_FILE}"

    [[ -s "${INDEX_FILE}" ]] \
        || die "go-licenses produced no entries for ${PACKAGES[*]} — refusing to write empty notices file."

    if LC_ALL=C awk -F, '$4 == "unknown" || $5 == "unknown" { found = 1; exit } END { exit !found }' \
        "${INDEX_FILE}"; then
        die "could not resolve module@version for some packages from ${MODULES_TXT}." \
            "Run 'go mod vendor' and re-run, rather than committing a file with unattributed entries."
    fi

    if LC_ALL=C awk -F, '$3 == "" || $3 ~ /(^| \/ )Unknown( \/ |$)/ { found = 1; exit } END { exit !found }' \
        "${INDEX_FILE}"; then
        die "go-licenses could not identify a license for some dependencies." \
            "Check the entries reported as Unknown before committing the file."
    fi

    check_no_stale_overrides "${INDEX_FILE}"
}

check_no_stale_overrides() {
    local index="$1" override_package
    while IFS=$'\t' read -r override_package _ _; do
        case "${override_package}" in
            ''|'#'*) continue ;;
        esac
        LC_ALL=C awk -F, -v key="${override_package}" \
            '$1 == key { found = 1; exit } END { exit !found }' "${index}" \
            || die "${LICENSE_OVERRIDES} has a row for ${override_package}, which is no longer shipped." \
                   "Remove the row rather than leaving a stale license claim in the file."
    done < <(LC_ALL=C awk '{ print }' "${LICENSE_OVERRIDES}")
}

license_files_for() {
    local dir="$1" license_file file_name
    [[ -d "${dir}" ]] || return 0
    shopt -s nocasematch
    while IFS= read -r -d '' license_file; do
        file_name="${license_file##*/}"
        case "${file_name}" in
            *.go|*.c|*.h|*.s|*.py|*.sh|*.java|*.ts|*.js) continue ;;
        esac
        if [[ "${file_name}" =~ ^(licen[cs]e|notice|copying|copyright|authors|patents)([-._].*)?$ ]]; then
            printf '%s\n' "${license_file}"
        fi
    done < <(find "${dir}" -maxdepth 1 -type f -print0 2>/dev/null | LC_ALL=C sort -z)
    shopt -u nocasematch
}

relative_license_dir_within_module() {
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

license_identifier_for() {
    local package="$1" reported="$2" override
    override=$(LC_ALL=C awk -F'\t' -v key="${package}" '
        /^#/ { next }
        $1 == key { print $2; found = 1; exit }
        END { exit !found }
    ' "${LICENSE_OVERRIDES}") && {
        [[ -n "${override}" ]] \
            || die "${LICENSE_OVERRIDES} has an empty license identifier for ${package}."
        printf '%s' "${override}"
        return 0
    }
    printf '%s' "${reported}"
}

vendored_license_file_location() {
    local license_file="$1" module="$2" relative_license_dir="$3"
    local license_path="${relative_license_dir:+${relative_license_dir}/}${license_file##*/}"
    if [[ "${MODE}" == release ]]; then
        printf '%s/blob/%s/vendor/%s/%s' "${REPO_URL}" "${LINK_REF}" "${module}" "${license_path}"
    else
        printf 'vendor/%s/%s' "${module}" "${license_path}"
    fi
}

location_markup_for() {
    local governing_dir="$1" module="$2" relative_license_dir="$3"
    local location_links="" license_file
    while IFS= read -r license_file; do
        location_links+="${location_links:+ / }[${license_file##*/}]($(vendored_license_file_location \
            "${license_file}" "${module}" "${relative_license_dir}"))"
    done < <(license_files_for "${governing_dir}")
    [[ -n "${location_links}" ]] || return 1
    printf '%s' "${location_links}"
}

base_image_from_dockerfile() {
    local from_line repository tag
    from_line=$(LC_ALL=C awk '$1 == "FROM" { line = $0 } END { print line }' "${DOCKERFILE}")
    [[ -n "${from_line}" ]] || die "no FROM instruction found in ${DOCKERFILE}."

    [[ "${from_line}" =~ ^FROM[[:space:]]+([A-Za-z0-9._/-]+):([A-Za-z0-9_][A-Za-z0-9._-]*)@sha256:[0-9a-f]{64}[[:space:]]*$ ]] \
        || die "the final FROM in ${DOCKERFILE} is not a digest-pinned literal image." \
               "Teach base_image_from_dockerfile about it rather than publishing an unverified base image version." \
               "  ${from_line}"

    repository="${BASH_REMATCH[1]}"
    tag="${BASH_REMATCH[2]}"

    [[ "${repository}" == nvcr.io/nvidia/distroless/* ]] \
        || die "base image ${repository} is not an NVIDIA distroless image." \
               "Its OSS source index is published elsewhere; teach the generator where before releasing."

    printf '%s\t%s\n' "${repository}" "${tag}"
}

emit_base_image_table() {
    local repository="$1" tag="$2" image_short_name source_index_version
    image_short_name="${repository##*/}"
    source_index_version="${tag%-dev}"

    cat <<'EOF'
The base image's own sources are published per version. A `-dev` variant is
built from the sources published under the corresponding release version.

| Image | Version | Role | Notices and source |
|-------|---------|------|--------------------|
EOF
    # shellcheck disable=SC2016  # backticks are literal markdown here.
    printf '| `%s` | `%s` | final runtime base | [NVIDIA Distroless OSS source index](https://developer.download.nvidia.com/distroless-oss/%s/%s/index.html) |\n' \
        "${repository}" "${tag}" "${image_short_name}" "${source_index_version}"
}

dockerfile_arg_default() {
    local name="$1" value
    value=$(LC_ALL=C awk -v name="${name}" '
        $1 == "ARG" && index($2, name "=") == 1 { print substr($2, length(name) + 2); exit }
    ' "${DOCKERFILE}")
    [[ -n "${value}" ]] || return 1
    printf '%s' "${value}"
}

expand_dockerfile_args() {
    local text="$1" name value expansions=0
    while [[ "${text}" =~ \$\{([A-Za-z_][A-Za-z0-9_]*)\} ]]; do
        name="${BASH_REMATCH[1]}"
        (( ++expansions <= 16 )) || die "ARG references in '$1' expand recursively."
        value="$(dockerfile_arg_default "${name}")" \
            || die "ARG ${name} has no default in ${DOCKERFILE}, so ${text} cannot be resolved."
        text="${text//\$\{${name}\}/${value}}"
    done
    printf '%s' "${text}"
}

final_stage_copies() {
    local base_repository="$1"
    [[ -n "${base_repository}" ]] \
        || die "final_stage_copies needs the repository of the image the final stage is built from."

    LC_ALL=C awk -v base_repository="${base_repository}" '
        $1 == "FROM" {
            copy_count = 0
            final_stage_matches = (index($2, base_repository) == 1)
            next
        }
        $1 == "COPY" {
            from_stage = ""; first_source = 0
            for (i = 2; i <= NF; i++) {
                if ($i ~ /^--/) { if ($i ~ /^--from=/) from_stage = $i; continue }
                first_source = i; break
            }
            if (from_stage == "" || first_source == 0) next
            for (i = first_source; i < NF; i++) copies[++copy_count] = $i "\t" $NF
        }
        END {
            if (!final_stage_matches) {
                print "ERROR: the last FROM is not " base_repository > "/dev/stderr"
                exit 1
            }
            for (i = 1; i <= copy_count; i++) print copies[i]
        }
    ' "${DOCKERFILE}"
}

bundled_component_row() {
    LC_ALL=C awk -F'\t' -v key="$1" '
        /^#/ { next }
        $1 == key { print; found = 1; exit }
        END { exit !found }
    ' "${BUNDLED_COMPONENTS}"
}

check_bundled_catalogue_shape() {
    LC_ALL=C awk -F'\t' -v file="${BUNDLED_COMPONENTS}" '
        /^#/ || /^[[:space:]]*$/ { next }
        NF != 9 {
            printf "ERROR: %s line %d has %d fields, expected 9.\n", file, NR, NF > "/dev/stderr"
            bad = 1
            next
        }
        {
            for (i = 1; i <= NF; i++)
                if ($i == "") {
                    printf "ERROR: %s line %d field %d is empty; write \"-\".\n", file, NR, i > "/dev/stderr"
                    bad = 1
                }
        }
        END { exit bad }
    ' "${BUNDLED_COMPONENTS}" || die "${BUNDLED_COMPONENTS} is malformed."
}

check_no_duplicate_bundled_keys() {
    local duplicates
    duplicates=$(LC_ALL=C awk -F'\t' '!/^#/ && NF { print $1 }' "${BUNDLED_COMPONENTS}" \
        | LC_ALL=C sort | LC_ALL=C uniq -d)
    [[ -z "${duplicates}" ]] \
        || die "${BUNDLED_COMPONENTS} has more than one row for: ${duplicates//$'\n'/, }"
}

check_bundled_coverage() {
    local base_repository="$1"
    [[ -n "${base_repository}" ]] \
        || die "check_bundled_coverage needs the repository of the image the final stage is built from."
    local source_path destination expanded
    while IFS=$'\t' read -r source_path destination; do
        [[ -z "${source_path}" ]] && continue
        expanded="$(expand_dockerfile_args "${source_path}")" || exit 1
        bundled_component_row "${source_path}" >/dev/null \
            || bundled_component_row "${expanded}" >/dev/null \
            || die "${DOCKERFILE} copies ${source_path} into the released image, and ${BUNDLED_COMPONENTS} has no row for it." \
                   "Add one saying what it is and under what terms it is redistributed."
    done < <(final_stage_copies "${base_repository}")
}

check_bundled_rows() {
    local component version license license_file notices_url
    [[ -s "${BUNDLED_ROWS}" ]] || return 0
    while IFS=$'\t' read -r component version license license_file notices_url _ _; do
        [[ "${version}" != "-" ]] \
            || die "${BUNDLED_COMPONENTS} lists ${component} as third-party with no version."
        [[ "${license}" != "-" ]] \
            || die "${BUNDLED_COMPONENTS} lists ${component} as third-party with no license identifier."
        [[ "${license_file}" != "-" ]] \
            || die "${BUNDLED_COMPONENTS} lists ${component} as third-party with no license file."
        [[ "${notices_url}" != "-" ]] \
            || die "${BUNDLED_COMPONENTS} lists ${component} as third-party with no notices URL."
        [[ -f "${LICENSE_TEXTS_DIR}/${license_file}" ]] \
            || die "${BUNDLED_COMPONENTS} names ${license_file} for ${component}, which is missing from ${LICENSE_TEXTS_DIR}."
    done < "${BUNDLED_ROWS}"
}

check_version_provenance() {
    local source_path component provenance_digest
    while IFS=$'\t' read -r source_path component _ _ _ _ _ provenance_digest _; do
        case "${source_path}" in ''|'#'*) continue ;; esac
        [[ "${provenance_digest}" == "-" ]] && continue
        LC_ALL=C grep -qF "${provenance_digest}" "${DOCKERFILE}" \
            || die "${BUNDLED_COMPONENTS} records ${component}'s version from ${provenance_digest}, which ${DOCKERFILE} no longer builds from." \
                   "Re-check the version in the new image and update the row."
    done < <(LC_ALL=C awk '{ print }' "${BUNDLED_COMPONENTS}")
}

third_party_bundled_rows() {
    local base_repository="$1"
    local source_path destination expanded row
    local component disposition version license license_file notices_url notes

    while IFS=$'\t' read -r source_path destination; do
        [[ -z "${source_path}" ]] && continue
        expanded="$(expand_dockerfile_args "${source_path}")" || return 1
        row="$(bundled_component_row "${source_path}")" \
            || row="$(bundled_component_row "${expanded}")"
        IFS=$'\t' read -r _ component disposition version license license_file notices_url _ notes <<< "${row}"

        case "${disposition}" in project|first-party) continue ;; esac

        local expanded_version expanded_notices_url expanded_destination
        expanded_version="$(expand_dockerfile_args "${version}")" || return 1
        expanded_notices_url="$(expand_dockerfile_args "${notices_url}")" || return 1
        expanded_destination="$(expand_dockerfile_args "${destination}")" || return 1
        printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
            "${component}" "${expanded_version}" "${license}" "${license_file}" \
            "${expanded_notices_url}" "${expanded_destination}" "${notes}"
    done < <(final_stage_copies "${base_repository}")
}

emit_bundled_index() {
    local component version license notices_url

    cat <<'EOF'

## Bundled Components

The image also carries software that is not a Go module, copied in by the
Dockerfile. Components built from this repository are covered by the Dependency
Index above and are not repeated here.

| Component | Version | License | Notices and source |
|-----------|---------|---------|--------------------|
EOF

    while IFS=$'\t' read -r component version license _ notices_url _ _; do
        # shellcheck disable=SC2016  # backticks are literal markdown here.
        printf '| `%s` | %s | %s | [source](%s) |\n' \
            "${component}" "${version}" "${license}" "${notices_url}"
    done < "${BUNDLED_ROWS}"
}

emit_bundled_sections() {
    local component version license license_file notices_url destination notes license_text_path fence

    printf '\n## Bundled Component License Texts\n\n'

    while IFS=$'\t' read -r component version license license_file notices_url destination notes; do
        printf '### %s\n\n' "${component}"
        printf '* Version: %s\n' "${version}"
        printf '* License: %s\n' "${license}"
        # shellcheck disable=SC2016  # backticks are literal markdown here.
        printf '* Installed at: `%s`\n' "${destination}"
        printf '* Notices and source: <%s>\n' "${notices_url}"
        [[ "${notes}" != "-" ]] && printf '* Note: %s\n' "${notes}"
        printf '\n'

        license_text_path="${LICENSE_TEXTS_DIR}/${license_file}"
        fence="$(fence_for "${license_text_path}")"
        printf '#### %s\n\n' "${license_file##*/}"
        printf '%stext\n' "${fence}"
        cat "${license_text_path}"
        echo
        printf '%s\n\n' "${fence}"
    done < "${BUNDLED_ROWS}"
}

resolve_index_rows() {
    local index_file="$1"
    local package _ license module version
    local license_identifier relative_license_dir governing_dir

    while IFS=, read -r package _ license module version; do
        [[ -z "${package}" ]] && continue
        license_identifier="$(license_identifier_for "${package}" "${license:-Unknown}")"
        relative_license_dir="$(relative_license_dir_within_module "${package}" "${module}")" \
            || die "no license file found for ${package} under ${VENDOR_DIR}/${module}." \
                   "Run 'go mod vendor' and re-run."
        governing_dir="${VENDOR_DIR}/${module}${relative_license_dir:+/${relative_license_dir}}"
        printf '%s\t%s\t%s\t%s\t%s\t%s\n' \
            "${package}" "${version}" "${license_identifier}" \
            "${module}" "${governing_dir}" "${relative_license_dir}"
    done < "${index_file}"
}

emit_index_table() {
    local resolved_rows="$1"
    local package version license_identifier module governing_dir relative_license_dir
    local location_markup

    if [[ "${MODE}" == release ]]; then
        printf '| Package | Version | License | Location |\n'
        printf '|---------|---------|---------|----------|\n'
    else
        printf '| Package | License | Location |\n'
        printf '|---------|---------|----------|\n'
    fi

    while IFS=$'\t' read -r package version license_identifier module governing_dir relative_license_dir; do
        location_markup="$(location_markup_for "${governing_dir}" "${module}" "${relative_license_dir}")" \
            || die "could not resolve a license location for ${package} (${module})."

        # shellcheck disable=SC2016  # backticks are literal markdown here.
        if [[ "${MODE}" == release ]]; then
            printf '| `%s` | %s | %s | %s |\n' \
                "${package}" "${version}" "${license_identifier}" "${location_markup}"
        else
            printf '| `%s` | %s | %s |\n' \
                "${package}" "${license_identifier}" "${location_markup}"
        fi
    done < "${resolved_rows}"
}

emit_sections() {
    local resolved_rows="$1"
    local package version license_identifier module governing_dir relative_license_dir
    local license_file fence license_location

    while IFS=$'\t' read -r package version license_identifier module governing_dir relative_license_dir; do
        printf '### %s\n\n' "${package}"
        [[ "${MODE}" == release ]] && printf '* Version: %s\n' "${version}"
        printf '* License: %s\n\n' "${license_identifier}"

        while IFS= read -r license_file; do
            fence="$(fence_for "${license_file}")"
            license_location="$(vendored_license_file_location \
                "${license_file}" "${module}" "${relative_license_dir}")"
            printf '#### %s\n\n' "${license_file##*/}"
            if [[ "${MODE}" == release ]]; then
                printf '<%s>\n\n' "${license_location}"
            else
                printf '[%s](%s)\n\n' "${license_location}" "${license_location}"
            fi
            printf '%stext\n' "${fence}"
            cat "${license_file}"
            echo
            printf '%s\n' "${fence}"
            echo
        done < <(license_files_for "${governing_dir}")
        echo
    done < "${resolved_rows}"
}

emit_header() {
    printf '# Third-Party Notices\n\n'
    if [[ "${MODE}" == release ]]; then
        printf 'NVIDIA GPU Operator %s\n\n' "${RELEASE_VERSION}"
    else
        printf 'NVIDIA GPU Operator\n\n'
    fi

    cat <<'EOF'
This file lists every third-party dependency that GPU Operator redistributes,
along with the verbatim text of each dependency's license. In particular, this
covers all **Go modules** statically linked into the commands under `cmd/`,
resolved as the union across every released image platform. The `gpu-operator`,
`manage-crds`, `cleanup-gpuclusters` and `nvidia-validator` commands ship in the
`gpu-operator` image. The `gpuop-cfg` command is a build-time helper that is not
shipped; its dependencies are listed here as well rather than excluded. Go
standard library packages are excluded; they are covered by the license of the
Go distribution itself. Modules used only by this repository's tests and build
tooling are not redistributed and are not listed.

Each dependency's Location links to its license file as vendored in this
repository, so every link serves the exact text reproduced below it. Where a
dependency ships more than one license-bearing file, such as a PATENTS or
NOTICE alongside its LICENSE, each one is listed and reproduced.
EOF

    if [[ "${MODE}" == release ]]; then
        cat <<'EOF'

Software the Dockerfile adds on top of the base image is listed under Bundled
Components below, with its license and corresponding source. NVIDIA's own
components are not third party to NVIDIA and are out of scope here.

EOF
    else
        cat <<'EOF'

The `gpu-operator` image uses `nvcr.io/nvidia/distroless/cc` as a base image.
All of the OSS packages and source included in this image can be found at
<https://developer.nvidia.com/w/distroless-oss/index.html>. A statically
compiled busybox binary is added to the image, which is licensed under GPLv2.
The image also carries a CUDA sample and the CUDA compatibility libraries, which
are handled separately, including any source-distribution obligations they
carry.
EOF
    fi
}

compose_document() {
    log "Composing ${OUTPUT}..."
    {
        emit_header
        if [[ "${MODE}" == release ]]; then
            emit_base_image_table "${BASE_IMAGE_REPOSITORY}" "${BASE_IMAGE_TAG}"
            emit_bundled_index
        fi
        printf '\n## Dependency Index\n\n'
        emit_index_table "${RESOLVED_ROWS}"
        printf '\n## License Texts\n\n'
        emit_sections "${RESOLVED_ROWS}"
        [[ "${MODE}" == release ]] && emit_bundled_sections
    } > "${OUT_TMP}"
    chmod 644 "${OUT_TMP}"
    mv "${OUT_TMP}" "${OUTPUT}"
}

usage() {
    cat >&2 <<'EOF'
Usage:
  generate-third-party-notices.sh repo    [--output FILE]
  generate-third-party-notices.sh release --version VERSION --link-ref REF \
                                          --output FILE [--repo-url URL]

  repo      the document tracked on main: no versions, cites main
  release   the artifact for one ref: versions, cites that ref
  --link-ref  tag the release was cut from, or a full 40-character SHA
EOF
    exit 2
}

parse_arguments() {
    local output_given=""

    [[ $# -gt 0 ]] || usage
    MODE="$1"
    shift
    case "${MODE}" in repo|release) ;; *) usage ;; esac

    while [[ $# -gt 0 ]]; do
        [[ "$1" == --* && $# -ge 2 && "$2" != --* ]] || usage
        case "$1" in
            --version)  RELEASE_VERSION="$2" ;;
            --link-ref) LINK_REF="$2" ;;
            --repo-url) REPO_URL="$2" ;;
            --output)   OUTPUT="$2"; output_given=1 ;;
            *)          usage ;;
        esac
        shift 2
    done

    if [[ "${MODE}" != release ]]; then
        [[ -z "${RELEASE_VERSION}" ]] || die "--version is only valid for the release subcommand."
        [[ -z "${LINK_REF}" ]] || die "--link-ref is only valid for the release subcommand."
        LINK_REF="main"
        return 0
    fi

    [[ -n "${RELEASE_VERSION}" ]] || die "the release subcommand needs --version."
    [[ "${RELEASE_VERSION}" =~ ^[A-Za-z0-9][A-Za-z0-9._+-]*$ ]] \
        || die "invalid --version '${RELEASE_VERSION}'."
    [[ "${REPO_URL}" =~ ^https://[A-Za-z0-9._~:/?#@!$\&*+,\;=%-]+$ ]] \
        || die "invalid --repo-url '${REPO_URL}'."
    [[ -n "${LINK_REF}" ]] || die "the release subcommand needs --link-ref."
    [[ "${LINK_REF}" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]] \
        || die "invalid --link-ref '${LINK_REF}': expected a tag name or a full 40-character SHA."
    [[ -n "${output_given}" ]] \
        || die "the release subcommand needs --output; it is never written into the checkout."
}

main() {
    parse_arguments "$@"

    check_prerequisites
    verify_platform_matrix
    prepare_workspace
    if [[ "${MODE}" == release ]]; then
        IFS=$'\t' read -r BASE_IMAGE_REPOSITORY BASE_IMAGE_TAG < <(base_image_from_dockerfile)
        check_bundled_catalogue_shape
        check_no_duplicate_bundled_keys
        check_bundled_coverage "${BASE_IMAGE_REPOSITORY}"
        third_party_bundled_rows "${BASE_IMAGE_REPOSITORY}" > "${BUNDLED_ROWS}"
        check_bundled_rows
        check_version_provenance
    fi

    collect_licenses
    build_index
    resolve_index_rows "${INDEX_FILE}" > "${RESOLVED_ROWS}"
    compose_document

    local count
    count=$(wc -l < "${INDEX_FILE}" | tr -d ' ')
    log "Wrote ${OUTPUT} (${count} Go packages)"
}

if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    main "$@"
fi
