#! /bin/bash

# This test cases configures the nvidia-experimental runtime as the default runtime for
# Docker containers.

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )"/../scripts && pwd )"
source ${SCRIPT_DIR}/.definitions.sh

TOOLKIT_CONTAINER_NVIDIA_RUNTIME="nvidia-experimental"
export TOOLKIT_CONTAINER_OPTIONS="--set toolkit.env[0].name=DOCKER_RUNTIME_NAME --set toolkit.env[0].value=\"${TOOLKIT_CONTAINER_NVIDIA_RUNTIME}\""
export TOOLKIT_CONTAINER_IMAGE="registry.gitlab.com/elezar/container-config/staging/container-toolkit:e0723e8a-ubuntu18.04"

# Run an end-to-end test cycle
${SCRIPT_DIR}/end-to-end.sh
