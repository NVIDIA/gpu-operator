#!/usr/bin/env bash 
# Copyright (c) 2022, NVIDIA CORPORATION. All rights reserved.
#
# SCRIPT TO DEPLOY GPU OPERATOR
# This script is intended for NVIDIA AI Enterprise customers.
# 
# In order to use this script, you have to create an environment variable named NGC_API_KEY and populate it with your NGC API key. 
# Same step for NGC_USER_EMAIL where the variable contains your email address.
# Example to run the script:
# NGC_API_KEY=<key> NGC_USER_EMAIL<email> ./install-gpu-operator-nvaie.sh
#
# Update DEPLOYMENT_TYPE environment variable below, with one of the possible values: "virtual" or "baremetal".
# You can customize the DATACENTER_DRIVER_VERSION for the baremetal DEPLOYMENT_TYPE. Find driver versions here:
# https://catalog.ngc.nvidia.com/orgs/nvidia/containers/driver/tags
# Example for baremetal deployement:
# DEPLOYMENT_TYPE=baremetal DATACENTER_DRIVER_VERSION=470.82.01 NGC_API_KEY=<key> NGC_USER_EMAIL<email> ./install-gpu-operator-nvaie.sh
#
NGC_API_KEY=${NGC_API_KEY:?"Missing NGC_API_KEY"}
NGC_USER_EMAIL=${NGC_USER_EMAIL:?"Missing NGC_USER_EMAIL"}
DEPLOYMENT_TYPE=${DEPLOYMENT_TYPE:-"virtual"}
DATACENTER_DRIVER_VERSION=${DATACENTER_DRIVER_VERSION:-"510.47.03"}

REGISTRY_SECRET_NAME=ngc-secret
PRIVATE_REGISTRY=nvcr.io/nvaie

# step1: create namespace for GPU Operator
kubectl create namespace gpu-operator


# step2: create configmap for vGPU licensing
#
# note: the NLS client license token is stored in client_configuration_token.tok
#

if [ DEPLOYMENT_TYPE = virtual ]; then
    sudo touch gridd.conf

    kubectl create configmap licensing-config \
        -n gpu-operator --from-file=gridd.conf --from-file=client_configuration_token.tok
fi



# step3: create pull secret to download images from NGC

kubectl create secret docker-registry ${REGISTRY_SECRET_NAME} \
    --docker-server=${PRIVATE_REGISTRY} \
    --docker-username='$oauthtoken' \
    --docker-password=${NGC_API_KEY} \
    --docker-email=${NGC_USER_EMAIL} \
    -n gpu-operator


# step4: add NVIDIA AI Enterprise Helm repository

helm repo add nvaie https://helm.ngc.nvidia.com/nvaie \
  --username='$oauthtoken' --password=${NGC_API_KEY} \
  && helm repo update


# step5: Install the NVIDIA GPU Operator

if [ DEPLOYMENT_TYPE = virtual ]; then
    helm install --wait gpu-operator nvaie/gpu-operator-2-0 -n gpu-operator
else
    helm install --wait gpu-operator nvaie/gpu-operator-2-0 -n gpu-operator \
        --set driver.repository=nvcr.io/nvidia \
        --set driver.image=driver \
        --set driver.version="${DATACENTER_DRIVER_VERSION}" \
        --set driver.licensingConfig.config.name=""
fi
