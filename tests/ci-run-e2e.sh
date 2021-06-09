#! /bin/bash

if [[ $# -ne 2 ]]; then
	echo "Operator image and version are required"
	exit 1
fi

export OPERATOR_IMAGE=${1}
export OPERATOR_VERSION=${2}

TEST_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source ${TEST_DIR}/scripts/.definitions.sh

# We ensure that the prerequisites are installed
${SCRIPT_DIR}/prerequisites.sh

# We run the default test case
${CASES_DIR}/defaults.sh

exit $rc
