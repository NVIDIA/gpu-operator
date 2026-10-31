#! /bin/bash
# This test case runs the operator installation / test case with the default options.

SCRIPTS_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )"/../scripts && pwd )"
source "${SCRIPTS_DIR}"/.definitions.sh

if [[ "${USE_GINKGO:-}" == "true" ]]; then
    exec "${SCRIPTS_DIR}"/run-ginkgo.sh nvidia-driver
fi

# Run an end-to-end test cycle
"${SCRIPTS_DIR}"/end-to-end-nvidia-driver.sh
