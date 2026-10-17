#! /bin/bash
# This test case runs the Ginkgo port of the nvidia-driver test case.

SCRIPTS_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )"/../scripts && pwd )"

"${SCRIPTS_DIR}"/run-ginkgo.sh nvidia-driver
