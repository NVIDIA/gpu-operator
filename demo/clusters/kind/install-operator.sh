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

usage () {
  echo 'USAGE:'
  echo './install-operator.sh [option]'
  echo 'where [option] is one of local, gdrcopy, release, template, template-release'
  exit 1
}

CURRENT_DIR="$(cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd)"
source "${CURRENT_DIR}/scripts/common.sh"

# setting default
# but these can be overridden by environment variables
if [[ -z $1 ]]; then
  usage
elif [[ $1 == 'local' ]]; then
:  ${TARGET_CHART:="${PROJECT_DIR}/deployments/gpu-operator"}
:  ${TARGET_ACTION:="upgrade -i"}
:  ${XTRA_OPTS:="--wait"}
elif [[ $1 == 'gdrcopy' ]]; then
:  ${TARGET_CHART:="${PROJECT_DIR}/deployments/gpu-operator"}
:  ${TARGET_ACTION:="upgrade -i"}
:  ${XTRA_OPTS:="--wait --set gdrcopy.enabled=true"}
elif [[ $1 == 'release' ]]; then
:  ${TARGET_CHART:="nvidia/gpu-operator"}
:  ${TARGET_ACTION:="upgrade -i"}
:  ${XTRA_OPTS:="--wait"}
elif [[ $1 == 'template' ]]; then
:  ${TARGET_CHART:="${PROJECT_DIR}/deployments/gpu-operator"}
:  ${TARGET_ACTION:="template"}
:  ${XTRA_OPTS:="--output-dir /tmp/gpu-operator"}
elif [[ $1 == 'template-release' ]]; then
:  ${TARGET_CHART:="nvidia/gpu-operator"}
:  ${TARGET_ACTION:="template"}
:  ${XTRA_OPTS:="--output-dir /tmp/gpu-operator-release"}
else
  echo unknown usage "$0 $@"
  usage
fi

set -ex
set -o pipefail

#kubectl label node "${KIND_CLUSTER_NAME}-worker" --overwrite nvidia.com/gpu.present=true

helm ${TARGET_ACTION} \
  --set cdi.enabled=true \
  --set driver.enabled=false \
  --set operator.runtimeClass=nvidia \
  --set toolkit.enabled=true \
  --set validator.driver.env[0].name="DISABLE_DEV_CHAR_SYMLINK_CREATION" \
  --set-string validator.driver.env[0].value="true" \
  --namespace gpu-operator --create-namespace \
  ${XTRA_OPTS} \
  nvidia-gpu-operator \
  ${TARGET_CHART}

  #--set runtimeClassName=nvidia \

set +x
printf '\033[0;32m'
echo "$TARGET_ACTION complete:"
kubectl get pod -n gpu-operator
printf '\033[0m'
