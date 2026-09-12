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

# If the guard ever regresses, sourcing must not overwrite the committed
# notices file. OUTPUT is honoured by compose_document.
OUTPUT="$(mktemp)"
export OUTPUT

# shellcheck source=tools/generate-third-party-notices.sh disable=SC1091
source "${HERE}/generate-third-party-notices.sh"

# If the guard is missing, sourcing runs the generator and exits before here.
assert_eq "sourced" "sourced" "sourcing the generator does not execute main"

# Environment-independent: proves the guard is present rather than relying on
# main failing fast, which it only does on a host without go-licenses.
assert_eq "1" \
    "$(LC_ALL=C grep -c 'BASH_SOURCE\[0\]' "${HERE}/generate-third-party-notices.sh")" \
    "the generator guards main against running on source"

fixture="$(mktemp)"
trap 'rm -f "${fixture}"' EXIT
printf 'plain text, no backticks\n' > "${fixture}"
assert_eq '```' "$(fence_for "${fixture}")" "fence_for: minimum width is three"
printf 'a ```` b\n' > "${fixture}"
assert_eq '`````' "$(fence_for "${fixture}")" "fence_for: one wider than the longest run"

modules_fixture="$(mktemp)"
cat > "${modules_fixture}" <<'MODULES'
# github.com/klauspost/compress v1.19.1
## explicit
# k8s.io/api v0.36.4
MODULES

index_input="$(mktemp)"
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

# No rows: exercises license_identifier_for's not-found path, so the fixtures
# below that do not care about overrides are unaffected by them.
empty_overrides_fixture="$(mktemp)"
printf '# no overrides\n' > "${empty_overrides_fixture}"

license_files_fixture="$(mktemp -d)"
touch "${license_files_fixture}/LICENSE" "${license_files_fixture}/LICENSE.md" "${license_files_fixture}/license.go"
assert_eq "$(printf '%s/LICENSE\n%s/LICENSE.md' "${license_files_fixture}" "${license_files_fixture}")" \
    "$(license_files_for "${license_files_fixture}")" \
    "license_files_for excludes a Go source file even when its name matches"
rm -rf "${license_files_fixture}"

vendor_fixture="$(mktemp -d)"
mkdir -p "${vendor_fixture}/github.com/klauspost/compress/zstd/internal/xxhash"
touch "${vendor_fixture}/github.com/klauspost/compress/LICENSE"
touch "${vendor_fixture}/github.com/klauspost/compress/zstd/internal/xxhash/LICENSE.txt"
assert_eq "zstd/internal/xxhash" \
    "$(VENDOR_DIR="${vendor_fixture}" license_dir_within_module \
        github.com/klauspost/compress/zstd/internal/xxhash github.com/klauspost/compress)" \
    "license_dir_within_module finds the nearest enclosing license"
assert_eq "" \
    "$(VENDOR_DIR="${vendor_fixture}" license_dir_within_module \
        github.com/klauspost/compress github.com/klauspost/compress)" \
    "license_dir_within_module is empty at the module root"
# $1 is expanded by the child bash -c, not here.
# shellcheck disable=SC2016
assert_fails "license_dir_within_module fails when no license exists" \
    env VENDOR_DIR="${vendor_fixture}" bash -c \
    'source "$1"; license_dir_within_module github.com/absent/mod github.com/absent/mod' \
    _ "${HERE}/generate-third-party-notices.sh"

render="$(mktemp -d)"
mkdir -p "${render}/cache/github.com/klauspost/compress/zstd/internal/xxhash"
printf 'MIT text\n' > "${render}/cache/github.com/klauspost/compress/zstd/internal/xxhash/LICENSE.txt"
cat > "${render}/index.csv" <<'IDX'
github.com/klauspost/compress/zstd/internal/xxhash,ignored,MIT,github.com/klauspost/compress,v1.19.1
IDX

TEST_REPO_URL="https://github.com/NVIDIA/gpu-operator"
TEST_SHA="0123456789abcdef0123456789abcdef01234567"

MODE=release LINK_REF="${TEST_SHA}"
assert_eq '| Package | Version | License | Location |' \
    "$(VENDOR_DIR="${vendor_fixture}" LICENSE_OVERRIDES="${empty_overrides_fixture}" \
       emit_index_table "${render}/index.csv" | sed -n 1p)" \
    "release index header has four columns"
# Expected literal Markdown, not shell expansion.
# shellcheck disable=SC2016
assert_eq "| \`github.com/klauspost/compress/zstd/internal/xxhash\` | v1.19.1 | MIT | [LICENSE.txt](${TEST_REPO_URL}/blob/${TEST_SHA}/vendor/github.com/klauspost/compress/zstd/internal/xxhash/LICENSE.txt) |" \
    "$(VENDOR_DIR="${vendor_fixture}" LICENSE_OVERRIDES="${empty_overrides_fixture}" \
       emit_index_table "${render}/index.csv" | sed -n 3p)" \
    "release index pins the link to the commit, not a tag"

MODE=repo LINK_REF=main
assert_eq '| Package | License | Location |' \
    "$(VENDOR_DIR="${vendor_fixture}" LICENSE_OVERRIDES="${empty_overrides_fixture}" \
       emit_index_table "${render}/index.csv" | sed -n 1p)" \
    "repo index header has three columns"
# shellcheck disable=SC2016
assert_eq "| \`github.com/klauspost/compress/zstd/internal/xxhash\` | MIT | [LICENSE.txt](${TEST_REPO_URL}/blob/main/vendor/github.com/klauspost/compress/zstd/internal/xxhash/LICENSE.txt) |" \
    "$(VENDOR_DIR="${vendor_fixture}" LICENSE_OVERRIDES="${empty_overrides_fixture}" \
       emit_index_table "${render}/index.csv" | sed -n 3p)" \
    "repo index cites main and omits the version"

repo_section="$(MODE=repo LINK_REF=main VENDOR_DIR="${vendor_fixture}" \
    LICENSE_OVERRIDES="${empty_overrides_fixture}" emit_sections "${render}/index.csv")"
assert_eq "0" "$(printf '%s' "${repo_section}" | LC_ALL=C grep -c '^\* Version: ')" \
    "repo section states no version"

MODE=release LINK_REF="${TEST_SHA}"

section="$(MODE=release LINK_REF="${TEST_SHA}" VENDOR_DIR="${vendor_fixture}" \
    LICENSE_OVERRIDES="${empty_overrides_fixture}" emit_sections "${render}/index.csv")"
assert_eq "* Version: v1.19.1" "$(printf '%s' "${section}" | sed -n 3p)" "release section names the version"
assert_eq "* License: MIT" "$(printf '%s' "${section}" | sed -n 4p)" "section names the license"
assert_eq "0" "$(printf '%s' "${section}" | LC_ALL=C grep -c '^\* Module: ')" "section no longer names the module"
assert_eq "<${TEST_REPO_URL}/blob/${TEST_SHA}/vendor/github.com/klauspost/compress/zstd/internal/xxhash/LICENSE.txt>" \
    "$(printf '%s' "${section}" | LC_ALL=C grep -m1 '^<http')" "section prints the pinned file URL"

overrides_fixture="$(mktemp)"
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

# Expected literal Markdown, not shell expansion.
# shellcheck disable=SC2016
assert_eq "| \`github.com/klauspost/compress/zstd/internal/xxhash\` | v1.19.1 | Apache-2.0 AND MIT | [LICENSE.txt](${TEST_REPO_URL}/blob/${TEST_SHA}/vendor/github.com/klauspost/compress/zstd/internal/xxhash/LICENSE.txt) |" \
    "$(MODE=release LINK_REF="${TEST_SHA}" VENDOR_DIR="${vendor_fixture}" \
       LICENSE_OVERRIDES="${overrides_fixture}" emit_index_table "${render}/index.csv" | sed -n 3p)" \
    "emit_index_table renders the overridden identifier in the License column"

stale_overrides="$(mktemp)"
printf 'github.com/absent/package\tApache-2.0 / MIT\ttest fixture\n' > "${stale_overrides}"
# $1/$2 are expanded by the child bash -c, not here.
# shellcheck disable=SC2016
assert_fails "check_override_coverage fails when an override names a package absent from the index" \
    env LICENSE_OVERRIDES="${stale_overrides}" bash -c \
    'source "$1"; check_override_coverage "$2"' _ "${HERE}/generate-third-party-notices.sh" "${render}/index.csv"

# The subcommand is the whole guard against release-shaped output reaching the
# committed document, so each way of getting it wrong must fail rather than
# silently pick a mode.
gen="${HERE}/generate-third-party-notices.sh"
assert_fails "no subcommand is rejected" bash "${gen}"
assert_fails "an unknown subcommand is rejected" bash "${gen}" notarealmode
assert_fails "release without --version is rejected" bash "${gen}" release
assert_fails "--version on repo is rejected" bash "${gen}" repo --version v1.2.3
assert_fails "--commit on repo is rejected" bash "${gen}" repo --commit 0123456789abcdef0123456789abcdef01234567
assert_fails "release without --commit is rejected" bash "${gen}" release --version v1.2.3
assert_fails "release with a short SHA is rejected" bash "${gen}" release --version v1.2.3 --commit abc1234
assert_fails "a version containing a path separator is rejected" \
    bash "${gen}" release --version ../../etc/passwd
assert_fails "an unknown flag is rejected" bash "${gen}" repo --nope

# An inherited RELEASE_VERSION must not reach the mode decision: that is exactly
# the leak the subcommand replaced.
assert_eq "0" \
    "$(LC_ALL=C grep -c 'RELEASE_VERSION:-' "${gen}")" \
    "RELEASE_VERSION is never read from the environment"

rm -rf "${vendor_fixture}" "${render}"
rm -f "${modules_fixture}" "${index_input}" "${empty_overrides_fixture}" "${overrides_fixture}" "${stale_overrides}"

finish
