#!/bin/sh

set -xe

IMAGE=$1
TAG=$2

curl -L https://git.io/get_helm.sh | bash
kubectl create serviceaccount -n kube-system tiller
kubectl create clusterrolebinding tiller-cluster-rule --clusterrole=cluster-admin --serviceaccount=kube-system:tiller
helm init --service-account tiller --wait
helm install ../deployments/gpu-operator --set image.repository="${IMAGE}" --set image.tag="${TAG}" -n test-operator --wait
