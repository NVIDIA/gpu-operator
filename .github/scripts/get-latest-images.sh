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

COMPONENT=${1:-}

if [[ -z "${COMPONENT}" ]]; then
    echo "Usage: $0 <toolkit|device-plugin|mig-manager>" >&2
    exit 1
fi

# Verify regctl is available
if ! command -v regctl &> /dev/null; then
    echo "Error: regctl not found. Please install regctl first." >&2
    exit 1
fi

# Map component names to GHCR image repositories and GitHub source repositories
case "${COMPONENT}" in
    toolkit)
        IMAGE_REPO="ghcr.io/nvidia/container-toolkit"
        GITHUB_REPO="NVIDIA/container-toolkit"
        ;;
    device-plugin)
        IMAGE_REPO="ghcr.io/nvidia/k8s-device-plugin"
        GITHUB_REPO="NVIDIA/k8s-device-plugin"
        ;;
    mig-manager)
        IMAGE_REPO="ghcr.io/nvidia/k8s-mig-manager"
        GITHUB_REPO="NVIDIA/k8s-mig-manager"
        ;;
    *)
        echo "Error: Unknown component '${COMPONENT}'" >&2
        echo "Valid components: toolkit, device-plugin, mig-manager" >&2
        exit 1
        ;;
esac

echo "Fetching latest commit from ${GITHUB_REPO}..." >&2

# Get the latest commit SHA from the main branch using GitHub API.
# NOTE: We use 8-char truncated SHAs as image tags. This must match the
# tag convention used by each component's CI pipeline when publishing images.
GITHUB_API_URL="https://api.github.com/repos/${GITHUB_REPO}/commits/main"

# Use GITHUB_TOKEN if available for authentication (higher rate limits)
if [[ -n "${GITHUB_TOKEN:-}" ]]; then
    LATEST_COMMIT=$(curl -sSL \
        -H "Authorization: Bearer ${GITHUB_TOKEN}" \
        -H "Accept: application/vnd.github.v3+json" \
        "${GITHUB_API_URL}" | \
        jq -r '.sha[0:8]')
else
    LATEST_COMMIT=$(curl -sSL \
        -H "Accept: application/vnd.github.v3+json" \
        "${GITHUB_API_URL}" | \
        jq -r '.sha[0:8]')
fi

if [[ -z "${LATEST_COMMIT}" || "${LATEST_COMMIT}" == "null" ]]; then
    echo "Error: Failed to fetch latest commit from ${GITHUB_REPO}" >&2
    exit 1
fi

echo "Latest commit SHA: ${LATEST_COMMIT}" >&2

# Construct full image path with commit tag
FULL_IMAGE="${IMAGE_REPO}:${LATEST_COMMIT}"

echo "Verifying image exists: ${FULL_IMAGE}" >&2

# Verify the image exists using regctl with retry
MAX_RETRIES=5
RETRY_DELAY=30
for i in $(seq 1 ${MAX_RETRIES}); do
    if regctl manifest head "${FULL_IMAGE}" &> /dev/null; then
        echo "Verified ${COMPONENT} image: ${FULL_IMAGE}" >&2
        echo "${FULL_IMAGE}"
        exit 0
    fi

    if [[ $i -lt ${MAX_RETRIES} ]]; then
        echo "Image not found (attempt $i/${MAX_RETRIES}), waiting ${RETRY_DELAY}s for CI to build..." >&2
        sleep ${RETRY_DELAY}
        # Exponential backoff: 30s, 60s, 120s, 240s
        RETRY_DELAY=$((RETRY_DELAY * 2))
    fi
done

echo "Error: Image ${FULL_IMAGE} does not exist after ${MAX_RETRIES} attempts" >&2
echo "The image may not have been built yet for commit ${LATEST_COMMIT}" >&2
exit 1
