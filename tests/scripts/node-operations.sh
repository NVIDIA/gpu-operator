#!/usr/bin/env bash

# Copyright NVIDIA CORPORATION
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -euo pipefail

# This script runs ON the node hosting the cluster. It is either executed
# locally or streamed over SSH stdin via `bash -s --`, which means it MUST stay
# fully self-contained: it must not source sibling scripts such as
# .definitions.sh and must not refer to any path relative to the repository,
# since the repository is not guaranteed to exist on the node.

usage() {
	cat <<'EOF'
Usage: node-operations.sh <operation> [args...]

Operations:
  load-modules
      Load the kernel modules required by the GPU Operator.

  restart-operator-container <runtime>
      Kill the running gpu-operator container so that kubernetes restarts it.
      Supported runtimes: containerd, docker.
EOF
}

load_modules() {
	echo "Load kernel modules i2c_core and ipmi_msghandler"
	sudo modprobe -a i2c_core ipmi_msghandler
}

# The x-prefixed comparisons and the container selection pipelines below are
# kept as they were in tests/scripts/checks.sh so that the behaviour of the
# restart test does not change.
# shellcheck disable=SC2268
restart_operator_container() {
	local runtime="${1:-}"
	local container_id=""

	if [[ x"${runtime}" == x"containerd" ]]; then
		# The operator is the only container that has the string '"gpu-operator"'
		# TODO: This requires permissions on containerd.sock
		container_id="$(sudo crictl ps --name gpu-operator | awk '{if(NR>1)print $1}')" || true
		if [[ -z "${container_id}" ]]; then
			echo "Error: no running gpu-operator container found via crictl" >&2
			return 1
		fi
		sudo crictl rm --force "${container_id}"
	elif [[ x"${runtime}" == x"docker" ]]; then
		# The operator is the only container that has the string '"gpu-operator"'
		container_id="$(docker ps --format '{{.ID}} {{.Command}}' | grep "gpu-operator" | cut -f 1 -d ' ')" || true
		if [[ -z "${container_id}" ]]; then
			echo "Error: no running gpu-operator container found via docker" >&2
			return 1
		fi
		docker kill "${container_id}"
	else
		echo "Error: unknown runtime '${runtime}'. Supported runtimes: containerd, docker" >&2
		return 1
	fi
}

main() {
	if [[ $# -lt 1 ]]; then
		usage >&2
		exit 2
	fi

	local operation="${1}"
	shift

	case "${operation}" in
		load-modules)
			load_modules "$@"
			;;
		restart-operator-container)
			restart_operator_container "$@"
			;;
		*)
			echo "Error: unknown operation '${operation}'" >&2
			usage >&2
			exit 2
			;;
	esac
}

main "$@"
