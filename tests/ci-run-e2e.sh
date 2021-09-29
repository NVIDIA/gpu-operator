#!/bin/bash

set -xe

if [[ $# -ne 4 ]]; then
	echo "Operator image and version are required"
	exit 1
fi

export OPERATOR_IMAGE=${1}
export OPERATOR_VERSION=${2}
export VALIDATOR_IMAGE=${3}
export VALIDATOR_VERSION=${4}

TEST_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

${TEST_DIR}/local.sh
