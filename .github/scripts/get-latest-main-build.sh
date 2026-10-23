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

readonly MAIN_VERSION_PATTERN='^0\.0\.0-main-([0-9]{8})$'

if [[ $# -ne 3 ]]; then
    echo "Usage: $0 MAIN_BUILD_REPOSITORY CHART_REPOSITORY MAX_AGE_DAYS" >&2
    exit 1
fi

main_build_repository="$1"
chart_repository="$2"
max_age_days="$3"

if ! command -v regctl &> /dev/null; then
    echo "Error: regctl not found. Please install regctl first." >&2
    exit 1
fi

if ! command -v jq &> /dev/null; then
    echo "Error: jq not found. Please install jq first." >&2
    exit 1
fi

main_build_tags=$(regctl tag ls "${main_build_repository}")
main_version=$(grep -E "${MAIN_VERSION_PATTERN}" <<< "${main_build_tags}" | sort | tail -n 1 || true)

if [[ -z "${main_version}" ]]; then
    echo "Error: ${main_build_repository} has no main build" >&2
    exit 1
fi

[[ "${main_version}" =~ ${MAIN_VERSION_PATTERN} ]]
published_date="${BASH_REMATCH[1]}"

oldest_accepted_date=$(date -u -d "${max_age_days} days ago" +%Y%m%d)
if (( published_date < oldest_accepted_date )); then
    echo "Error: newest main build ${main_version} is older than ${max_age_days} day(s); the scheduled publish did not run or failed" >&2
    exit 1
fi

main_build_record=$(regctl artifact get "${main_build_repository}:${main_version}")
operator_version=$(jq -er '.operatorVersion' <<< "${main_build_record}")
recorded_chart_digest=$(jq -r '.chart.digest' <<< "${main_build_record}")
current_chart_digest=$(regctl image digest "${chart_repository}:${main_version}")
if [[ "${current_chart_digest}" != "${recorded_chart_digest}" ]]; then
    echo "Error: ${chart_repository}:${main_version} is ${current_chart_digest}, but main build ${main_version} recorded ${recorded_chart_digest}" >&2
    exit 1
fi

echo "chart_version=${main_version}"
echo "operator_version=${operator_version}"
