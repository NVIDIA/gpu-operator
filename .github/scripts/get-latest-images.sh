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

readonly COMPONENT_SOURCES='{
    "toolkit": {
        "imageRepository": "ghcr.io/nvidia/container-toolkit",
        "githubRepository": "NVIDIA/nvidia-container-toolkit",
        "githubBranch": "main"
    },
    "device-plugin": {
        "imageRepository": "ghcr.io/nvidia/k8s-device-plugin",
        "githubRepository": "NVIDIA/k8s-device-plugin",
        "githubBranch": "main"
    },
    "mig-manager": {
        "imageRepository": "ghcr.io/nvidia/k8s-mig-manager",
        "githubRepository": "NVIDIA/mig-parted",
        "githubBranch": "main"
    },
    "driver-manager": {
        "imageRepository": "ghcr.io/nvidia/k8s-driver-manager",
        "githubRepository": "NVIDIA/k8s-driver-manager",
        "githubBranch": "main"
    },
    "vgpu-device-manager": {
        "imageRepository": "ghcr.io/nvidia/vgpu-device-manager",
        "githubRepository": "NVIDIA/vgpu-device-manager",
        "githubBranch": "main"
    },
    "kata-sandbox-device-plugin": {
        "imageRepository": "ghcr.io/nvidia/nvidia-sandbox-device-plugin",
        "githubRepository": "NVIDIA/sandbox-device-plugin",
        "githubBranch": "main"
    },
    "kubevirt-gpu-device-plugin": {
        "imageRepository": "ghcr.io/nvidia/kubevirt-gpu-device-plugin",
        "githubRepository": "NVIDIA/kubevirt-gpu-device-plugin",
        "githubBranch": "master"
    },
    "cc-manager": {
        "imageRepository": "ghcr.io/nvidia/k8s-cc-manager",
        "githubRepository": "NVIDIA/k8s-cc-manager",
        "githubBranch": "main",
        "tagSuffix": "-distroless"
    }
}'
VALID_COMPONENTS=$(jq -r 'keys_unsorted | join(" ")' <<< "${COMPONENT_SOURCES}")
readonly VALID_COMPONENTS
readonly MAX_RETRIES=5

if [[ $# -eq 0 ]]; then
    echo "Usage: $0 <component> [<component>...]" >&2
    echo "Valid components: ${VALID_COMPONENTS}" >&2
    exit 1
fi

if ! command -v regctl &> /dev/null; then
    echo "Error: regctl not found. Please install regctl first." >&2
    exit 1
fi

get_latest_image() {
    local component=$1

    local component_source
    if ! component_source=$(jq -e --arg component "${component}" '.[$component]' <<< "${COMPONENT_SOURCES}"); then
        echo "Error: Unknown component '${component}'" >&2
        echo "Valid components: ${VALID_COMPONENTS}" >&2
        return 1
    fi

    local image_repo github_repo github_branch tag_suffix
    image_repo=$(jq -er '.imageRepository' <<< "${component_source}")
    github_repo=$(jq -er '.githubRepository' <<< "${component_source}")
    github_branch=$(jq -er '.githubBranch' <<< "${component_source}")
    tag_suffix=$(jq -r '.tagSuffix // ""' <<< "${component_source}")

    echo "Fetching latest commit from ${github_repo}@${github_branch}..." >&2

    # NOTE: We use 8-char truncated SHAs as image tags. This must match the
    # tag convention used by each component's CI pipeline when publishing images.
    local github_api_url="https://api.github.com/repos/${github_repo}/commits/${github_branch}"

    local latest_commit
    if [[ -n "${GITHUB_TOKEN:-}" ]]; then
        latest_commit=$(curl -sSL \
            -H "Authorization: Bearer ${GITHUB_TOKEN}" \
            -H "Accept: application/vnd.github.v3+json" \
            "${github_api_url}" | \
            jq -r '.sha[0:8]')
    else
        latest_commit=$(curl -sSL \
            -H "Accept: application/vnd.github.v3+json" \
            "${github_api_url}" | \
            jq -r '.sha[0:8]')
    fi

    if [[ -z "${latest_commit}" || "${latest_commit}" == "null" ]]; then
        echo "Error: Failed to fetch latest commit from ${github_repo}" >&2
        return 1
    fi

    echo "Latest commit SHA: ${latest_commit}" >&2

    local full_image="${image_repo}:${latest_commit}${tag_suffix}"

    echo "Verifying image exists: ${full_image}" >&2

    local retry_delay=30
    local i
    for i in $(seq 1 ${MAX_RETRIES}); do
        if regctl manifest head "${full_image}" &> /dev/null; then
            echo "Verified ${component} image: ${full_image}" >&2
            echo "${full_image}"
            return 0
        fi

        if [[ $i -lt ${MAX_RETRIES} ]]; then
            echo "Image not found (attempt $i/${MAX_RETRIES}), waiting ${retry_delay}s for CI to build..." >&2
            sleep ${retry_delay}
            retry_delay=$((retry_delay * 2))
        fi
    done

    echo "Error: Image ${full_image} does not exist after ${MAX_RETRIES} attempts" >&2
    echo "The image may not have been built yet for commit ${latest_commit}" >&2
    return 1
}

components=("$@")
image_lookup_dir=$(mktemp -d)
trap 'rm -rf "${image_lookup_dir}"' EXIT

lookup_pids=()
for index in "${!components[@]}"; do
    component=${components[index]}
    get_latest_image "${component}" \
        > "${image_lookup_dir}/${index}.image" \
        2> >(sed -u "s/^/[${component}] /" >&2) &
    lookup_pids+=($!)
done

failed_components=()
for index in "${!components[@]}"; do
    if ! wait "${lookup_pids[index]}"; then
        failed_components+=("${components[index]}")
    fi
done

if [[ ${#failed_components[@]} -gt 0 ]]; then
    echo "::error::Image lookup failed for ${failed_components[*]}" >&2
    exit 1
fi

for index in "${!components[@]}"; do
    echo "${components[index]}=$(< "${image_lookup_dir}/${index}.image")"
done
