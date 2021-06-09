# !/bin/bash

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source ${SCRIPT_DIR}/.definitions.sh

if [[ -z ${instance_hostname} ]]; then
export instance_hostname=$(${TERRAFORM} output -raw instance_hostname)
fi

if [[ -z ${private_key} ]]; then
export private_key=$(${TERRAFORM} output -raw private_key)
fi

ssh -i ${private_key} ${instance_hostname} "${@}"