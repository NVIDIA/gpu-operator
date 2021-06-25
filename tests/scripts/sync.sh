#! /bin/bash

if [[ "${SKIP_SYNC}" == "true" ]]; then
    echo "Skipping sync: SKIP_SYNC=${SKIP_SYNC}"
    exit 0
fi

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source ${SCRIPT_DIR}/.definitions.sh

source ${SCRIPT_DIR}/.local.sh

# TODO: Create an exclude file for this instead
rsync -e "ssh -i ${private_key} -o StrictHostKeyChecking=no" \
    -avz --delete \
        --exclude="vendor/" --exclude=".git" --exclude="aws-kube-ci" \
        ${@}
