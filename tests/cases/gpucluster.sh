#! /bin/bash

SCRIPTS_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )"/../scripts && pwd )"

exec "${SCRIPTS_DIR}"/run-ginkgo.sh gpucluster
