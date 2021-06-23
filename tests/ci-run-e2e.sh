#!/bin/bash

set -xe

if [[ $# -ne 2 ]]; then
	echo "Operator image and version are required"
	exit 1
fi

export OPERATOR_IMAGE=${1}
export OPERATOR_VERSION=${2}

TEST_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

${TEST_DIR}/local.sh
