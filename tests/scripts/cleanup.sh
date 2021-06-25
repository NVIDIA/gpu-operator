#! /bin/bash

if [[ "${SKIP_CLEANUP}" == "true" ]]; then
    echo "Skipping cleanup: SKIP_CLEANUP=${SKIP_CLEANUP}"
    exit 0
fi

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source ${SCRIPT_DIR}/.definitions.sh

${TERRAFORM} destroy -var "legacy_setup=false" -var "container_runtime=${CONTAINER_RUNTIME}"
