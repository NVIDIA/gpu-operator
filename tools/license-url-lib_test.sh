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
# shellcheck source=tools/license-url-lib.sh disable=SC1091
source "${HERE}/license-url-lib.sh"

# The bug that silently broke proxy resolution for all ten uppercase modules.
assert_eq "github.com/!n!v!i!d!i!a/go-nvlib" \
    "$(proxy_escape github.com/NVIDIA/go-nvlib)" "proxy_escape lowercases with a bang"
assert_eq "github.com/!mellanox/maintenance-operator/api" \
    "$(proxy_escape github.com/Mellanox/maintenance-operator/api)" "proxy_escape single capital"
assert_eq "k8s.io/api" "$(proxy_escape k8s.io/api)" "proxy_escape leaves lowercase alone"

assert_eq "github.com/Masterminds/semver" \
    "$(strip_major_suffix github.com/Masterminds/semver/v3)" "strip /v3"
assert_eq "go.yaml.in/yaml" \
    "$(strip_major_suffix go.yaml.in/yaml/v3)" "strip /v3 from a vanity path"
assert_eq "gopkg.in/inf.v0" \
    "$(strip_major_suffix gopkg.in/inf.v0)" "gopkg.in .vN is not a /vN suffix"

assert_eq "v2.0.1" "$(normalize_version 'v2.0.1+incompatible')" "strip +incompatible"
assert_eq "faa5f7b0171c" \
    "$(pseudo_version_hash v0.0.0-20250102033503-faa5f7b0171c)" "pseudo-version hash"
assert_eq "d8f796af33cc" \
    "$(pseudo_version_hash v1.1.2-0.20180830191138-d8f796af33cc)" "pre-release pseudo-version"
assert_eq "" "$(pseudo_version_hash v1.19.1)" "a tagged version has no hash"

assert_eq "https://github.com/klauspost/compress" \
    "$(github_repo_from_path github.com/klauspost/compress)" "github root module"
assert_eq "https://github.com/Mellanox/maintenance-operator" \
    "$(github_repo_from_path github.com/Mellanox/maintenance-operator/api)" "github submodule"
assert_eq "" "$(github_repo_from_path k8s.io/api)" "non-github yields empty"

# Without this the github fallback loses the submodule tag prefix entirely.
assert_eq "api" \
    "$(github_subdir_from_path github.com/Mellanox/maintenance-operator/api)" "github subdir"
assert_eq "pkg/apis/monitoring" \
    "$(github_subdir_from_path github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring)" \
    "multi-segment github subdir"
assert_eq "" "$(github_subdir_from_path github.com/Masterminds/semver/v3)" "/vN is not a subdir"
assert_eq "" "$(github_subdir_from_path github.com/klauspost/compress)" "root module has no subdir"

assert_eq "https://github.com/go-inf/inf" "$(gopkg_in_repo gopkg.in/inf.v0)" "gopkg.in single segment"
assert_eq "https://github.com/go-yaml/yaml" "$(gopkg_in_repo gopkg.in/yaml.v3)" "gopkg.in yaml"
assert_eq "https://github.com/evanphx/json-patch" \
    "$(gopkg_in_repo gopkg.in/evanphx/json-patch.v4)" "gopkg.in user/pkg"
assert_eq "" "$(gopkg_in_repo github.com/foo/bar)" "non-gopkg.in yields empty"

assert_eq "https://github.com/cyphar/libpathrs" \
    "$(normalize_repo_url 'https://github.com/cyphar/libpathrs.git')" "strip .git"
assert_eq "https://github.com/foo/bar" \
    "$(normalize_repo_url 'https://github.com/foo/bar/')" "strip trailing slash"

assert_eq "api" "$(derived_subdir sigs.k8s.io/kustomize/api sigs.k8s.io/kustomize)" "subdir from prefix"
assert_eq "" "$(derived_subdir github.com/klauspost/compress github.com/klauspost/compress)" "root module"

assert_eq "https://github.com/klauspost/compress/blob/v1.19.1/LICENSE" \
    "$(blob_url https://github.com/klauspost/compress v1.19.1 LICENSE)" "github blob template"
assert_eq "https://go.googlesource.com/crypto/+/refs/tags/v0.55.0/LICENSE" \
    "$(blob_url https://go.googlesource.com/crypto refs/tags/v0.55.0 LICENSE)" "gerrit blob template"

assert_eq "https://raw.githubusercontent.com/klauspost/compress/v1.19.1/zstd/internal/xxhash/LICENSE.txt" \
    "$(raw_url_for https://github.com/klauspost/compress/blob/v1.19.1/zstd/internal/xxhash/LICENSE.txt)" \
    "github raw URL"
assert_eq "https://go.googlesource.com/crypto/+/refs/tags/v0.55.0/LICENSE?format=TEXT" \
    "$(raw_url_for https://go.googlesource.com/crypto/+/refs/tags/v0.55.0/LICENSE)" "gerrit raw URL"
assert_fails "github raw is not base64" raw_is_base64 https://github.com/a/b/blob/v1/LICENSE

assert_eq "hello" "$(printf 'aGVsbG8=' | base64_decode)" "base64_decode works on this host"

finish
