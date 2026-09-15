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

# Citing this repository rather than each dependency's upstream: the link then
# serves the exact bytes reproduced below it, and cannot rot when an upstream
# retags, renames or archives. Constant rather than derived from the git
# remote, so the document is byte-identical wherever it is generated.
REPO_URL="${REPO_URL:-https://github.com/NVIDIA/gpu-operator}"

# repo: the document tracked on main, describing a moving target. It states no
# version and cites main, which the CI check keeps in step with this file.
# release: the artifact for one commit. It states the version of every
# dependency and cites that commit, which cannot move even if a tag is
# re-pointed later.
#
# A subcommand rather than an environment variable: an inherited variable would
# silently redirect the output file, leaving THIRD_PARTY_NOTICES.md untouched
# while 'make check-third-party-notices' reported success.
MODE=""
RELEASE_VERSION=""
LINK_REF="main"
BASE_IMAGE_REPOSITORY=""
BASE_IMAGE_TAG=""
LICENSE_OVERRIDES="${LICENSE_OVERRIDES:-tools/license-overrides.tsv}"
DOCKERFILE="${DOCKERFILE:-docker/Dockerfile}"
BUNDLED_COMPONENTS="${BUNDLED_COMPONENTS:-tools/bundled-components.tsv}"
LICENSE_TEXTS_DIR="${LICENSE_TEXTS_DIR:-tools/licenses}"

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
        | awk '{ if (length($0) > m) m = length($0) } END { print m+0 }')
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

    local required_file
    for required_file in "${MULTI_ARCH_MK}" "${MODULES_TXT}" "${LICENSE_OVERRIDES}" "${DOCKERFILE}" "${BUNDLED_COMPONENTS}"; do
        [[ -f "${required_file}" ]] \
            || die "${required_file} not found — run 'make third-party-notices' from the repo root."
    done
    [[ -d "${VENDOR_DIR}" ]] \
        || die "${VENDOR_DIR} not found — run 'go mod vendor' and re-run."

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
    # Explicit templates: macOS mktemp ignores TMPDIR without one.
    local workspace_template="${TMPDIR:-/tmp}/gpu-operator-notices"
    COMBINED_CSV="$(mktemp "${workspace_template}-csv.XXXXXX")"
    INDEX_FILE="$(mktemp "${workspace_template}-idx.XXXXXX")"

    # Composed beside its destination, not under TMPDIR, so the last step is a
    # same-filesystem rename(2) rather than a copy-then-unlink.
    local out_dir
    out_dir="$(dirname "${OUTPUT}")"
    mkdir -p "${out_dir}"
    OUT_TMP="$(mktemp "${out_dir}/.$(basename "${OUTPUT}").XXXXXX")"

    trap 'rm -f "${COMBINED_CSV}" "${INDEX_FILE}" "${OUT_TMP}"' EXIT
}

# Only the classification is collected. License text is read from vendor/, which
# holds the bytes actually redistributed, and holds the secondary files such as
# PATENTS and NOTICE that 'go-licenses save' leaves behind when it copies just
# the one file it classified.
collect_licenses() {
    local platform goos goarch

    for platform in "${PLATFORMS[@]}"; do
        goos="${platform%/*}"
        goarch="${platform#*/}"
        log "Collecting licenses for ${goos}/${goarch}..."

        # Only the local module: --ignore matches plain string prefixes, not
        # path segments, so a stdlib list's bare "go" would silently drop
        # golang.org/x/*, google.golang.org/* and gopkg.in/*.
        GOOS="${goos}" GOARCH="${goarch}" "${GO_LICENSES}" csv "${PACKAGES[@]}" \
            --ignore="${LOCAL_MODULE}" \
            >> "${COMBINED_CSV}"
    done
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

# Rows carry module names from modules.txt rather than a URL: in vendor mode
# go-licenses reports a URL into this repo at HEAD, which stops describing
# released content once main moves. The version is carried in both modes and
# only its rendering is gated, so one index serves both documents.
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
            # A read error makes getline return -1 and the loop never run.
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

    check_override_coverage "${INDEX_FILE}"
}

# A dropped dependency would otherwise leave its row in LICENSE_OVERRIDES
# silently asserting a license for a package no longer shipped.
check_override_coverage() {
    local index="$1" override_package
    while IFS=$'\t' read -r override_package _ _; do
        case "${override_package}" in
            ''|'#'*) continue ;;
        esac
        LC_ALL=C cut -d, -f1 "${index}" | LC_ALL=C grep -qFx "${override_package}" \
            || die "${LICENSE_OVERRIDES} has a row for ${override_package}, which is no longer shipped." \
                   "Remove the row rather than leaving a stale license claim in the file."
    done < "${LICENSE_OVERRIDES}"
}

# Filter by name: these are vendored module directories, so most of what they
# hold is source code.
#
# Parameter expansion and [[ =~ ]] rather than basename and grep: this runs for
# every file of every scanned directory, and the subshell-per-file version cost
# ~5,700 process spawns and about 40 seconds per run.
license_files_for() {
    local dir="$1" license_file file_name
    [[ -d "${dir}" ]] || return 0
    while IFS= read -r -d '' license_file; do
        file_name="${license_file##*/}"
        # Exclude source files: the name pattern below also matches source files
        # that merely start with a license-shaped header, such as
        # k8s.io/kube-openapi/pkg/validation/spec/license.go, a Go file opening
        # "// Copyright 2015 go-swagger maintainers".
        case "${file_name}" in
            *.go|*.c|*.h|*.s|*.py|*.sh|*.java|*.ts|*.js) continue ;;
        esac
        shopt -s nocasematch
        if [[ "${file_name}" =~ ^(licen[cs]e|notice|copying|copyright|authors|patents)([-._].*)?$ ]]; then
            shopt -u nocasematch
            printf '%s\n' "${license_file}"
            continue
        fi
        shopt -u nocasematch
    done < <(find "${dir}" -maxdepth 1 -type f -print0 2>/dev/null | LC_ALL=C sort -z)
}

# A module may license a subtree separately, so the nearest license walking up
# from the package wins; only if none is found up to the module root does the
# package have no license of its own.
license_dir_within_module() {
    local package="$1" module="$2" dir="$1" relative
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

# go-licenses reports one identifier per file and scores a file bundling two
# licenses as whichever it rates highest, which understates the terms. A curated
# row wins over that guess.
license_identifier_for() {
    local package="$1" reported="$2" override
    override=$(LC_ALL=C awk -F'\t' -v key="${package}" '
        /^#/ { next }
        $1 == key { print $2; found = 1; exit }
        END { exit !found }
    ' "${LICENSE_OVERRIDES}") && printf '%s' "${override}" && return 0
    printf '%s' "${reported}"
}

license_url() {
    local module="$1" license_path="$2"
    printf '%s/blob/%s/vendor/%s/%s' "${REPO_URL}" "${LINK_REF}" "${module}" "${license_path}"
}

location_cell() {
    local governing_dir="$1" module="$2" relative_license_dir="$3"
    local cell="" license_file file_name license_path
    while IFS= read -r license_file; do
        [[ -z "${license_file}" ]] && continue
        file_name="${license_file##*/}"
        license_path="${relative_license_dir:+${relative_license_dir}/}${file_name}"
        cell+="${cell:+ / }[${file_name}]($(license_url "${module}" "${license_path}"))"
    done < <(license_files_for "${governing_dir}")
    [[ -n "${cell}" ]] || return 1
    printf '%s' "${cell}"
}

# Only a pinned literal image is accepted. An ARG-interpolated tag cannot be
# resolved without the build arguments the release was actually built with, and
# guessing one would put a false version in a legal document.
base_image_from_dockerfile() {
    local from_line repository tag
    from_line=$(LC_ALL=C grep -E '^FROM[[:space:]]' "${DOCKERFILE}" | tail -1)
    [[ -n "${from_line}" ]] || die "no FROM instruction found in ${DOCKERFILE}."

    # Same shape renovate pins against in .github/renovate.json.
    # The tag charset is Docker's own, which excludes '$' and '{': an
    # interpolated tag must not slip through as if it were a literal one.
    [[ "${from_line}" =~ ^FROM[[:space:]]+([A-Za-z0-9._/-]+):([A-Za-z0-9_][A-Za-z0-9._-]*)@sha256:[0-9a-f]{64}[[:space:]]*$ ]] \
        || die "the final FROM in ${DOCKERFILE} is not a digest-pinned literal image." \
               "Teach base_image_from_dockerfile about it rather than publishing an unverified base image version." \
               "  ${from_line}"

    repository="${BASH_REMATCH[1]}"
    tag="${BASH_REMATCH[2]}"

    # The source index below is published per distroless image name. Any other
    # base has its sources somewhere else, so refuse rather than link to a page
    # that does not describe it.
    [[ "${repository}" == nvcr.io/nvidia/distroless/* ]] \
        || die "base image ${repository} is not an NVIDIA distroless image." \
               "Its OSS source index is published elsewhere; teach the generator where before releasing."

    printf '%s\t%s\n' "${repository}" "${tag}"
}

# NVIDIA publishes distroless sources per released version. A "-dev" tag is
# built from the sources published under the corresponding release version, so
# the suffix is dropped to find the index.
resolve_base_image() {
    IFS=$'\t' read -r BASE_IMAGE_REPOSITORY BASE_IMAGE_TAG < <(base_image_from_dockerfile)
}

emit_base_image_table() {
    local image_name index_version
    [[ -n "${BASE_IMAGE_REPOSITORY}" ]] || resolve_base_image
    image_name="${BASE_IMAGE_REPOSITORY##*/}"
    index_version="${BASE_IMAGE_TAG%-dev}"

    cat <<'EOF'
The base image's own sources are published per version. A `-dev` variant is
built from the sources published under the corresponding release version.

| Image | Version | Role | Notices and source |
|-------|---------|------|--------------------|
EOF
    printf '| `%s` | `%s` | final runtime base | [NVIDIA Distroless OSS source index](https://developer.download.nvidia.com/distroless-oss/%s/%s/index.html) |\n' \
        "${BASE_IMAGE_REPOSITORY}" "${BASE_IMAGE_TAG}" "${image_name}" "${index_version}"
}

# An ARG's default value, as declared in the Dockerfile. awk rather than sed:
# BSD sed has no \+, so the same expression matches on Linux and not on macOS.
dockerfile_arg() {
    local name="$1" value
    value=$(LC_ALL=C awk -v name="${name}" '
        $1 == "ARG" && index($2, name "=") == 1 { print substr($2, length(name) + 2); exit }
    ' "${DOCKERFILE}")
    [[ -n "${value}" ]] || return 1
    printf '%s' "${value}"
}

# Substitutes ${ARG} references against the Dockerfile's declared defaults. A
# release built with --build-arg overrides would not match, which is why the
# base image itself is required to be a literal.
expand_dockerfile_args() {
    local text="$1" name value
    while [[ "${text}" =~ \$\{([A-Za-z_][A-Za-z0-9_]*)\} ]]; do
        name="${BASH_REMATCH[1]}"
        # Assigned first: a die here would only exit the substitution's
        # subshell, and the expansion would silently produce an empty version.
        value="$(dockerfile_arg "${name}")" \
            || die "ARG ${name} has no default in ${DOCKERFILE}, so ${text} cannot be resolved."
        text="${text//\$\{${name}\}/${value}}"
    done
    printf '%s' "${text}"
}

# Source paths the final stage copies out of an earlier build stage, in order.
final_stage_copy_sources() {
    LC_ALL=C awk -v base="${BASE_IMAGE_REPOSITORY}" '
        $1 == "FROM" && index($2, base) == 1 { in_final = 1; next }
        in_final && $1 == "COPY" && $2 ~ /^--from=/ { print $3 "\t" $4 }
    ' "${DOCKERFILE}"
}

bundled_component_row() {
    LC_ALL=C awk -F'\t' -v key="$1" '
        /^#/ { next }
        $1 == key { print; found = 1; exit }
        END { exit !found }
    ' "${BUNDLED_COMPONENTS}"
}

# Fails when the final stage copies something no row accounts for, so a new
# component cannot reach the image without an attribution decision.
check_bundled_coverage() {
    local source_path destination expanded
    while IFS=$'\t' read -r source_path destination; do
        [[ -z "${source_path}" ]] && continue
        expanded="$(expand_dockerfile_args "${source_path}")" || exit 1
        bundled_component_row "${source_path}" >/dev/null \
            || bundled_component_row "${expanded}" >/dev/null \
            || die "${DOCKERFILE} copies ${source_path} into the released image, and ${BUNDLED_COMPONENTS} has no row for it." \
                   "Add one saying what it is and under what terms it is redistributed."
    done < <(final_stage_copy_sources)
}

# A version recorded by hand is only true of the image it was observed in, so
# bumping that image must force someone to re-check it rather than letting the
# document keep asserting the old one.
check_recorded_versions() {
    local source_path component provenance_digest
    while IFS=$'\t' read -r source_path component _ _ _ _ _ provenance_digest _; do
        case "${source_path}" in ''|'#'*) continue ;; esac
        [[ "${provenance_digest}" == "-" ]] && continue
        LC_ALL=C grep -qF "${provenance_digest}" "${DOCKERFILE}" \
            || die "${BUNDLED_COMPONENTS} records ${component}'s version from ${provenance_digest}, which ${DOCKERFILE} no longer builds from." \
                   "Re-check the version in the new image and update the row."
    done < "${BUNDLED_COMPONENTS}"
}

emit_bundled_index() {
    local source_path destination component disposition version license license_file source_url notes
    local provenance_digest expanded row rendered_version rendered_source

    cat <<'EOF'

## Bundled Components

The image also carries software that is not a Go module: binaries and libraries
copied in by the Dockerfile. Components built from this repository are covered
by the Dependency Index above and are not repeated here.

| Component | Version | License | Notices and source |
|-----------|---------|---------|--------------------|
EOF

    while IFS=$'\t' read -r source_path destination; do
        [[ -z "${source_path}" ]] && continue
        expanded="$(expand_dockerfile_args "${source_path}")" || exit 1
        row="$(bundled_component_row "${source_path}")" || row="$(bundled_component_row "${expanded}")"
        IFS=$'\t' read -r _ component disposition version license license_file source_url provenance_digest notes <<< "${row}"
        # project and first-party components are not third party to NVIDIA.
        case "${disposition}" in project|first-party) continue ;; esac

        rendered_version="$(expand_dockerfile_args "${version}")" || exit 1
        [[ "${rendered_version}" == "-" ]] && rendered_version="not pinned by the build"
        rendered_source="$(expand_dockerfile_args "${source_url}")" || exit 1
        printf '| `%s` | %s | %s | [source](%s) |\n' \
            "${component}" "${rendered_version}" "${license}" "${rendered_source}"
    done < <(final_stage_copy_sources)
}

emit_bundled_sections() {
    local source_path destination component disposition version license license_file source_url notes
    local provenance_digest expanded row rendered_version rendered_source text_path fence

    printf '\n## Bundled Component License Texts\n\n'

    while IFS=$'\t' read -r source_path destination; do
        [[ -z "${source_path}" ]] && continue
        expanded="$(expand_dockerfile_args "${source_path}")" || exit 1
        row="$(bundled_component_row "${source_path}")" || row="$(bundled_component_row "${expanded}")"
        IFS=$'\t' read -r _ component disposition version license license_file source_url provenance_digest notes <<< "${row}"
        # project and first-party components are not third party to NVIDIA.
        case "${disposition}" in project|first-party) continue ;; esac

        rendered_version="$(expand_dockerfile_args "${version}")" || exit 1
        [[ "${rendered_version}" == "-" ]] && rendered_version="not pinned by the build"

        printf '### %s\n\n' "${component}"
        printf '* Version: %s\n' "${rendered_version}"
        printf '* License: %s\n' "${license}"
        printf '* Installed at: `%s`\n' "$(expand_dockerfile_args "${destination}")"
        rendered_source="$(expand_dockerfile_args "${source_url}")" || exit 1
        printf '* Corresponding source: <%s>\n' "${rendered_source}"
        [[ -n "${notes}" ]] && printf '* Note: %s\n' "${notes}"
        printf '\n'

        if [[ "${license_file}" == "-" ]]; then
            printf 'Redistributed under NVIDIA terms rather than an open source license, so the text is referenced above rather than reproduced.\n\n'
            continue
        fi

        text_path="${LICENSE_TEXTS_DIR}/${license_file}"
        [[ -f "${text_path}" ]] \
            || die "${BUNDLED_COMPONENTS} names ${license_file} for ${component}, which is missing from ${LICENSE_TEXTS_DIR}."
        fence="$(fence_for "${text_path}")"
        printf '#### %s\n\n' "${license_file##*/}"
        printf '%stext\n' "${fence}"
        cat "${text_path}"
        echo
        printf '%s\n\n' "${fence}"
    done < <(final_stage_copy_sources)
}

emit_index_table() {
    local index="$1" package _ license module version
    local license_identifier relative_license_dir governing_dir location_markup

    if [[ "${MODE}" == release ]]; then
        printf '| Package | Version | License | Location |\n'
        printf '|---------|---------|---------|----------|\n'
    else
        printf '| Package | License | Location |\n'
        printf '|---------|---------|----------|\n'
    fi

    while IFS=, read -r package _ license module version; do
        [[ -z "${package}" ]] && continue

        license_identifier="$(license_identifier_for "${package}" "${license:-Unknown}")"
        relative_license_dir="$(license_dir_within_module "${package}" "${module}")" \
            || die "no license file found for ${package} under ${VENDOR_DIR}/${module}." \
                   "Run 'go mod vendor' and re-run."
        governing_dir="${VENDOR_DIR}/${module}${relative_license_dir:+/${relative_license_dir}}"

        # Assigned before it is printed, so a package with no resolvable license
        # aborts the run rather than rendering an empty Location cell.
        location_markup="$(location_cell "${governing_dir}" "${module}" "${relative_license_dir}")" \
            || die "could not resolve a license location for ${package} (${module})."

        # shellcheck disable=SC2016  # backticks are literal markdown here.
        if [[ "${MODE}" == release ]]; then
            printf '| `%s` | %s | %s | %s |\n' \
                "${package}" "${version}" "${license_identifier}" "${location_markup}"
        else
            printf '| `%s` | %s | %s |\n' \
                "${package}" "${license_identifier}" "${location_markup}"
        fi
    done < "${index}"
}

emit_sections() {
    local index="$1"
    local package _ license module version files license_file fence
    local license_identifier relative_license_dir governing_dir file_name license_path

    while IFS=, read -r package _ license module version; do
        [[ -z "${package}" ]] && continue

        license_identifier="$(license_identifier_for "${package}" "${license:-Unknown}")"
        printf '### %s\n\n' "${package}"
        [[ "${MODE}" == release ]] && printf '* Version: %s\n' "${version}"
        printf '* License: %s\n\n' "${license_identifier}"

        relative_license_dir="$(license_dir_within_module "${package}" "${module}")" \
            || die "no license file found for ${package} under ${VENDOR_DIR}/${module}." \
                   "Run 'go mod vendor' and re-run."
        governing_dir="${VENDOR_DIR}/${module}${relative_license_dir:+/${relative_license_dir}}"

        files=()
        while IFS= read -r license_file; do
            [[ -n "${license_file}" ]] && files+=("${license_file}")
        done < <(license_files_for "${governing_dir}")

        if (( ${#files[@]} == 0 )); then
            printf 'License text unavailable. See upstream source for the full license.\n'
        else
            for license_file in "${files[@]}"; do
                file_name="${license_file##*/}"
                license_path="${relative_license_dir:+${relative_license_dir}/}${file_name}"
                fence="$(fence_for "${license_file}")"
                printf '#### %s\n\n' "${file_name}"
                printf '<%s>\n\n' "$(license_url "${module}" "${license_path}")"
                printf '%stext\n' "${fence}"
                cat "${license_file}"
                echo
                printf '%s\n' "${fence}"
                echo
            done
        fi
        echo
    done < "${index}"
}

compose_document() {
    log "Composing ${OUTPUT}..."
    {
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
EOF

        if [[ "${MODE}" == release ]]; then
            cat <<'EOF'

Each dependency is listed with the version redistributed, and its Location
links to that license file as vendored at the commit this release was built
from, so every link serves the exact text reproduced below it. Where a
dependency ships more than one license-bearing file, such as a PATENTS or
NOTICE alongside its LICENSE, each one is listed and reproduced.
EOF
        else
            cat <<'EOF'

Each dependency's Location links to its license file as vendored in this
repository, so every link serves the exact text reproduced below it. Where
a dependency ships more than one license-bearing file, such as a PATENTS or
NOTICE alongside its LICENSE, each one is listed and reproduced.
EOF
        fi

        # The repo document names the base image in prose because it tracks a
        # moving Dockerfile. A release describes one build, so it states the
        # version that build used, read from the Dockerfile at that commit.
        if [[ "${MODE}" == release ]]; then
            cat <<'EOF'

Software the Dockerfile adds on top of the base image is listed under Bundled
Components below, with its license and corresponding source. NVIDIA's own
components are not third party to NVIDIA and are out of scope here.

EOF
            emit_base_image_table
            emit_bundled_index
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

        cat <<'EOF'

## Dependency Index

EOF
        emit_index_table "${INDEX_FILE}"

        cat <<'EOF'

## License Texts

EOF
        emit_sections "${INDEX_FILE}"

        if [[ "${MODE}" == release ]]; then
            emit_bundled_sections
        fi
    } > "${OUT_TMP}"
    # mktemp creates 0600, so fix the mode before the rename. mv, not cp: the
    # rename is atomic, so a failed run leaves the previous document intact.
    chmod 644 "${OUT_TMP}"
    mv "${OUT_TMP}" "${OUTPUT}"
}

usage() {
    cat >&2 <<'EOF'
Usage:
  generate-third-party-notices.sh repo    [--output FILE]
  generate-third-party-notices.sh release --version VERSION --commit SHA \
                                          [--repo-url URL] [--output FILE]

  repo     the document tracked on main: no versions, cites main
  release  the artifact for one commit: versions, cites that commit
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
        case "$1" in
            --version|--commit|--repo-url)
                [[ "${MODE}" == release || "$1" == --repo-url ]] \
                    || die "$1 is only valid for the release subcommand."
                [[ $# -ge 2 ]] || die "$1 needs a value."
                case "$1" in
                    --version)  RELEASE_VERSION="$2" ;;
                    --commit)   LINK_REF="$2" ;;
                    --repo-url) REPO_URL="$2" ;;
                esac
                shift 2
                ;;
            --output)
                [[ $# -ge 2 ]] || die "--output needs a value."
                OUTPUT="$2"
                output_given=1
                shift 2
                ;;
            *)
                usage
                ;;
        esac
    done

    if [[ "${MODE}" == release ]]; then
        [[ -n "${RELEASE_VERSION}" ]] || die "the release subcommand needs --version."
        # The version reaches a filename and a Markdown table cell, so it must
        # not be able to escape either.
        [[ "${RELEASE_VERSION}" != */* ]] \
            || die "invalid --version '${RELEASE_VERSION}': must not contain '/'."
        # A tag can be re-pointed at another commit later; a commit cannot, so
        # the links are pinned to the commit even when a tag names the release.
        [[ "${LINK_REF}" =~ ^[0-9a-f]{40}$ ]] \
            || die "the release subcommand needs --commit with a full 40-character SHA."
        [[ -n "${output_given}" ]] \
            || OUTPUT="gpu-operator-${RELEASE_VERSION}-THIRD_PARTY_NOTICES.md"
    else
        [[ "${LINK_REF}" == main ]] || die "--commit is only valid for the release subcommand."
    fi
}

main() {
    parse_arguments "$@"

    check_prerequisites
    verify_platform_matrix
    if [[ "${MODE}" == release ]]; then
        resolve_base_image
        check_bundled_coverage
        check_recorded_versions
    fi
    prepare_workspace

    collect_licenses
    build_indexes
    compose_document

    local count
    count=$(wc -l < "${INDEX_FILE}" | tr -d ' ')
    log "Wrote ${OUTPUT} (${count} Go packages)"
}

# Sourced by the tests, which reuse these functions without the side effects of
# a full run.
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    main "$@"
fi
