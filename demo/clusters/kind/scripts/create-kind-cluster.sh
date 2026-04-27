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

set -ex
set -o pipefail

source "${CURRENT_DIR}/common.sh"

worker_exec() {
  docker exec "${KIND_WORKER_NODE}" bash -ceu "$1"
}

kind create cluster \
  --retain \
  --name "${KIND_CLUSTER_NAME}" \
  --image "${KIND_IMAGE}" \
  --config "${KIND_CLUSTER_CONFIG_PATH}"

# Unmount the masked /proc/driver/nvidia to allow
# dynamically generated MIG devices to be discovered
worker_exec "umount -R /proc/driver/nvidia || true"

# Install the nvidia-container-toolkit.
# TODO: Once kind supports a more standard GPU runtime bootstrap, we can remove this.
worker_exec "\
  apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates curl gnupg2 \
  && curl -fsSL https://nvidia.github.io/libnvidia-container/gpgkey | \
    gpg --dearmor -o /usr/share/keyrings/nvidia-container-toolkit-keyring.gpg \
  && curl -s -L https://nvidia.github.io/libnvidia-container/stable/deb/nvidia-container-toolkit.list | \
    sed 's#deb https://#deb [signed-by=/usr/share/keyrings/nvidia-container-toolkit-keyring.gpg] https://#g' | \
    tee /etc/apt/sources.list.d/nvidia-container-toolkit.list \
  && apt-get update \
  && apt-get install -y nvidia-container-toolkit \
  && ln -sf /sbin/ldconfig /sbin/ldconfig.real"

# Configure containerd with an NVIDIA runtime handler for the operator's
# RuntimeClass and enable CDI support for workload injection.
worker_exec "\
  nvidia-ctk config --in-place --set nvidia-container-runtime.modes.cdi.annotation-prefixes=nvidia.cdi.k8s.io/ \
  && nvidia-ctk runtime configure --runtime=containerd --cdi.enabled \
  && systemctl restart containerd"
