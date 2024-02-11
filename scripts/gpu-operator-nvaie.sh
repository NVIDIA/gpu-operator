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
# This script is for installing the GPU Operator with NVIDIA vGPU drivers which are part of NVIDIA AI Enterprise (NVAIE)
#
# Pre-requisites:
#   1. Following environment variables must be set before running this script.
# 
#     VGPU_DRIVER_VERSION - NVIDIA vGPU Guest Driver Version (default: 535.154.05)
#     NVAIE_VERSION - NVAIE version for the driver install
#     NGC_API_KEY - NGC user API key to create the image pull secret
#     NGC_USER_EMAIL - NGC user email to create the image pull secret
#     PRIVATE_REGISTRY - NVAIE repository to pull the NVIDIA vGPU guest driver image (default: nvcr.io/nvaie)
#
#   2. vGPU driver license have to be downloaded from the NVIDIA licensing portal in the current directory and saved as "client_configuration_token.tok"

set -u

NGC_API_KEY=${NGC_API_KEY:?"Missing NGC_API_KEY"}
NGC_USER_EMAIL=${NGC_USER_EMAIL:?"Missing NGC_USER_EMAIL"}
VGPU_DRIVER_VERSION=${VGPU_DRIVER_VERSION:-"535.154.05"}
VGPU_DRIVER_NAME=${VGPU_DRIVER_NAME:-"vgpu-guest-driver"}
NVAIE_VERSION=${NVAIE_VERSION:-""}
GPU_OPERATOR_VERSION=${GPU_OPERATOR_VERSION:-""}

REGISTRY_SECRET_NAME=${REGISTRY_SECRET_NAME:-"ngc-secret"}
PRIVATE_REGISTRY=${PRIVATE_REGISTRY:-"nvcr.io/nvaie"}

NAMESPACE=${NAMESPACE:-"gpu-operator"}
VALUES_FILE=${VALUES_FILE:-""}
HELM_INSTALL_OPTS=${HELM_INSTALL_OPTS:-""}

if [ "${NVAIE_VERSION}" != "" ]; then
    # apply custom naming for nvaie drivers
    VGPU_DRIVER_NAME=${VGPU_DRIVER_NAME}-$(echo ${NVAIE_VERSION} | sed s/\\./-/g)
fi

create_operator_namespace() {
    # Return if the namespace is already created
    kubectl get namespace ${NAMESPACE} > /dev/null 2>&1  && echo "${NAMESPACE} namespace already exists" && return 0

    # Create namespace for the GPU Operator
    kubectl create namespace ${NAMESPACE}
}

apply_psa_labels() {
    # Apply PSA labels
    kubectl label --overwrite namespace ${NAMESPACE} pod-security.kubernetes.io/enforce=privileged
}

create_nls_config() {
    if [ ! -f $PWD/client_configuration_token.tok ]; then
        echo "missing NLS licensing token file client_configuration_token.tok in the current directory"
        exit 1
    fi

    # Return if the configmap is already created
    kubectl get configmap licensing-config -n ${NAMESPACE} > /dev/null 2>&1 && echo "licensing-config already exists" && return 0

    # Create a configmap for vGPU licensing
    touch gridd.conf && kubectl create configmap licensing-config \
        -n ${NAMESPACE} --from-file=gridd.conf --from-file=client_configuration_token.tok
}

create_ngc_secret() {
    # Return if the secret is already created
    kubectl get secret ${REGISTRY_SECRET_NAME} -n ${NAMESPACE} > /dev/null 2>&1 && echo "ngc-secret is already created" && return 0

    # Create a pull secret to pulling images from NGC
    kubectl create secret docker-registry ${REGISTRY_SECRET_NAME} \
        --docker-server=${PRIVATE_REGISTRY} \
        --docker-username='$oauthtoken' \
        --docker-password=${NGC_API_KEY} \
        --docker-email=${NGC_USER_EMAIL} \
        -n ${NAMESPACE}
}

add_helm_repo() {
    # Add nvidia helm repository
    helm repo add nvidia https://helm.ngc.nvidia.com/nvidia || true
}

update_helm_repo() {
    # Refresh nvidia helm repository
    helm repo update
}

_prepare_install() {
    create_operator_namespace
    apply_psa_labels
    create_nls_config
    create_ngc_secret
    add_helm_repo
    update_helm_repo
}

_prepare_upgrade() {
    add_helm_repo
    update_helm_repo
}

_set_helm_install_options() {
    if [ -n "${GPU_OPERATOR_VERSION}" ]; then
        HELM_INSTALL_OPTS="${HELM_INSTALL_OPTS} --version=${GPU_OPERATOR_VERSION}"
    fi
    if [ -n "${VALUES_FILE}" ]; then
        HELM_INSTALL_OPTS="${HELM_INSTALL_OPTS} --values=${VALUES_FILE}"
    fi
    echo "helm options ${HELM_INSTALL_OPTS}"
}

install_operator() {
    # Setup for install
    _prepare_install

    # Set helm install options
    _set_helm_install_options

    # Install the operator
    helm upgrade --install --wait gpu-operator nvidia/gpu-operator -n ${NAMESPACE} \
          --set operator.upgradeCRD=true \
          --set driver.repository=${PRIVATE_REGISTRY} \
          --set driver.image=${VGPU_DRIVER_NAME} \
          --set driver.version="${VGPU_DRIVER_VERSION}" \
          --set driver.imagePullSecrets={${REGISTRY_SECRET_NAME}} \
          --set driver.licensingConfig.configMapName="licensing-config" \
          ${HELM_INSTALL_OPTS}

    # List all deployed pods in the namespace
    kubectl get pods -n ${NAMESPACE}
}

cleanup_operator() {
    helm delete gpu-operator -n ${NAMESPACE}
    kubectl delete secret ${REGISTRY_SECRET_NAME} -n ${NAMESPACE}
    kubectl delete configmap licensing-config -n ${NAMESPACE}
}

usage() {
    cat >&2 <<EOF
Usage: $0 COMMAND

Commands:
  install
  upgrade
  cleanup
EOF
    exit 1
}

if [ $# -eq 0 ]; then
    usage
fi
command=$1;
case "${command}" in
    install|upgrade) install_operator;;
    cleanup) cleanup_operator;;
    *) usage ;;
esac
