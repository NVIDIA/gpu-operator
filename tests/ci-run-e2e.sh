#!/bin/sh

set -e

IMAGE=$1
TAG=$2
LOG_DIR="/tmp/logs"

echo "Create log dir ${LOG_DIR}"
mkdir -p "${LOG_DIR}"

echo "Load kernel modules i2c_core and ipmi_msghandler"
sudo modprobe -a i2c_core ipmi_msghandler

echo "Install dependencies"
sudo apt update && sudo apt install -y jq

echo "Deploy NFD"
#kubectl apply -f https://raw.githubusercontent.com/kubernetes-sigs/node-feature-discovery/master/nfd-master.yaml.template
#kubectl apply -f ./nfd-worker-daemonset.yaml

echo "Install Helm"
curl -L https://git.io/get_helm.sh | bash
kubectl create serviceaccount -n kube-system tiller
kubectl create clusterrolebinding tiller-cluster-rule --clusterrole=cluster-admin --serviceaccount=kube-system:tiller
helm init --service-account tiller --wait

echo "Deploy operator"
helm install ../deployments/gpu-operator --set image.repository="${IMAGE}" --set image.tag="${TAG}" -n test-operator --wait

# Should be done by default by helm deployment
echo "Deploy default CRD"
#kubectl apply -f ../manifests/cr/sro_cr_sched_none.yaml

echo "Deploy GPU pod"
kubectl apply -f gpu-pod.yaml

rc=1
while :; do
  echo "Get all pods"
  pods="$(kubectl get --all-namespaces pods -o json | jq '.items[] | {name: .metadata.name, ns: .metadata.namespace}' | jq -s -c .)"

  echo "Checking GPU pod status"
  status=$(kubectl get pods gpu-operator-test -o json | jq -r .status.phase)
  if [ "${status}" = "Succeeded" ]; then
    echo "GPU pod terminated successfully";
    rc=0
    break;
  fi
  echo "GPU pod status: ${status}";

  for pod in $(echo "$pods" | jq -r .[].name); do
    ns=$(echo "$pods" | jq -r ".[] | select(.name == \"$pod\") | .ns")
    echo "Generating logs for pod: ${pod} ns: ${ns}"
    echo "------------------------------------------------" >> "${LOG_DIR}/${pod}.describe"
    kubectl -n "${ns}" describe pods "${pod}" >> "${LOG_DIR}/${pod}.describe"
    kubectl -n "${ns}" logs "${pod}" --all-containers=true > "${LOG_DIR}/${pod}.logs" || true
  done
  
  echo "Generating cluster logs"
  echo "------------------------------------------------" >> "${LOG_DIR}/cluster.logs"
  kubectl get --all-namespaces pods >> "${LOG_DIR}/cluster.logs"

  echo "Sleeping 5 seconds"
  sleep 5;
done

exit $rc
