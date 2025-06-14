#!/bin/bash

set -xe

if [[ $# -ne 4 ]]; then
	echo "OPERATOR_IMAGE, OPERATOR_VERSION, GPU_PRODUCT_NAME, TEST_CASE are required"
	exit 1
fi

export OPERATOR_IMAGE=${1}
export OPERATOR_VERSION=${2}
export GPU_PRODUCT_NAME=${3}
export TEST_CASE=${4}

TEST_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

${TEST_DIR}/local.sh
