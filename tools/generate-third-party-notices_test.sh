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

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=tools/test-helpers.sh disable=SC1091
source "${HERE}/test-helpers.sh"

TEST_TMPDIR="$(mktemp -d)"
trap 'rm -rf "${TEST_TMPDIR}"' EXIT

temp_path() {
    if [[ "${1:-}" == "-d" ]]; then
        mktemp -d "${TEST_TMPDIR}/tmp.XXXXXX"
    else
        mktemp "${TEST_TMPDIR}/tmp.XXXXXX"
    fi
}

OUTPUT="$(temp_path)"
export OUTPUT

# shellcheck source=tools/generate-third-party-notices.sh disable=SC1091
source "${HERE}/generate-third-party-notices.sh"
set +e

assert_eq "" "$(cat "${OUTPUT}")" "sourcing the generator writes no document"

assert_eq "1" \
    "$(LC_ALL=C grep -c 'BASH_SOURCE\[0\]' "${HERE}/generate-third-party-notices.sh")" \
    "the generator guards main against running on source"

fixture="$(temp_path)"
printf 'plain text, no backticks\n' > "${fixture}"
assert_eq '```' "$(fence_for "${fixture}")" "fence_for: minimum width is three"
printf 'a ```` b\n' > "${fixture}"
assert_eq '`````' "$(fence_for "${fixture}")" "fence_for: one wider than the longest run"

modules_fixture="$(temp_path)"
cat > "${modules_fixture}" <<'MODULES'
# github.com/klauspost/compress v1.19.1
## explicit
# k8s.io/api v0.36.4
MODULES

index_input="$(temp_path)"
cat > "${index_input}" <<'ROWS'
github.com/klauspost/compress,ignored,Apache-2.0
github.com/klauspost/compress/zstd/internal/xxhash,ignored,MIT
k8s.io/api,ignored,Apache-2.0
ROWS

assert_eq "github.com/klauspost/compress/zstd/internal/xxhash,ignored,MIT,github.com/klauspost/compress,v1.19.1" \
    "$(MODULES_TXT="${modules_fixture}" annotate_modules < "${index_input}" | sed -n 2p)" \
    "annotate_modules appends module and version"
assert_eq "k8s.io/api,ignored,Apache-2.0,k8s.io/api,v0.36.4" \
    "$(MODULES_TXT="${modules_fixture}" annotate_modules < "${index_input}" | sed -n 3p)" \
    "annotate_modules resolves a root module"

empty_overrides_fixture="$(temp_path)"
printf '# no overrides\n' > "${empty_overrides_fixture}"

license_files_fixture="$(temp_path -d)"
touch "${license_files_fixture}/LICENSE" "${license_files_fixture}/LICENSE.md" "${license_files_fixture}/license.go"
assert_eq "$(printf '%s/LICENSE\n%s/LICENSE.md' "${license_files_fixture}" "${license_files_fixture}")" \
    "$(license_files_for "${license_files_fixture}")" \
    "license_files_for excludes a Go source file even when its name matches"

vendor_fixture="$(temp_path -d)"
mkdir -p "${vendor_fixture}/github.com/klauspost/compress/zstd/internal/xxhash"
touch "${vendor_fixture}/github.com/klauspost/compress/LICENSE"
touch "${vendor_fixture}/github.com/klauspost/compress/zstd/internal/xxhash/LICENSE.txt"
assert_eq "zstd/internal/xxhash" \
    "$(VENDOR_DIR="${vendor_fixture}" relative_license_dir_within_module \
        github.com/klauspost/compress/zstd/internal/xxhash github.com/klauspost/compress)" \
    "relative_license_dir_within_module finds the nearest enclosing license"
assert_eq "" \
    "$(VENDOR_DIR="${vendor_fixture}" relative_license_dir_within_module \
        github.com/klauspost/compress github.com/klauspost/compress)" \
    "relative_license_dir_within_module is empty at the module root"
# shellcheck disable=SC2016
assert_fails "relative_license_dir_within_module fails when no license exists" \
    env VENDOR_DIR="${vendor_fixture}" bash -c \
    'source "$1"; relative_license_dir_within_module github.com/absent/mod github.com/absent/mod' \
    _ "${HERE}/generate-third-party-notices.sh"

render="$(temp_path -d)"
mkdir -p "${render}/cache/github.com/klauspost/compress/zstd/internal/xxhash"
printf 'MIT text\n' > "${render}/cache/github.com/klauspost/compress/zstd/internal/xxhash/LICENSE.txt"
cat > "${render}/index.csv" <<'IDX'
github.com/klauspost/compress/zstd/internal/xxhash,ignored,MIT,github.com/klauspost/compress,v1.19.1
IDX

printf '%s\t%s\t%s\t%s\t%s\t%s\n' \
    github.com/klauspost/compress/zstd/internal/xxhash v1.19.1 MIT \
    github.com/klauspost/compress \
    "${vendor_fixture}/github.com/klauspost/compress/zstd/internal/xxhash" \
    zstd/internal/xxhash > "${render}/resolved.tsv"
printf '%s\t%s\t%s\t%s\t%s\t%s\n' \
    github.com/klauspost/compress/zstd/internal/xxhash v1.19.1 "Apache-2.0 AND MIT" \
    github.com/klauspost/compress \
    "${vendor_fixture}/github.com/klauspost/compress/zstd/internal/xxhash" \
    zstd/internal/xxhash > "${render}/resolved-override.tsv"

TEST_REPO_URL="https://github.com/NVIDIA/gpu-operator"
TEST_SHA="0123456789abcdef0123456789abcdef01234567"

MODE=release LINK_REF="${TEST_SHA}"
assert_eq '| Package | Version | License | Location |' \
    "$(VENDOR_DIR="${vendor_fixture}" LICENSE_OVERRIDES="${empty_overrides_fixture}" \
       emit_index_table "${render}/resolved.tsv" | sed -n 1p)" \
    "release index header has four columns"
# shellcheck disable=SC2016
assert_eq "| \`github.com/klauspost/compress/zstd/internal/xxhash\` | v1.19.1 | MIT | [LICENSE.txt](${TEST_REPO_URL}/blob/${TEST_SHA}/vendor/github.com/klauspost/compress/zstd/internal/xxhash/LICENSE.txt) |" \
    "$(VENDOR_DIR="${vendor_fixture}" LICENSE_OVERRIDES="${empty_overrides_fixture}" \
       emit_index_table "${render}/resolved.tsv" | sed -n 3p)" \
    "release index cites the given link ref and the version"

MODE=repo LINK_REF=main
assert_eq '| Package | License | Location |' \
    "$(VENDOR_DIR="${vendor_fixture}" LICENSE_OVERRIDES="${empty_overrides_fixture}" \
       emit_index_table "${render}/resolved.tsv" | sed -n 1p)" \
    "repo index header has three columns"
# shellcheck disable=SC2016
assert_eq "| \`github.com/klauspost/compress/zstd/internal/xxhash\` | MIT | [LICENSE.txt](vendor/github.com/klauspost/compress/zstd/internal/xxhash/LICENSE.txt) |" \
    "$(VENDOR_DIR="${vendor_fixture}" LICENSE_OVERRIDES="${empty_overrides_fixture}" \
       emit_index_table "${render}/resolved.tsv" | sed -n 3p)" \
    "repo index links relatively and omits the version"

assert_eq "0" \
    "$(MODE=repo VENDOR_DIR="${vendor_fixture}" LICENSE_OVERRIDES="${empty_overrides_fixture}" \
       emit_index_table "${render}/resolved.tsv" | LC_ALL=C grep -c 'https://')" \
    "repo index emits no absolute link"

repo_section="$(MODE=repo LINK_REF=main VENDOR_DIR="${vendor_fixture}" \
    LICENSE_OVERRIDES="${empty_overrides_fixture}" emit_sections "${render}/resolved.tsv")"
assert_eq "0" "$(printf '%s' "${repo_section}" | LC_ALL=C grep -c '^\* Version: ')" \
    "repo section states no version"

MODE=release LINK_REF="${TEST_SHA}"

section="$(MODE=release LINK_REF="${TEST_SHA}" VENDOR_DIR="${vendor_fixture}" \
    LICENSE_OVERRIDES="${empty_overrides_fixture}" emit_sections "${render}/resolved.tsv")"
assert_eq "* Version: v1.19.1" "$(printf '%s' "${section}" | sed -n 3p)" "release section names the version"
assert_eq "* License: MIT" "$(printf '%s' "${section}" | sed -n 4p)" "section names the license"
assert_eq "0" "$(printf '%s' "${section}" | LC_ALL=C grep -c '^\* Module: ')" "section no longer names the module"
assert_eq "<${TEST_REPO_URL}/blob/${TEST_SHA}/vendor/github.com/klauspost/compress/zstd/internal/xxhash/LICENSE.txt>" \
    "$(printf '%s' "${section}" | LC_ALL=C grep -m1 '^<http')" "section prints the pinned file URL"

overrides_fixture="$(temp_path)"
cat > "${overrides_fixture}" <<'OVERRIDES'
# package	license	reason
github.com/klauspost/compress	Apache-2.0 AND MIT	test fixture
github.com/klauspost/compress/zstd/internal/xxhash	Apache-2.0 AND MIT	test fixture
OVERRIDES

assert_eq "Apache-2.0 AND MIT" \
    "$(LICENSE_OVERRIDES="${overrides_fixture}" license_identifier_for github.com/klauspost/compress Apache-2.0)" \
    "license_identifier_for returns the override for a package that has one"
assert_eq "MIT" \
    "$(LICENSE_OVERRIDES="${overrides_fixture}" license_identifier_for k8s.io/api MIT)" \
    "license_identifier_for returns the passed-in default for a package without an override"

# shellcheck disable=SC2016
assert_eq "| \`github.com/klauspost/compress/zstd/internal/xxhash\` | v1.19.1 | Apache-2.0 AND MIT | [LICENSE.txt](${TEST_REPO_URL}/blob/${TEST_SHA}/vendor/github.com/klauspost/compress/zstd/internal/xxhash/LICENSE.txt) |" \
    "$(MODE=release LINK_REF="${TEST_SHA}" \
       emit_index_table "${render}/resolved-override.tsv" | sed -n 3p)" \
    "emit_index_table renders the identifier the resolver produced"

empty_override="$(temp_path)"
printf 'k8s.io/api\t\treason\n' > "${empty_override}"
# shellcheck disable=SC2016
assert_fails "an empty override identifier is rejected" \
    env LICENSE_OVERRIDES="${empty_override}" bash -c \
    'source "$1"; license_identifier_for k8s.io/api MIT' _ "${HERE}/generate-third-party-notices.sh"

stale_overrides="$(temp_path)"
printf 'github.com/absent/package\tApache-2.0 / MIT\ttest fixture\n' > "${stale_overrides}"
# shellcheck disable=SC2016
assert_fails "check_no_stale_overrides fails when an override names a package absent from the index" \
    env LICENSE_OVERRIDES="${stale_overrides}" bash -c \
    'source "$1"; check_no_stale_overrides "$2"' _ "${HERE}/generate-third-party-notices.sh" "${render}/index.csv"

gen="${HERE}/generate-third-party-notices.sh"
assert_fails "no subcommand is rejected" bash "${gen}"
assert_fails "an unknown subcommand is rejected" bash "${gen}" notarealmode
assert_fails "release without --version is rejected" bash "${gen}" release
assert_fails "--version on repo is rejected" bash "${gen}" repo --version v1.2.3
assert_fails "--link-ref on repo is rejected" bash "${gen}" repo --link-ref v1.2.3
assert_fails "release without --link-ref is rejected" bash "${gen}" release --version v1.2.3
assert_fails "release without --output is rejected" \
    bash "${gen}" release --version v1.2.3 --link-ref v1.2.3
assert_fails "release with a path-shaped link ref is rejected" \
    bash "${gen}" release --version v1.2.3 --link-ref refs/heads/main
assert_fails "a version containing a path separator is rejected" \
    bash "${gen}" release --version ../../etc/passwd
assert_fails "an unknown flag is rejected" bash "${gen}" repo --nope somevalue

assert_eq "0" \
    "$(LC_ALL=C grep -c 'RELEASE_VERSION:-' "${gen}")" \
    "RELEASE_VERSION is never read from the environment"

dockerfile_fixture="$(temp_path)"
cat > "${dockerfile_fixture}" <<'DOCKERFILE'
FROM golang:1.27.1@sha256:aaaabbbbccccddddeeeeffff00001111222233334444555566667777888899990 AS builder
FROM nvcr.io/nvidia/distroless/cc:v4.1.4@sha256:b1deb9e97732ab5da2f1f6e0d3ac1e56bdc2df0a5bf900180662a4740f05957c
DOCKERFILE
assert_eq "$(printf 'nvcr.io/nvidia/distroless/cc\tv4.1.4')" \
    "$(DOCKERFILE="${dockerfile_fixture}" base_image_from_dockerfile)" \
    "base_image_from_dockerfile takes the final FROM, not a builder stage"

assert_eq "1" \
    "$(emit_base_image_table nvcr.io/nvidia/distroless/cc v4.0.6-dev \
       | LC_ALL=C grep -c 'distroless-oss/cc/v4.0.6/index.html')" \
    "emit_base_image_table strips a -dev suffix for the source index"
# shellcheck disable=SC2016  # the backticks are literal markdown being matched.
assert_eq "1" \
    "$(emit_base_image_table nvcr.io/nvidia/distroless/cc v4.0.6-dev \
       | LC_ALL=C grep -c '`v4.0.6-dev`')" \
    "emit_base_image_table still reports the tag actually used"

interpolated_dockerfile="$(temp_path)"
# shellcheck disable=SC2016  # the ARG reference must reach the fixture unexpanded.
printf 'ARG CUDA_VERSION=13.2\nFROM nvcr.io/nvidia/distroless/cc:${CUDA_VERSION}@sha256:%064d\n' 0 > "${interpolated_dockerfile}"
# shellcheck disable=SC2016
assert_fails "an ARG-interpolated base image tag is refused" \
    env DOCKERFILE="${interpolated_dockerfile}" bash -c \
    'source "$1"; base_image_from_dockerfile' _ "${HERE}/generate-third-party-notices.sh"

undigested_dockerfile="$(temp_path)"
printf 'FROM nvcr.io/nvidia/distroless/cc:v4.1.4\n' > "${undigested_dockerfile}"
# shellcheck disable=SC2016
assert_fails "a base image without a digest is refused" \
    env DOCKERFILE="${undigested_dockerfile}" bash -c \
    'source "$1"; base_image_from_dockerfile' _ "${HERE}/generate-third-party-notices.sh"

foreign_dockerfile="$(temp_path)"
printf 'FROM registry.access.redhat.com/ubi9/ubi:latest@sha256:%064d\n' 0 > "${foreign_dockerfile}"
# shellcheck disable=SC2016
assert_fails "a non-distroless base image is refused" \
    env DOCKERFILE="${foreign_dockerfile}" bash -c \
    'source "$1"; base_image_from_dockerfile' _ "${HERE}/generate-third-party-notices.sh"

rm -f "${dockerfile_fixture}" "${interpolated_dockerfile}" \
      "${undigested_dockerfile}" "${foreign_dockerfile}"

bundled_dockerfile="$(temp_path)"
cat > "${bundled_dockerfile}" <<'DOCKERFILE'
ARG CUDA_SAMPLES_VERSION=12.9
FROM debian:trixie-slim@sha256:0000000000000000000000000000000000000000000000000000000000000000 AS shell
FROM nvcr.io/nvidia/distroless/cc:v4.1.4@sha256:1111111111111111111111111111111111111111111111111111111111111111
COPY --from=shell /busybox /busybox
COPY --from=builder /workspace/gpu-operator /usr/bin/
COPY assets /opt/gpu-operator/
DOCKERFILE

assert_eq "12.9" \
    "$(DOCKERFILE="${bundled_dockerfile}" dockerfile_arg_default CUDA_SAMPLES_VERSION)" \
    "dockerfile_arg_default reads an ARG default"
# shellcheck disable=SC2016  # the ARG reference must reach the function unexpanded.
assert_eq "/usr/local/cuda-12.9/compat" \
    "$(DOCKERFILE="${bundled_dockerfile}" expand_dockerfile_args '/usr/local/cuda-${CUDA_SAMPLES_VERSION}/compat')" \
    "expand_dockerfile_args substitutes a declared ARG"
# shellcheck disable=SC2016
assert_fails "expand_dockerfile_args refuses an ARG with no default" \
    env DOCKERFILE="${bundled_dockerfile}" bash -c \
    'source "$1"; expand_dockerfile_args '"'"'${UNDECLARED_ARG}'"'"'' _ "${HERE}/generate-third-party-notices.sh"

assert_eq "$(printf '/busybox\t/busybox\n/workspace/gpu-operator\t/usr/bin/')" \
    "$(DOCKERFILE="${bundled_dockerfile}" \
       final_stage_copies nvcr.io/nvidia/distroless/cc)" \
    "final_stage_copies reads only the final stage's COPY --from lines"

# shellcheck disable=SC2016
assert_fails "final_stage_copies refuses an empty base repository" \
    env DOCKERFILE="${bundled_dockerfile}" bash -c \
    'source "$1"; final_stage_copies ""' _ "${HERE}/generate-third-party-notices.sh"

bundled_fixture="$(temp_path)"
printf '# source_path\tcomponent\tdisposition\tversion\tlicense_identifier\tlicense_file\tnotices_url\tprovenance_digest\tattribution_note\n' > "${bundled_fixture}"
printf '/workspace/gpu-operator\tgpu-operator\tproject\t-\t-\t-\t-\t-\t-\n' >> "${bundled_fixture}"
# shellcheck disable=SC2016
assert_fails "check_bundled_coverage fails when a copied path has no row" \
    env DOCKERFILE="${bundled_dockerfile}" BUNDLED_COMPONENTS="${bundled_fixture}" \
    BUNDLED_ROWS=/dev/null bash -c \
    'source "$1"; check_bundled_coverage nvcr.io/nvidia/distroless/cc' _ "${HERE}/generate-third-party-notices.sh"

printf '/busybox\tbusybox\tthird-party\t1:1.37.0-6\tGPL-2.0-only\tabsent/LICENSE\thttps://example.invalid/src\t-\t-\n' >> "${bundled_fixture}"
license_texts_fixture="$(temp_path -d)"
mkdir -p "${license_texts_fixture}/absent"
touch "${license_texts_fixture}/absent/LICENSE"
assert_eq "0" \
    "$(DOCKERFILE="${bundled_dockerfile}" BUNDLED_COMPONENTS="${bundled_fixture}" \
       LICENSE_TEXTS_DIR="${license_texts_fixture}" BUNDLED_ROWS="${render}/b1.tsv" bash -c \
       'source "$1"; third_party_bundled_rows nvcr.io/nvidia/distroless/cc > "${BUNDLED_ROWS}"; check_bundled_coverage nvcr.io/nvidia/distroless/cc && check_bundled_rows' _ "${HERE}/generate-third-party-notices.sh" >/dev/null 2>&1; echo $?)" \
    "check_bundled_coverage passes once every copied path has a row"

# shellcheck disable=SC2016
assert_fails "check_bundled_rows fails when the named licence text is missing" \
    env DOCKERFILE="${bundled_dockerfile}" BUNDLED_COMPONENTS="${bundled_fixture}" \
    LICENSE_TEXTS_DIR="${render}/no-licenses" BUNDLED_ROWS="${render}/b3.tsv" bash -c \
    'source "$1"; third_party_bundled_rows nvcr.io/nvidia/distroless/cc > "${BUNDLED_ROWS}"; check_bundled_rows' _ "${HERE}/generate-third-party-notices.sh"

recorded_fixture="$(temp_path)"
printf '/busybox\tbusybox\tthird-party\t1:1.37.0-6\tGPL-2.0-only\tbusybox/LICENSE\thttps://example.invalid/copyright\tsha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\t\n' > "${recorded_fixture}"
# shellcheck disable=SC2016
assert_fails "check_version_provenance fails when the recorded image is no longer built from" \
    env DOCKERFILE="${bundled_dockerfile}" BUNDLED_COMPONENTS="${recorded_fixture}" bash -c \
    'source "$1"; check_version_provenance' _ "${HERE}/generate-third-party-notices.sh"

printf '/busybox\tbusybox\tthird-party\t1:1.37.0-6\tGPL-2.0-only\tbusybox/LICENSE\thttps://example.invalid/copyright\tsha256:0000000000000000000000000000000000000000000000000000000000000000\t\n' > "${recorded_fixture}"
assert_eq "0" \
    "$(DOCKERFILE="${bundled_dockerfile}" BUNDLED_COMPONENTS="${recorded_fixture}" bash -c \
       'source "$1"; check_version_provenance' _ "${HERE}/generate-third-party-notices.sh" >/dev/null 2>&1; echo $?)" \
    "check_version_provenance passes while the recorded image is still in the Dockerfile"


shape_fixture="$(temp_path)"
printf '/busybox\tbusybox\tthird-party\t\tGPL-2.0-only\tb/LICENSE\thttps://x\t-\t-\n' > "${shape_fixture}"
# shellcheck disable=SC2016
assert_fails "catalogue shape check rejects an empty field" \
    env BUNDLED_COMPONENTS="${shape_fixture}" bash -c \
    'source "$1"; check_bundled_catalogue_shape' _ "${HERE}/generate-third-party-notices.sh"
printf '/b\tb\tfirst-party\t-\t-\t-\t-\t-\t-\n/b\tb\tthird-party\t1\tMIT\tb/L\thttps://x\t-\t-\n' > "${shape_fixture}"
# shellcheck disable=SC2016
rows_fixture="$(temp_path)"
for missing_field in 2 3 4 5; do
    LC_ALL=C awk -v i="${missing_field}" 'BEGIN {
        OFS = "\t"
        split("busybox 1.0 GPL-2.0 busybox/LICENSE https://example.invalid /busybox -", f, " ")
        f[i] = "-"
        print f[1], f[2], f[3], f[4], f[5], f[6], f[7]
    }' > "${rows_fixture}"
    # shellcheck disable=SC2016
    assert_fails "check_bundled_rows rejects a row missing field ${missing_field}" \
        env BUNDLED_ROWS="${rows_fixture}" LICENSE_TEXTS_DIR=tools/licenses bash -c \
        'source "$1"; check_bundled_rows' _ "${HERE}/generate-third-party-notices.sh"
done

# shellcheck disable=SC2016
assert_fails "duplicate catalogue keys are rejected" \
    env BUNDLED_COMPONENTS="${shape_fixture}" bash -c \
    'source "$1"; check_no_duplicate_bundled_keys' _ "${HERE}/generate-third-party-notices.sh"

# shellcheck disable=SC2016
assert_fails "check_bundled_coverage fails when given no base repository" \
    env DOCKERFILE="${bundled_dockerfile}" BUNDLED_COMPONENTS="${bundled_fixture}" \
    BUNDLED_ROWS=/dev/null bash -c \
    'source "$1"; check_bundled_coverage ""' _ "${HERE}/generate-third-party-notices.sh"

stage_leak_dockerfile="$(temp_path)"
cat > "${stage_leak_dockerfile}" <<'DOCKERFILE'
FROM nvcr.io/nvidia/distroless/cc:v1@sha256:1111111111111111111111111111111111111111111111111111111111111111 AS early
COPY --from=builder /intermediate /intermediate
FROM nvcr.io/nvidia/distroless/cc:v2@sha256:3333333333333333333333333333333333333333333333333333333333333333
COPY --from=shell /busybox /busybox
DOCKERFILE
assert_eq "$(printf '/busybox\t/busybox')" \
    "$(DOCKERFILE="${stage_leak_dockerfile}" final_stage_copies nvcr.io/nvidia/distroless/cc)" \
    "final_stage_copies ignores an earlier stage built from the same image"

flags_dockerfile="$(temp_path)"
cat > "${flags_dockerfile}" <<'DOCKERFILE'
FROM nvcr.io/nvidia/distroless/cc:v1@sha256:0000000000000000000000000000000000000000000000000000000000000000
COPY --chown=0:0 --from=builder /a /b /usr/bin/
COPY assets /opt/
DOCKERFILE
assert_eq "$(printf '/a\t/usr/bin/\n/b\t/usr/bin/')" \
    "$(DOCKERFILE="${flags_dockerfile}" final_stage_copies nvcr.io/nvidia/distroless/cc)" \
    "final_stage_copies handles COPY flags and several sources"



finish
