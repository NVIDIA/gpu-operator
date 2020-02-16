#! /bin/bash

set -e

IMAGE=$1
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

# See: https://github.com/helm/helm/issues/6374
helm init --service-account tiller --override spec.selector.matchLabels.'name'='tiller',spec.selector.matchLabels.'app'='helm' --output yaml | sed 's@apiVersion: extensions/v1beta1@apiVersion: apps/v1@' | kubectl apply -f -
kubectl wait --for=condition=available -n kube-system deployment tiller-deploy

echo "Deploy operator"
REPOSITORY="$(dirname "${IMAGE}")"
helm install ../deployments/gpu-operator --set operator.repository="${REPOSITORY}" -n test-operator --wait

echo "Deploy GPU pod"
kubectl apply -f gpu-pod.yaml

current_time=0
while :; do
	pods="$(kubectl get --all-namespaces pods -o json | jq '.items[] | {name: .metadata.name, ns: .metadata.namespace}' | jq -s -c .)"
	status=$(kubectl get pods gpu-operator-test -o json | jq -r .status.phase)
	if [ "${status}" = "Succeeded" ]; then
		echo "GPU pod terminated successfully"
		rc=0
		break;
	fi

	if [[ "${current_time}" -gt $((60 * 45)) ]]; then
		echo "timeout reached"
		exit 1
	fi

	# Echo useful information on stdout
	kubectl get pods --all-namespaces

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
	current_time=$((${current_time} + 5))
	sleep 5;
done

current_time=0
while :; do
	echo "Checking dcgm pod status"
	kubectl get pods -lapp=nvidia-dcgm-exporter -n gpu-operator-resources

	dcgm_pod_status=$(kubectl get pods -lapp=nvidia-dcgm-exporter -n gpu-operator-resources -ojsonpath='{range .items[*]}{.status.phase}{"\n"}{end}')
	if [ "${dcgm_pod_status}" = "Running" ]; then
		# Sleep to give the gpu-exporter enough time to output it's metrics
		# TODO need to add a readiness probe
		sleep 5

		dcgm_pod_ip=$(kubectl get pods -n gpu-operator-resources -o wide -l app=nvidia-dcgm-exporter | tail -n 1 | awk '{print $6}')
		curl -s "$dcgm_pod_ip:9400/gpu/metrics" | grep "dcgm_gpu_temp"
		rc=0

		break;
	fi

	if [[ "${current_time}" -gt $((60 * 45)) ]]; then
		echo "timeout reached"
		exit 1
	fi

	# Echo useful information on stdout
	kubectl get pods --all-namespaces

	echo "Sleeping 5 seconds"
	current_time=$((${current_time} + 5))
	sleep 5
done

# This function kills the operator and waits for the operator to be back in a running state
# Timeout is 100 seconds
test_restart_operator() {
	# The operator is the only container that has the string '"gpu-operator"'
	docker kill "$(docker ps --format '{{.ID}} {{.Command}}' | grep '"gpu-operator"' | cut -f 1 -d ' ')"

	for i in $(seq 1 10); do
		# Sleep a reasonable amount of time for k8s to update the container status to crashing
		sleep 10

		num="$(kubectl get pods -n gpu-operator -o json | jq '.items | length')"
		if [ "$num" -ne 1 ]; then
			echo "Expected only one pod in the gpu-operator namespace"
			exit 1
		fi

		state=$(kubectl get pods -n gpu-operator -o json | jq -r '.items[0].status.containerStatuses[0].state.running')
		echo "Checking state of the GPU Operator, it is: '$state'"
		if [ "$state" != "null" ]; then
			return 0
		fi
	done

	echo "Timeout reached, the GPU Operator is still not ready. See below for logs:"
	kubectl logs -n gpu-operator "$(kubectl get pods -n gpu-operator -o json | jq -r '.items[0].metadata.name')"
	exit 1
}

test_restart_operator

exit $rc
