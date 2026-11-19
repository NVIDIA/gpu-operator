#!/usr/bin/env bash
# Copyright (c) NVIDIA CORPORATION. All rights reserved.
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#     http://www.apache.org/licenses/LICENSE-2.0
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
chart="$root/deployments/gpu-operator"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

render_ok() {
  helm template gpu-test "$chart" "$@" >"$tmp/chart.yaml" 2>"$tmp/error.log" || {
    cat "$tmp/error.log" >&2
    return 1
  }
}

render_guard() {
  if helm template gpu-test "$chart" "$@" >"$tmp/chart.yaml" 2>"$tmp/error.log"; then
    echo "Expected OpenShift RBAC validation to stop rendering: $*" >&2
    return 1
  fi
  if ! grep -q 'OpenShift detected, but platform.openshift=false' "$tmp/error.log"; then
    cat "$tmp/error.log" >&2
    return 1
  fi
}

# An API group can exist without serving the ClusterVersion resource.
# That is Kubernetes to the runtime detector and must render successfully.
render_ok --set platform.openshift=false
render_ok --api-versions config.openshift.io/v1 --set platform.openshift=false
# A served ClusterVersion Kind is evidence the OpenShift permissions are needed.
render_guard --api-versions config.openshift.io/v1/ClusterVersion --set platform.openshift=false
render_guard --api-versions config.openshift.io/v1 --api-versions config.openshift.io/v1/ClusterVersion --set platform.openshift=false
render_ok --api-versions config.openshift.io/v1/ClusterVersion --set platform.openshift=true
echo 'OpenShift chart capability validation passed'
