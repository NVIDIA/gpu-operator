# !/bin/bash

if [[ "${SKIP_PREREQUISITES}" == "true" ]]; then
    echo "Skipping prerequisites: SKIP_PREREQUISITES=${SKIP_PREREQUISITES}"
    exit 0
fi

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source ${SCRIPT_DIR}/.definitions.sh

echo "Create log dir ${LOG_DIR}"
mkdir -p "${LOG_DIR}"

export DEBIAN_FRONTEND=noninteractive

echo "Load kernel modules i2c_core and ipmi_msghandler"
sudo modprobe -a i2c_core ipmi_msghandler

echo "Install dependencies"
sudo apt update && sudo apt install -y jq

echo "Install Helm"
curl https://raw.githubusercontent.com/helm/helm/master/scripts/get-helm-3 | bash
