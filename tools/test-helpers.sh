#!/usr/bin/env bash
# Copyright (c) NVIDIA CORPORATION.  All rights reserved.
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
#

TESTS_RUN=0
TESTS_FAILED=0

assert_eq() {
    local expected="$1" actual="$2" description="$3"
    TESTS_RUN=$(( TESTS_RUN + 1 ))
    if [[ "${expected}" != "${actual}" ]]; then
        TESTS_FAILED=$(( TESTS_FAILED + 1 ))
        printf 'FAIL: %s\n  expected: [%s]\n  actual:   [%s]\n' \
            "${description}" "${expected}" "${actual}" >&2
    fi
}

assert_fails() {
    local description="$1"
    shift
    local status=0 output
    TESTS_RUN=$(( TESTS_RUN + 1 ))
    output="$("$@" 2>&1)" || status=$?
    if (( status == 0 )) || (( status == 127 )); then
        TESTS_FAILED=$(( TESTS_FAILED + 1 ))
        printf 'FAIL: %s\n  expected a deliberate non-zero exit, got %d\n  output: %s\n' \
            "${description}" "${status}" "${output}" >&2
    fi
}

finish() {
    printf '%s: %d assertions, %d failures\n' \
        "$(basename "${0}")" "${TESTS_RUN}" "${TESTS_FAILED}" >&2
    (( TESTS_FAILED == 0 ))
}
