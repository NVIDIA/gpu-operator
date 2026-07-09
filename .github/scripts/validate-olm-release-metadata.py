#!/usr/bin/env python3

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

import argparse
import re
import sys

import yaml


def parse_args():
    parser = argparse.ArgumentParser(description="Validate OLM release metadata.")
    parser.add_argument("--release-tag", required=True)
    parser.add_argument("--csv", required=True, help="Path to ClusterServiceVersion YAML")
    return parser.parse_args()


def main():
    args = parse_args()
    match = re.fullmatch(r"v?([0-9]+)\.([0-9]+)\.([0-9]+)-rc\.([0-9]+)", args.release_tag)
    if not match:
        print(f"::error::release tag must look like v26.3.2-rc.1: {args.release_tag}")
        return 1

    major, minor, patch, rc = (int(part) for part in match.groups())
    final_version = f"{major}.{minor}.{patch}"
    expected_version = f"{final_version}-rc.{rc}"
    expected_name = f"gpu-operator-certified.v{expected_version}"
    expected_skip_range = f">=1.9.0 <{final_version}"

    with open(args.csv) as f:
        csv = yaml.safe_load(f)

    annotations = csv.get("metadata", {}).get("annotations", {})
    spec = csv.get("spec", {})
    checks = {
        "metadata.name": (
            csv.get("metadata", {}).get("name"),
            expected_name,
        ),
        "metadata.annotations.olm.skipRange": (
            annotations.get("olm.skipRange"),
            expected_skip_range,
        ),
        "spec.version": (str(spec.get("version")), expected_version),
    }

    failed = False
    for field, (actual, expected) in checks.items():
        if actual != expected:
            print(f"::error::{field} is {actual!r}; expected {expected!r}")
            failed = True

    if failed:
        return 1

    print(
        "::notice::Verified OLM release metadata: "
        f"name={expected_name}, version={expected_version}, "
        f"replaces={spec.get('replaces')}, "
        f"olm.skipRange='{expected_skip_range}'"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
