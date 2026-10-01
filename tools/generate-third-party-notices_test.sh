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

# main() normally creates this in prepare_workspace; the tests call the
# guards directly, so give them somewhere to materialise the copies.
export FINAL_STAGE_COPIES
FINAL_STAGE_COPIES="$(temp_path)"
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

# shellcheck disable=SC2034  # read by the sourced generator, not by this file
MODE=release LINK_REF="${TEST_SHA}" REPO_URL="${TEST_REPO_URL}"
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
assert_eq "1" \
    "$(env LICENSE_OVERRIDES="${stale_overrides}" bash -c \
       'source "$1"; check_no_stale_overrides "$2"' _ \
       "${HERE}/generate-third-party-notices.sh" "${render}/index.csv" 2>&1 >/dev/null \
       | LC_ALL=C grep -c '^WARNING: ')" \
    "check_no_stale_overrides warns when an override names a package absent from the index"

gen="${HERE}/generate-third-party-notices.sh"
assert_fails "no subcommand is rejected" bash "${gen}"
assert_fails "an unknown subcommand is rejected" bash "${gen}" notarealmode
assert_fails "release without --output is rejected" bash "${gen}" release
assert_fails "an unknown flag is rejected on repo" bash "${gen}" repo --nope somevalue
assert_fails "an unknown flag is rejected on release" bash "${gen}" release --nope somevalue
assert_fails "the withdrawn --version flag is rejected" \
    bash "${gen}" release --version v1.2.3 --output "$(temp_path)"
assert_fails "the withdrawn --link-ref flag is rejected" \
    bash "${gen}" release --link-ref v1.2.3 --output "$(temp_path)"
assert_fails "release without --release-tag is rejected" \
    bash "${gen}" release --output "$(temp_path)"
assert_fails "--release-tag on repo is rejected" \
    bash "${gen}" repo --release-tag v1.2.3
assert_fails "a tag name that would truncate markdown links is rejected" \
    bash "${gen}" release --release-tag 'v1.0(beta)' --output "$(temp_path)"

outside_checkout="$(temp_path -d)"
# shellcheck disable=SC2016
assert_fails "release mode refuses to run outside a git checkout" \
    bash -c 'cd "$2" && source "$1" && MODE=release RELEASE_TAG=v9.9.9 resolve_release_provenance' \
    _ "${gen}" "${outside_checkout}"

untagged_checkout="$(temp_path -d)"
git -C "${untagged_checkout}" -c init.defaultBranch=main init -q
git -C "${untagged_checkout}" remote add origin https://github.com/NVIDIA/gpu-operator.git
git -C "${untagged_checkout}" -c user.name=test -c user.email=test@example.invalid \
    -c commit.gpgsign=false commit -q --allow-empty -m 'no tags here'
# shellcheck disable=SC2016
assert_fails "release mode refuses a tag that does not exist" \
    bash -c 'cd "$2" && source "$1" && MODE=release RELEASE_TAG=v9.9.9 resolve_release_provenance' \
    _ "${gen}" "${untagged_checkout}"

git -C "${untagged_checkout}" -c tag.gpgSign=false -c tag.forceSignAnnotated=false tag v9.9.9
resolved="$(bash -c 'cd "$2" && source "$1" \
    && MODE=release RELEASE_TAG=v9.9.9 resolve_release_provenance \
    && printf "%s %s" "${RELEASE_VERSION}" "${LINK_REF}"' _ "${gen}" "${untagged_checkout}")"
assert_eq "v9.9.9 $(git -C "${untagged_checkout}" rev-parse HEAD)" "${resolved}" \
    "the document is titled with the requested tag and its links cite the commit"

git -C "${untagged_checkout}" -c tag.gpgSign=false -c tag.forceSignAnnotated=false tag v9.9.9-final
resolved="$(bash -c 'cd "$2" && source "$1" \
    && MODE=release RELEASE_TAG=v9.9.9 resolve_release_provenance \
    && printf "%s" "${RELEASE_VERSION}"' _ "${gen}" "${untagged_checkout}")"
assert_eq "v9.9.9" "${resolved}" \
    "a second tag on the same commit cannot displace the requested one"

git -C "${untagged_checkout}" -c user.name=test -c user.email=test@example.invalid \
    -c commit.gpgsign=false commit -q --allow-empty -m 'past the tag'
# shellcheck disable=SC2016
assert_fails "release mode refuses a tag that names a different commit" \
    bash -c 'cd "$2" && source "$1" && MODE=release RELEASE_TAG=v9.9.9 resolve_release_provenance' \
    _ "${gen}" "${untagged_checkout}"

apt_pin_fixture() {
    local dir install_line row_version
    dir="$1"; install_line="$2"; row_version="$3"
    mkdir -p "${dir}"
    printf 'FROM debian:trixie-slim AS shell\nRUN apt-get update \\\n && apt-get install -y --no-install-recommends %s \\\n && cp /bin/busybox /busybox\n' \
        "${install_line}" > "${dir}/Dockerfile"
    printf 'busybox\t%s\tGPL-2.0-only\tbusybox/LICENSE\thttps://example.invalid\t-\t-\n' \
        "${row_version}" > "${dir}/rows.tsv"
}

apt_pin_check() {
    # shellcheck disable=SC2016
    bash -c 'source "$1" >/dev/null 2>&1
        DOCKERFILE="$2/Dockerfile"; BUNDLED_ROWS="$2/rows.tsv"
        BUNDLED_COMPONENTS="catalogue.tsv"
        check_apt_pinned_versions' _ "${gen}" "$1"
}

pin_matching="$(temp_path -d)"
apt_pin_fixture "${pin_matching}" "busybox-static=1:1.37.0-6+b9" "1:1.37.0-6+b9"
assert_eq "0" "$(apt_pin_check "${pin_matching}" >/dev/null 2>&1; printf '%s' "$?")" \
    "a Dockerfile pin matching the catalogue is accepted"

pin_mismatched="$(temp_path -d)"
apt_pin_fixture "${pin_mismatched}" "busybox-static=1:1.37.0-7" "1:1.37.0-6+b9"
assert_fails "a Dockerfile pin disagreeing with the catalogue is rejected" \
    apt_pin_check "${pin_mismatched}"

pin_absent="$(temp_path -d)"
apt_pin_fixture "${pin_absent}" "busybox-static" "1:1.37.0-6+b9"
assert_eq "0" "$(apt_pin_check "${pin_absent}" >/dev/null 2>&1; printf '%s' "$?")" \
    "an unpinned package is tolerated, for tags that predate the pin"
assert_eq "1" \
    "$(apt_pin_check "${pin_absent}" 2>&1 >/dev/null | LC_ALL=C grep -c '^WARNING: ')" \
    "an unpinned package warns that the recorded version is unverifiable"

pin_unrelated="$(temp_path -d)"
apt_pin_fixture "${pin_unrelated}" "ca-certificates" "1:1.37.0-6+b9"
assert_eq "0" \
    "$(apt_pin_check "${pin_unrelated}" 2>&1 >/dev/null | LC_ALL=C grep -c '^WARNING: ')" \
    "a package unrelated to any catalogue component is ignored"

weak_license_module="$(temp_path -d)"
mkdir -p "${weak_license_module}/example.com/mod/pkg"
printf 'MIT License\n\nreal terms\n' > "${weak_license_module}/example.com/mod/LICENSE"
printf 'Alice\nBob\n' > "${weak_license_module}/example.com/mod/pkg/AUTHORS"
assert_eq "" \
    "$(VENDOR_DIR="${weak_license_module}" \
       relative_license_dir_within_module example.com/mod/pkg example.com/mod)" \
    "a subdirectory holding only AUTHORS does not end the licence search"
assert_eq "${weak_license_module}/example.com/mod/LICENSE" \
    "$(VENDOR_DIR="${weak_license_module}" \
       governing_license_files_for "${weak_license_module}/example.com/mod")" \
    "the module root's licence is what gets reproduced"

ssh_remote_checkout="$(temp_path -d)"
git -C "${ssh_remote_checkout}" -c init.defaultBranch=main init -q
git -C "${ssh_remote_checkout}" remote add origin git@github.com:abrarshivani/gpu-operator.git
git -C "${ssh_remote_checkout}" -c user.name=test -c user.email=test@example.invalid \
    -c commit.gpgsign=false commit -q --allow-empty -m 'ssh remote'
git -C "${ssh_remote_checkout}" -c tag.gpgSign=false -c tag.forceSignAnnotated=false tag v7.7.7
assert_eq "https://github.com/abrarshivani/gpu-operator" \
    "$(bash -c 'cd "$2" && source "$1" \
        && MODE=release RELEASE_TAG=v7.7.7 resolve_release_provenance \
        && printf "%s" "${REPO_URL}"' _ "${gen}" "${ssh_remote_checkout}")" \
    "an ssh origin is normalised to the https URL the links need"

no_remote_checkout="$(temp_path -d)"
git -C "${no_remote_checkout}" -c init.defaultBranch=main init -q
git -C "${no_remote_checkout}" -c user.name=test -c user.email=test@example.invalid \
    -c commit.gpgsign=false commit -q --allow-empty -m 'no remote'
git -C "${no_remote_checkout}" -c tag.gpgSign=false -c tag.forceSignAnnotated=false tag v6.6.6
# shellcheck disable=SC2016
assert_fails "release mode refuses a checkout with no origin remote" \
    bash -c 'cd "$2" && source "$1" && MODE=release RELEASE_TAG=v6.6.6 resolve_release_provenance' \
    _ "${gen}" "${no_remote_checkout}"

release_header="$(MODE=release RELEASE_VERSION=v26.7.0-152-g9ad814a3a RESOLVED_COMMIT="${TEST_SHA}" \
    emit_header)"
assert_eq "$(printf '# Third-Party Notices\n\nNVIDIA GPU Operator v26.7.0-152-g9ad814a3a\n\nThis file lists every third-party dependency that GPU Operator redistributes,')" \
    "$(printf '%s' "${release_header}" | sed -n '1,5p')" \
    "the release header opens with the title, the release version and the scope paragraph"
assert_eq "0" \
    "$(printf '%s' "${release_header}" | LC_ALL=C grep -c "${TEST_SHA}")" \
    "the release header does not name the commit it was generated from"
assert_eq "$(printf '# Third-Party Notices\n\nNVIDIA GPU Operator\n\nThis file lists every third-party dependency that GPU Operator redistributes,')" \
    "$(MODE=repo emit_header | sed -n '1,5p')" \
    "the repo header opens with the title and the scope paragraph, carrying no version"

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
assert_eq "The base image's own sources are published per version." \
    "$(emit_base_image_table nvcr.io/nvidia/distroless/cc v4.0.6-dev | sed -n 1p)" \
    "emit_base_image_table introduces the table with one sentence"

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

assert_eq "$(printf '/busybox\t/busybox\tshell\n/workspace/gpu-operator\t/usr/bin/\tbuilder\nassets\t/opt/gpu-operator/\t-')" \
    "$(DOCKERFILE="${bundled_dockerfile}" \
       final_stage_copies nvcr.io/nvidia/distroless/cc)" \
    "final_stage_copies reads the final stage's copies and tags each with its source stage"

# shellcheck disable=SC2016
assert_fails "final_stage_copies refuses an empty base repository" \
    env DOCKERFILE="${bundled_dockerfile}" bash -c \
    'source "$1"; final_stage_copies ""' _ "${HERE}/generate-third-party-notices.sh"

bundled_fixture="$(temp_path)"
printf '# source_path\tcomponent\tdisposition\tversion\tlicense_identifier\tlicense_file\tnotices_url\tprovenance_digest\tattribution_note\n' > "${bundled_fixture}"
printf '/workspace/gpu-operator\tgpu-operator\tproject\t-\t-\t-\t-\t-\t-\n' >> "${bundled_fixture}"
printf 'assets\tassets\tproject\t-\t-\t-\t-\t-\t-\n' >> "${bundled_fixture}"
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
provenance_dockerfile="$(temp_path)"
printf 'FROM debian@sha256:%064d AS old-shell\nFROM alpine@sha256:%064d AS new-shell\nFROM nvcr.io/nvidia/distroless/cc:v1@sha256:%064d\nCOPY --from=new-shell /busybox /busybox\n' 1 2 3 > "${provenance_dockerfile}"
provenance_catalogue="$(temp_path)"
printf '/busybox\tbusybox\tthird-party\t1.0\tGPL-2.0-only\tb/L\thttps://x\tsha256:%064d\t-\n' 1 > "${provenance_catalogue}"
# shellcheck disable=SC2016
assert_fails "check_version_provenance rejects a digest from a stage the component is not copied from" \
    env DOCKERFILE="${provenance_dockerfile}" BUNDLED_COMPONENTS="${provenance_catalogue}" bash -c \
    'source "$1"; check_version_provenance nvcr.io/nvidia/distroless/cc' _ "${HERE}/generate-third-party-notices.sh"

# shellcheck disable=SC2016
assert_fails "check_version_provenance fails when the recorded image is no longer built from" \
    env DOCKERFILE="${provenance_dockerfile}" BUNDLED_COMPONENTS="${recorded_fixture}" bash -c \
    'source "$1"; check_version_provenance nvcr.io/nvidia/distroless/cc' _ "${HERE}/generate-third-party-notices.sh"

printf '/busybox\tbusybox\tthird-party\t1.0\tGPL-2.0-only\tb/L\thttps://x\tsha256:%064d\t-\n' 2 > "${recorded_fixture}"
assert_eq "0" \
    "$(DOCKERFILE="${provenance_dockerfile}" BUNDLED_COMPONENTS="${recorded_fixture}" bash -c \
       'source "$1"; check_version_provenance nvcr.io/nvidia/distroless/cc' _ "${HERE}/generate-third-party-notices.sh" >/dev/null 2>&1; echo $?)" \
    "check_version_provenance passes when the digest matches the supplying stage"


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
assert_eq "$(printf '/busybox\t/busybox\tshell')" \
    "$(DOCKERFILE="${stage_leak_dockerfile}" final_stage_copies nvcr.io/nvidia/distroless/cc)" \
    "final_stage_copies ignores an earlier stage built from the same image"

flags_dockerfile="$(temp_path)"
cat > "${flags_dockerfile}" <<'DOCKERFILE'
FROM nvcr.io/nvidia/distroless/cc:v1@sha256:0000000000000000000000000000000000000000000000000000000000000000
COPY --chown=0:0 --from=builder /a /b /usr/bin/
COPY assets /opt/
DOCKERFILE
assert_eq "$(printf '/a\t/usr/bin/\tbuilder\n/b\t/usr/bin/\tbuilder\nassets\t/opt/\t-')" \
    "$(DOCKERFILE="${flags_dockerfile}" final_stage_copies nvcr.io/nvidia/distroless/cc)" \
    "final_stage_copies handles COPY flags and several sources"
case_dockerfile="$(temp_path)"
cat > "${case_dockerfile}" <<'DOCKERFILE'
from debian:trixie-slim@sha256:0000000000000000000000000000000000000000000000000000000000000000 As Shell
FROM nvcr.io/nvidia/distroless/cc:v4.1.4@sha256:1111111111111111111111111111111111111111111111111111111111111111
copy --from=Shell /busybox /busybox
Add manifests /opt/
ADD --from=builder /c /opt/c
DOCKERFILE
assert_eq "$(printf '/busybox\t/busybox\tShell\nmanifests\t/opt/\t-\n/c\t/opt/c\tbuilder')" \
    "$(DOCKERFILE="${case_dockerfile}" final_stage_copies nvcr.io/nvidia/distroless/cc)" \
    "final_stage_copies reads lower-case keywords and treats ADD as a copy"

assert_eq "sha256:0000000000000000000000000000000000000000000000000000000000000000" \
    "$(DOCKERFILE="${case_dockerfile}" stage_image_digest shell)" \
    "stage_image_digest matches a stage named in another case"

lowercase_base_dockerfile="$(temp_path)"
printf 'from golang:1.27.1@sha256:%064d as builder\nFrom nvcr.io/nvidia/distroless/cc:v4.1.4@sha256:%064d\n' 0 1 \
    > "${lowercase_base_dockerfile}"
assert_eq "$(printf 'nvcr.io/nvidia/distroless/cc\tv4.1.4')" \
    "$(DOCKERFILE="${lowercase_base_dockerfile}" base_image_from_dockerfile)" \
    "base_image_from_dockerfile reads a lower-case final FROM"

continuation_dockerfile="$(temp_path)"
cat > "${continuation_dockerfile}" <<'DOCKERFILE'
FROM nvcr.io/nvidia/distroless/cc:v1@sha256:0000000000000000000000000000000000000000000000000000000000000000
COPY --from=builder \
    /a \
    /b \
    /usr/bin/
# a comment between instructions is not part of one
COPY assets /opt/
DOCKERFILE
assert_eq "$(printf '/a\t/usr/bin/\tbuilder\n/b\t/usr/bin/\tbuilder\nassets\t/opt/\t-')" \
    "$(DOCKERFILE="${continuation_dockerfile}" final_stage_copies nvcr.io/nvidia/distroless/cc)" \
    "final_stage_copies joins a backslash-continued COPY"

json_copy_dockerfile="$(temp_path)"
printf 'FROM nvcr.io/nvidia/distroless/cc:v1@sha256:%064d\nCOPY ["/a", "/b"]\n' 0 > "${json_copy_dockerfile}"
# shellcheck disable=SC2016
assert_fails "final_stage_copies refuses a JSON-form COPY rather than skipping it" \
    env DOCKERFILE="${json_copy_dockerfile}" bash -c \
    'source "$1"; final_stage_copies nvcr.io/nvidia/distroless/cc' _ "${HERE}/generate-third-party-notices.sh"

chart_fixture="$(temp_path -d)"
mkdir -p "${chart_fixture}/charts/node-feature-discovery/crds"
chart_crds_path="${chart_fixture}/charts/node-feature-discovery/crds/nfd-api-crds.yaml"
chart_catalogue="$(temp_path)"
printf '%s\tnode-feature-discovery CRDs\tthird-party\tv0.19.0\tApache-2.0\tn/L\thttps://x\t-\t-\n' \
    "${chart_crds_path}" > "${chart_catalogue}"

printf 'apiVersion: v2\nappVersion: v0.20.1\nname: node-feature-discovery\nversion: 0.19.0\n' \
    > "${chart_fixture}/charts/node-feature-discovery/Chart.yaml"
# shellcheck disable=SC2016
assert_fails "check_chart_crd_versions fails when the catalogue lags the chart's appVersion" \
    env BUNDLED_COMPONENTS="${chart_catalogue}" bash -c \
    'source "$1"; check_chart_crd_versions' _ "${HERE}/generate-third-party-notices.sh"
assert_eq "1" \
    "$(BUNDLED_COMPONENTS="${chart_catalogue}" bash -c \
       'source "$1"; check_chart_crd_versions' _ "${HERE}/generate-third-party-notices.sh" 2>&1 >/dev/null \
       | LC_ALL=C grep -c 'records node-feature-discovery CRDs at v0.19.0, but .*Chart.yaml declares appVersion v0.20.1')" \
    "check_chart_crd_versions names both versions"

printf 'apiVersion: v2\nappVersion: "v0.19.0"\nname: node-feature-discovery\nversion: 0.19.0\n' \
    > "${chart_fixture}/charts/node-feature-discovery/Chart.yaml"
assert_eq "0" \
    "$(BUNDLED_COMPONENTS="${chart_catalogue}" bash -c \
       'source "$1"; check_chart_crd_versions' _ "${HERE}/generate-third-party-notices.sh" >/dev/null 2>&1; echo $?)" \
    "check_chart_crd_versions passes when the catalogue matches the chart's appVersion"

printf 'apiVersion: v2\nname: node-feature-discovery\nversion: 0.19.0\n' \
    > "${chart_fixture}/charts/node-feature-discovery/Chart.yaml"
# shellcheck disable=SC2016
assert_fails "check_chart_crd_versions fails when the chart declares no appVersion" \
    env BUNDLED_COMPONENTS="${chart_catalogue}" bash -c \
    'source "$1"; check_chart_crd_versions' _ "${HERE}/generate-third-party-notices.sh"

assert_eq "0" \
    "$(check_chart_crd_versions >/dev/null 2>&1; echo $?)" \
    "the catalogue in the tree agrees with every vendored chart it cites"


# A value inherited from the environment counts as set, so set -u cannot catch a
# provenance variable the release path failed to derive for itself.
inheritance_checkout="$(temp_path -d)"
git -C "${inheritance_checkout}" -c init.defaultBranch=main init -q
git -C "${inheritance_checkout}" remote add origin https://github.com/NVIDIA/gpu-operator.git
git -C "${inheritance_checkout}" -c user.name=test -c user.email=test@example.invalid \
    -c commit.gpgsign=false commit -q --allow-empty -m 'tagged release'
git -C "${inheritance_checkout}" -c tag.gpgSign=false -c tag.forceSignAnnotated=false tag v8.8.8

inherited_document="$(temp_path)"
# shellcheck disable=SC2016
RELEASE_VERSION=bogus-inherited-version \
LINK_REF=bogus-inherited-ref \
RESOLVED_COMMIT=bogus-inherited-commit \
RELEASE_TAG=bogus-inherited-tag \
DOCKERFILE="${bundled_dockerfile}" \
BUNDLED_COMPONENTS="${bundled_fixture}" \
LICENSE_TEXTS_DIR="${license_texts_fixture}" \
VENDOR_DIR="${vendor_fixture}" \
RESOLVED_FIXTURE="${render}/resolved.tsv" \
bash -c '
    cd "$2" || exit 1
    source "$1"
    check_prerequisites() { :; }
    verify_platform_matrix() { :; }
    collect_licenses() { :; }
    build_index() { : > "${INDEX_FILE}"; }
    resolve_index_rows() { cat "${RESOLVED_FIXTURE}"; }
    main release --release-tag v8.8.8 --output "$3"
' _ "${gen}" "${inheritance_checkout}" "${inherited_document}" >/dev/null 2>&1

assert_eq "1" \
    "$(LC_ALL=C grep -c '^NVIDIA GPU Operator v8\.8\.8$' "${inherited_document}")" \
    "release mode names the requested tag, not an inherited RELEASE_VERSION"
assert_eq "/blob/$(git -C "${inheritance_checkout}" rev-parse HEAD)/vendor/" \
    "$(LC_ALL=C grep -o '/blob/[^/]*/vendor/' "${inherited_document}" | LC_ALL=C sort -u)" \
    "every release link cites the checked-out commit, not an inherited LINK_REF"
assert_eq "0" \
    "$(LC_ALL=C grep -c 'bogus-inherited' "${inherited_document}")" \
    "no inherited provenance value reaches the released document"


finish
