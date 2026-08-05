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

# This script dispatches a host-mutating operation to the node hosting the
# cluster. When NODE_SSH_HOST is set the operation is streamed to the node over
# SSH; otherwise it is executed locally, which is the developer path where the
# tests already run on the node itself.

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
NODE_OPERATIONS="${SCRIPT_DIR}/node-operations.sh"

usage() {
	cat <<'EOF'
Usage: node-exec.sh <operation> [args...]

Runs one of the node-operations.sh operations on the node hosting the cluster.

Environment:
  NODE_SSH_HOST         user@host of the node, e.g. ubuntu@ec2-1-2-3-4.compute.amazonaws.com.
                        If unset or empty the operation is executed locally.
  NODE_SSH_KEY          Path to the private key. Required when NODE_SSH_HOST is set.
  NODE_SSH_KNOWN_HOSTS  Path to the known_hosts file. Required when NODE_SSH_HOST is set.

Operations:
  load-modules
  restart-operator-container <runtime>
EOF
}

require_readable_file() {
	local name="${1}"
	local value="${2}"

	if [[ -z "${value}" ]]; then
		echo "Error: ${name} must be set when NODE_SSH_HOST is set" >&2
		exit 1
	fi
	if [[ ! -r "${value}" ]]; then
		echo "Error: ${name} '${value}' does not exist or is not readable" >&2
		exit 1
	fi
}

if [[ $# -lt 1 ]]; then
	usage >&2
	exit 2
fi

if [[ ! -r "${NODE_OPERATIONS}" ]]; then
	echo "Error: ${NODE_OPERATIONS} does not exist or is not readable" >&2
	exit 1
fi

if [[ -z "${NODE_SSH_HOST:-}" ]]; then
	echo "Running '$*' locally"
	bash "${NODE_OPERATIONS}" "$@"
	exit $?
fi

require_readable_file "NODE_SSH_KEY" "${NODE_SSH_KEY:-}"
require_readable_file "NODE_SSH_KNOWN_HOSTS" "${NODE_SSH_KNOWN_HOSTS:-}"

# Quote each argument so that it survives the remote shell.
REMOTE_COMMAND="bash -s --"
for arg in "$@"; do
	REMOTE_COMMAND+=" $(printf '%q' "${arg}")"
done

echo "Running '$*' on ${NODE_SSH_HOST}"
ssh -i "${NODE_SSH_KEY}" \
	-o BatchMode=yes \
	-o ConnectTimeout=30 \
	-o StrictHostKeyChecking=accept-new \
	-o UserKnownHostsFile="${NODE_SSH_KNOWN_HOSTS}" \
	"${NODE_SSH_HOST}" \
	"${REMOTE_COMMAND}" < "${NODE_OPERATIONS}"
