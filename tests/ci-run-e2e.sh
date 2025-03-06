#!/bin/bash

set -xe

if [[ $# -ne 6 ]]; then
	echo "OPERATOR_IMAGE, OPERATOR_VERSION, VALIDATOR_IMAGE, VALIDATOR_VERSION, GPU_PRODUCT_NAME, TEST_CASE are required"
	exit 1
fi

export OPERATOR_IMAGE=${1}
export OPERATOR_VERSION=${2}
export VALIDATOR_IMAGE=${3}
export VALIDATOR_VERSION=${4}
export GPU_PRODUCT_NAME=${5}
export TEST_CASE=${6}

TEST_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

${TEST_DIR}/local.sh
