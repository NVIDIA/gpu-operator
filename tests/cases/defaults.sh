#! /bin/bash
# This test case runs the operator installation / test case with the default options.

SCRIPTS_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )"/../scripts && pwd )"
source ${SCRIPTS_DIR}/.definitions.sh

# Run an end-to-end test cycle
${SCRIPT_DIR}/end-to-end.sh
