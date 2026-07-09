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
import json
import pathlib
import re
import subprocess
import sys

import yaml


RELATED_IMAGE_COMPONENTS = {
    "gpu-operator-image": "operator",
    "gpu-operator-validator-image": "validator",
    "dcgm-exporter-image": "dcgmExporter",
    "dcgm-image": "dcgm",
    "container-toolkit-image": "toolkit",
    "device-plugin-image": "devicePlugin",
    "gpu-feature-discovery-image": "gfd",
    "mig-manager-image": "migManager",
    "k8s-driver-manager-image": "driver.manager",
    "vfio-manager-image": "vfioManager",
    "cc-manager-image": "ccManager",
    "sandbox-device-plugin-image": "sandboxDevicePlugin",
    "kata-sandbox-device-plugin-image": "kataSandboxDevicePlugin",
    "vgpu-device-manager-image": "vgpuDeviceManager",
    "gdrcopy-image": "gdrcopy",
}

ENV_IMAGE_COMPONENTS = {
    "VALIDATOR_IMAGE": "validator",
    "GFD_IMAGE": "gfd",
    "CONTAINER_TOOLKIT_IMAGE": "toolkit",
    "DCGM_IMAGE": "dcgm",
    "DCGM_EXPORTER_IMAGE": "dcgmExporter",
    "DEVICE_PLUGIN_IMAGE": "devicePlugin",
    "DRIVER_MANAGER_IMAGE": "driver.manager",
    "MIG_MANAGER_IMAGE": "migManager",
    "VFIO_MANAGER_IMAGE": "vfioManager",
    "CC_MANAGER_IMAGE": "ccManager",
    "SANDBOX_DEVICE_PLUGIN_IMAGE": "sandboxDevicePlugin",
    "KATA_SANDBOX_DEVICE_PLUGIN_IMAGE": "kataSandboxDevicePlugin",
    "VGPU_DEVICE_MANAGER_IMAGE": "vgpuDeviceManager",
    "GDRCOPY_IMAGE": "gdrcopy",
}

OS_SPECIFIC_COMPONENTS = {"driver", "gdrcopy"}
DRIVER_RELATED_IMAGES = {
    "default": "driver-image",
    "580": "driver-image-580",
}
DRIVER_ENV_IMAGES = {
    "default": "DRIVER_IMAGE",
    "580": "DRIVER_IMAGE-580",
}
RELEASE_TAG_PATTERN = re.compile(
    r"^v?(?P<major>[0-9]+)\.(?P<minor>[0-9]+)\.(?P<patch>[0-9]+)"
    r"(?:-rc\.(?P<rc>[0-9]+))?$"
)


def load_yaml(path):
    with path.open() as f:
        return yaml.safe_load(f) or {}


def component(values, path):
    current = values
    for part in path.split("."):
        current = current.get(part, {})
    return current


def driver_image_slot(values):
    version = str(component(values, "driver").get("version") or "").strip()
    if version.startswith("580"):
        return "580"
    return "default"


def os_image_ref_candidates(
    values,
    component_path,
    olm_bundle_os_suffix,
):
    data = component(values, component_path)
    repository = (data.get("repository") or "").strip()
    image = (data.get("image") or "").strip()
    version = (data.get("version") or "").strip()

    if not repository:
        if image and ("/" in image or "@" in image):
            return [image]
        return None

    if not image or not version:
        return []

    if version.startswith("sha256:"):
        return [f"{repository}/{image}@{version}"]

    tag = version
    if olm_bundle_os_suffix and not tag.endswith(f"-{olm_bundle_os_suffix}"):
        tag = f"{tag}-{olm_bundle_os_suffix}"

    return [f"{repository}/{image}:{tag}"]


def build_image_ref_candidates(
    values,
    chart,
    component_path,
    olm_bundle_os_suffix,
):
    if component_path in OS_SPECIFIC_COMPONENTS:
        return os_image_ref_candidates(
            values,
            component_path,
            olm_bundle_os_suffix,
        )

    data = component(values, component_path)
    repository = (data.get("repository") or "").strip()
    image = (data.get("image") or "").strip()
    version = data.get("version")

    if repository:
        version = (version or "").strip()
        if not image or not version:
            return []
        return [f"{repository}/{image}:{version}"]

    # Some override files provide a fully-qualified image in the image field.
    if image and ("/" in image or "@" in image):
        return [image]

    return []


def resolve_digest(image_ref, skip_digest):
    if not image_ref or skip_digest:
        return image_ref

    image_ref_without_digest = image_ref.split("@", 1)[0]
    image_name = image_ref_without_digest.rsplit("/", 1)[-1]
    if ":" not in image_name:
        return image_ref

    digest = subprocess.check_output(
        ["regctl", "image", "digest", image_ref_without_digest],
        text=True,
    ).strip()
    return f"{image_ref_without_digest}@{digest}"


def resolve_digest_candidates(image_refs, skip_digest):
    last_error = None
    for image_ref in image_refs:
        try:
            return resolve_digest(image_ref, skip_digest)
        except subprocess.CalledProcessError as exc:
            last_error = exc
            print(f"Warning: failed to resolve digest for {image_ref}.", file=sys.stderr)

    if last_error:
        raise last_error
    return None


def strip_tag_from_digest_ref(image_ref):
    if not image_ref or "@sha256:" not in image_ref:
        return image_ref

    image_ref_without_digest, digest = image_ref.split("@", 1)
    last_slash = image_ref_without_digest.rfind("/")
    last_colon = image_ref_without_digest.rfind(":")
    if last_colon > last_slash:
        image_ref_without_digest = image_ref_without_digest[:last_colon]

    return f"{image_ref_without_digest}@{digest}"


def split_repository_image_version(image_ref):
    if "@sha256:" in image_ref:
        image_ref_without_digest, digest = image_ref.split("@", 1)
    else:
        image_ref_without_digest = image_ref
        digest = ""

    repository_and_image, separator, tag = image_ref_without_digest.rpartition(":")
    if "/" not in tag:
        image_path = repository_and_image
    else:
        image_path = image_ref_without_digest
        tag = ""

    if "/" not in image_path:
        version = f"{tag}@{digest}" if tag and digest else digest or tag
        return "", image_path, version

    repository, image = image_path.rsplit("/", 1)
    version = f"{tag}@{digest}" if tag and digest else digest or tag
    return repository, image, version


def update_alm_examples(csv, image_refs):
    annotations = csv.get("metadata", {}).get("annotations", {})
    alm_examples = annotations.get("alm-examples")
    driver_ref = image_refs.get("driver")
    if not alm_examples or not driver_ref:
        return

    examples = json.loads(alm_examples)
    driver_repository, driver_image, driver_version = split_repository_image_version(driver_ref)

    for example in examples:
        if example.get("kind") != "NVIDIADriver":
            continue
        spec = example.setdefault("spec", {})
        spec["repository"] = driver_repository
        spec["image"] = driver_image
        spec["version"] = driver_version

    annotations["alm-examples"] = json.dumps(examples, indent=2)


def update_operator_deployment(csv, operator_ref, env_refs):
    install = csv.get("spec", {}).get("install", {})
    for deployment in install.get("spec", {}).get("deployments", []):
        spec = deployment.get("spec", {}).get("template", {}).get("spec", {})
        for container in spec.get("containers", []):
            if container.get("name") != "gpu-operator":
                continue
            if operator_ref:
                container["image"] = operator_ref
            for env in container.get("env", []):
                value = env_refs.get(env.get("name"))
                if value:
                    env["value"] = value


def csv_name_for_version(version):
    return f"gpu-operator-certified.v{version}"


def release_metadata_from_tag(release_tag):
    match = RELEASE_TAG_PATTERN.match((release_tag or "").strip())
    if not match:
        raise ValueError(
            f"release tag must look like v26.3.2 or v26.3.2-rc.1: {release_tag}"
        )

    major = int(match.group("major"))
    minor = int(match.group("minor"))
    patch = int(match.group("patch"))
    rc = match.group("rc")

    final_version = f"{major}.{minor}.{patch}"
    csv_version = f"{final_version}-rc.{rc}" if rc is not None else final_version

    if rc is not None and int(rc) > 1:
        replaces_version = f"{final_version}-rc.{int(rc) - 1}"
    elif patch > 0:
        replaces_version = f"{major}.{minor}.{patch - 1}"
    else:
        replaces_version = None

    return {
        "version": csv_version,
        "replaces": csv_name_for_version(replaces_version) if replaces_version else None,
        "skip_range": f">=1.9.0 <{final_version}",
    }


def update_release_metadata(csv, release_tag):
    if not release_tag:
        return

    metadata = release_metadata_from_tag(release_tag)
    csv_metadata = csv.setdefault("metadata", {})
    csv_metadata["name"] = csv_name_for_version(metadata["version"])
    annotations = csv_metadata.setdefault("annotations", {})
    annotations["olm.skipRange"] = metadata["skip_range"]

    spec = csv.setdefault("spec", {})
    spec["version"] = metadata["version"]
    if metadata["replaces"]:
        spec["replaces"] = metadata["replaces"]


def main():
    parser = argparse.ArgumentParser(
        description="Update OLM CSV image references from Helm values."
    )
    parser.add_argument("--values", required=True, type=pathlib.Path)
    parser.add_argument("--chart", required=True, type=pathlib.Path)
    parser.add_argument("--csv", required=True, type=pathlib.Path)
    parser.add_argument(
        "--olm-bundle-os-suffix",
        dest="olm_bundle_os_suffix",
        default="rhel9.6",
    )
    parser.add_argument(
        "--release-tag",
        default="",
        help="Release tag used to update CSV version, replaces, and olm.skipRange.",
    )
    parser.add_argument("--skip-digest", action="store_true")
    args = parser.parse_args()

    values = load_yaml(args.values)
    chart = load_yaml(args.chart)
    csv = load_yaml(args.csv)

    image_refs = {}
    component_names = set(RELATED_IMAGE_COMPONENTS.values()) | set(ENV_IMAGE_COMPONENTS.values())
    component_names.add("operator")
    component_names.add("driver")

    for component_name in sorted(component_names):
        image_ref_candidates = build_image_ref_candidates(
            values,
            chart,
            component_name,
            args.olm_bundle_os_suffix.strip(),
        )
        if image_ref_candidates:
            image_ref = resolve_digest_candidates(
                image_ref_candidates,
                args.skip_digest,
            )
            if component_name in OS_SPECIFIC_COMPONENTS:
                image_ref = strip_tag_from_digest_ref(image_ref)
            image_refs[component_name] = image_ref

    operator_ref = image_refs.get("operator")
    if operator_ref:
        csv.setdefault("metadata", {}).setdefault("annotations", {})["containerImage"] = operator_ref

    related_images = csv.get("spec", {}).get("relatedImages", [])
    driver_slot = driver_image_slot(values)
    driver_related_image_name = DRIVER_RELATED_IMAGES[driver_slot]
    for item in related_images:
        item_name = item.get("name")
        if item_name == driver_related_image_name and "driver" in image_refs:
            item["image"] = image_refs["driver"]
            continue

        component_name = RELATED_IMAGE_COMPONENTS.get(item_name)
        if component_name and component_name in image_refs:
            item["image"] = image_refs[component_name]

    env_refs = {
        env_name: image_refs[component_name]
        for env_name, component_name in ENV_IMAGE_COMPONENTS.items()
        if component_name in image_refs
    }
    driver_env_name = DRIVER_ENV_IMAGES[driver_slot]
    if "driver" in image_refs:
        env_refs[driver_env_name] = image_refs["driver"]

    update_operator_deployment(csv, operator_ref, env_refs)
    update_alm_examples(csv, image_refs)
    update_release_metadata(csv, args.release_tag)

    with args.csv.open("w") as f:
        yaml.safe_dump(csv, f, default_flow_style=False, sort_keys=False)


if __name__ == "__main__":
    main()
