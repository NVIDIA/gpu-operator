#!/bin/bash

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source ${SCRIPT_DIR}/.definitions.sh

echo "Enabling GPU Operator operands"
kubectl label nodes --all "nvidia.com/gpu.deploy.operands=true" --overwrite