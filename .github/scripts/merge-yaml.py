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
import pathlib
import re

import yaml


class NumericStringQuotingDumper(yaml.SafeDumper):
    pass


# Helm reads values.yaml with YAML 1.2 rules, which treat short commit hashes such as
# 08013248 or 12345e67, and 0o-prefixed octal, as numbers; PyYAML follows YAML 1.1 and
# writes them unquoted.
NumericStringQuotingDumper.add_implicit_resolver(
    "tag:yaml.org,2002:float",
    re.compile(r"^[-+]?(0[oO][0-7_]+|(\.[0-9]+|[0-9]+(\.[0-9]*)?)([eE][-+]?[0-9]+)?)$"),
    list("-+.0123456789"),
)


def load_yaml(path):
    with path.open() as f:
        return yaml.safe_load(f) or {}


def merge_yaml(base, override):
    for key, value in override.items():
        if isinstance(value, dict) and isinstance(base.get(key), dict):
            merge_yaml(base[key], value)
        else:
            base[key] = value
    return base


def main():
    parser = argparse.ArgumentParser(
        description="Merge one YAML file into another and write the merged result."
    )
    parser.add_argument("--base", required=True, type=pathlib.Path)
    parser.add_argument("--override", required=True, type=pathlib.Path)
    parser.add_argument("--output", required=True, type=pathlib.Path)
    args = parser.parse_args()

    merged = merge_yaml(load_yaml(args.base), load_yaml(args.override))
    with args.output.open("w") as f:
        yaml.dump(
            merged,
            f,
            Dumper=NumericStringQuotingDumper,
            default_flow_style=False,
            sort_keys=False,
        )

    print(f"Merged {args.override} into {args.output}")


if __name__ == "__main__":
    main()
