#! /bin/bash

# This test cases configures the nvidia-experimental runtime as the default runtime for
# Docker containers.

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )"/../scripts && pwd )"
source ${SCRIPT_DIR}/.definitions.sh

TOOLKIT_CONTAINER_NVIDIA_RUNTIME="nvidia-experimental"

if [[ "${CONTAINER_RUNTIME}" == "containerd" ]]; then
export TOOLKIT_CONTAINER_OPTIONS="--set toolkit.env[0].name=CONTAINERD_RUNTIME_CLASS --set toolkit.env[0].value=${TOOLKIT_CONTAINER_NVIDIA_RUNTIME}"
TOOLKIT_CONTAINER_TAG="f6bc90f9-ubuntu18.04"
else
# Assume docker as the container runtime
export TOOLKIT_CONTAINER_OPTIONS="--set toolkit.env[0].name=DOCKER_RUNTIME_NAME --set toolkit.env[0].value=${TOOLKIT_CONTAINER_NVIDIA_RUNTIME}"
TOOLKIT_CONTAINER_TAG="d83e85bb-ubuntu18.04"
fi

TOOLKIT_CONTAINER_REGISTRY="registry.gitlab.com/elezar/container-config/staging"
export TOOLKIT_CONTAINER_IMAGE="${TOOLKIT_CONTAINER_REGISTRY}/container-toolkit:${TOOLKIT_CONTAINER_TAG}"

# Run an end-to-end test cycle
${SCRIPT_DIR}/end-to-end.sh
