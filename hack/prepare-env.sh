#! /bin/bash

env=$(cat bundle/manifests/gpu-operator-certified.clusterserviceversion.yaml \
| yq \
| jq '.spec.install.spec.deployments[].spec.template.spec.containers[].env[] | .name, "=", .value, ";"' -r )
env=${env//$'\n'/}
echo $env > ./hack/.env
sed -i 's/;/\n/g' ./hack/.env

echo KUBECONFIG=${PWD}/hack/kubeconfig >> ./hack/.env
echo OPERATOR_NAMESPACE=nvidia-gpu-operator >> ./hack/.env

export KUBECONFIG=${PWD}/hack/kubeconfig
export OPERATOR_NAMESPACE=nvidia-gpu-operator

kubectl create namespace ${OPERATOR_NAMESPACE} 2>/dev/null || true
kubectl apply -f ./config/crd/bases/nvidia.com_clusterpolicies.yaml
kubectl apply -f ./config/samples/v1_clusterpolicy.yaml
