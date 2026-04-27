#!/usr/bin/env bash

# Copyright 2023 The Kubernetes Authors.
# Copyright 2023 NVIDIA CORPORATION.
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

# A reference to the current directory where this script is located
CURRENT_DIR="$(cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd)"

set -e
set -o pipefail

source "${CURRENT_DIR}/scripts/common.sh"

add_repo () {
  REPO_COUNT=$(helm repo list | awk 'NR > 1 && $1 == "nvidia" {count++} END {print count+0}')
  if [[ ${REPO_COUNT} < 1 ]]; then
    helm repo add nvidia https://helm.ngc.nvidia.com/nvidia
    helm repo update
  fi
}

clear_old_cluster () {
  NUM=$(kind get clusters | grep -Fxc "${KIND_CLUSTER_NAME}" || true)
  if [[ ${NUM} == 1 ]]; then
    ./delete-cluster.sh
  elif [[ ${NUM} -gt 1 ]]; then
    echo 'too many clusters debug'
    kind get clusters
    exit 1
  else
    echo 'no clusters to clear'
  fi
}

create_cluster () {
  clear_old_cluster
  add_repo
  ./create-cluster.sh
}

exec_local () {
  create_cluster
  ./install-operator.sh local
}

exec_gdrcopy () {
  create_cluster
  ./install-operator.sh gdrcopy
}

exec_release () {
  create_cluster
  ./install-operator.sh release
}

exec_bare () {
  create_cluster
  echo 'As this is a bare-cluster we will end here instead of installing the operator and the gpu-pod'
  exit 0
}

wait_for_daemonset () {
  TARGET_NAMESPACE=$1
  TARGET_DAEMONSET=$2
  kubectl rollout status --timeout=180s -n "${TARGET_NAMESPACE}" "daemonset/${TARGET_DAEMONSET}"
}

usage () {
  echo './demo.sh [CHOICE]'
  echo 'where [CHOICE] is one of "bare", "release", "local", or "gdrcopy"'
  exit 1
}

demo () {
  if [[ -z $1 ]]; then
    usage
  elif [[ $1 == 'release' ]]; then
    exec_release
  elif [[ $1 == 'local' ]]; then
    exec_local
  elif [[ $1 == 'gdrcopy' ]]; then
    exec_gdrcopy
  elif [[ $1 == 'bare' ]]; then
    exec_bare
  else
    echo 'unrecognized option'
    usage
  fi
  wait_for_daemonset gpu-operator nvidia-container-toolkit-daemonset
  wait_for_daemonset gpu-operator nvidia-device-plugin-daemonset
  kubectl apply -f gpu-pod.yml
  sleep 3
  kubectl get pod gpu-pod
}

time demo "$@"
