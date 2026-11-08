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

if [[ $# -lt 8 ]]; then
    echo "Usage: $0 MAIN_BUILD_REPOSITORY MAIN_VERSION OPERATOR_IMAGE OPERATOR_VERSION CHART_REFERENCE CHART_VERSION BUNDLE_IMAGE OPERAND_IMAGE..." >&2
    exit 1
fi

main_build_repository="$1"
main_version="$2"
operator_image="$3"
operator_version="$4"
chart_reference="$5"
chart_version="$6"
bundle_image="$7"
operand_images=("${@:8}")

source_commit="${GITHUB_SHA}"
run_url="${GITHUB_SERVER_URL}/${GITHUB_REPOSITORY}/actions/runs/${GITHUB_RUN_ID}"

if ! command -v regctl &> /dev/null; then
    echo "Error: regctl not found. Please install regctl first." >&2
    exit 1
fi

if ! command -v jq &> /dev/null; then
    echo "Error: jq not found. Please install jq first." >&2
    exit 1
fi

main_build_record_reference="${main_build_repository}:${main_version}"
tagged_chart_reference="${chart_reference#oci://}:${chart_version}"

recorded_images=$(printf '%s\n' "${operator_image}:${operator_version}" "${operand_images[@]}" | sort -u)

image_records="[]"
while read -r image; do
    image_digest=$(regctl image digest "${image}")
    image_records=$(jq -c --arg reference "${image}" --arg digest "${image_digest}" '. + [{$reference, $digest}]' <<< "${image_records}")
done <<< "${recorded_images}"

chart_digest=$(regctl image digest "${tagged_chart_reference}")
bundle_digest=$(regctl image digest "${bundle_image}")

main_build_record=$(jq -n \
    --arg mainVersion "${main_version}" \
    --arg operatorVersion "${operator_version}" \
    --arg sourceCommit "${source_commit}" \
    --arg runUrl "${run_url}" \
    --arg chartReference "${tagged_chart_reference}" \
    --arg chartDigest "${chart_digest}" \
    --arg bundleReference "${bundle_image}" \
    --arg bundleDigest "${bundle_digest}" \
    --argjson images "${image_records}" \
    '{
        $mainVersion,
        $operatorVersion,
        $sourceCommit,
        $runUrl,
        chart: {reference: $chartReference, digest: $chartDigest},
        bundle: {reference: $bundleReference, digest: $bundleDigest},
        $images
    }')

regctl artifact put \
    --artifact-type application/vnd.nvidia.gpu-operator.main-build.v1+json \
    --file-media-type application/json \
    "${main_build_record_reference}" <<< "${main_build_record}"
echo "::notice::Recorded main build ${main_build_record_reference}"
