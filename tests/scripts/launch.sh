#! /bin/bash

if [[ "${SKIP_LAUNCH}" == "true" ]]; then
    echo "Skipping launch: SKIP_LAUNCH=${SKIP_LAUNCH}"
    exit 0
fi

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source ${SCRIPT_DIR}/.definitions.sh

${TERRAFORM} plan
${TERRAFORM} apply
${TERRAFORM} output