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

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
readonly SCRIPT_DIR
readonly NODE_OPERATIONS="${SCRIPT_DIR}/node-operations.sh"

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

if [[ -z "${NODE_SSH_HOST:-}" ]]; then
	echo "Running '$*' locally"
	exec bash "${NODE_OPERATIONS}" "$@"
fi

require_readable_file "NODE_SSH_KEY" "${NODE_SSH_KEY:-}"
require_readable_file "NODE_SSH_KNOWN_HOSTS" "${NODE_SSH_KNOWN_HOSTS:-}"

REMOTE_ARGS=""
if (( $# )); then
	printf -v REMOTE_ARGS ' %q' "$@"
fi

echo "Running '$*' on ${NODE_SSH_HOST}"
ssh -i "${NODE_SSH_KEY}" \
	-o BatchMode=yes \
	-o IdentitiesOnly=yes \
	-o ConnectTimeout=30 \
	-o StrictHostKeyChecking=accept-new \
	-o UserKnownHostsFile="${NODE_SSH_KNOWN_HOSTS}" \
	"${NODE_SSH_HOST}" \
	"bash -s --${REMOTE_ARGS}" < "${NODE_OPERATIONS}"
