#! /bin/bash

if [[ "${SKIP_SYNC}" == "true" ]]; then
    echo "Skipping sync: SKIP_SYNC=${SKIP_SYNC}"
    exit 0
fi

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source ${SCRIPT_DIR}/.definitions.sh

instance_hostname=$(${TERRAFORM} output -raw instance_hostname)
private_key=$(${TERRAFORM} output -raw private_key)

REMOTE_PROJECT_FOLDER="~/${PROJECT}"
# TODO: Create an exclude file for this instead
# Copy over the contents of the project folder
rsync -e "ssh -i ${private_key} -o StrictHostKeyChecking=no" \
    -avz --delete \
        --exclude="vendor/" --exclude=".git" --exclude="aws-kube-ci" \
        "${PROJECT_DIR}/" \
        "${instance_hostname}:${REMOTE_PROJECT_FOLDER}" \

