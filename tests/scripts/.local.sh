#!/usr/env bash

if [[ -z "${instance_hostname}" && -z ${IS_REMOTE} ]]; then
instance_hostname=$(${TERRAFORM} output -raw instance_hostname)
fi

if [[ -z "${private_key}" && -z ${IS_REMOTE} ]]; then
private_key=$(${TERRAFORM} output -raw private_key)
fi

function remote() {
    ${SCRIPT_DIR}/remote.sh "cd ${PROJECT} && "$@""
}
