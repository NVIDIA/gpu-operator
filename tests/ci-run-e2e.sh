#! /bin/bash

set -e

IMAGE="$1"
TAG="$2"
LOG_DIR="/tmp/logs"

export DEBIAN_FRONTEND=noninteractive

echo "Create log dir ${LOG_DIR}"
mkdir -p "${LOG_DIR}"

echo "Load kernel modules i2c_core and ipmi_msghandler"
sudo modprobe -a i2c_core ipmi_msghandler

echo "Install dependencies"
sudo apt update && sudo apt install -y jq

echo "Install Helm"
curl https://raw.githubusercontent.com/helm/helm/master/scripts/get-helm-3 | bash

REPOSITORY="$(dirname "${IMAGE}")"
NS="test-operator"
echo "Deploy operator with repository: ${REPOSITORY}"
kubectl create namespace "${NS}"
helm install ../deployments/gpu-operator --generate-name --set operator.tag="${TAG}" --set operator.repository="${REPOSITORY}" -n "${NS}" --wait

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
	echo "Checking dcgm pod"
	kubectl get pods -lapp=nvidia-dcgm-exporter -n gpu-operator-resources

	echo "Checking dcgm pod readiness"
	is_dcgm_ready=$(kubectl get pods -lapp=nvidia-dcgm-exporter -n gpu-operator-resources -ojsonpath='{range .items[*]}{.status.conditions[?(@.type=="Ready")].status}{"\n"}{end}')

	if [ "${is_dcgm_ready}" = "True" ]; then
		dcgm_pod_ip=$(kubectl get pods -n gpu-operator-resources -o wide -l app=nvidia-dcgm-exporter | tail -n 1 | awk '{print $6}')
		curl -s "$dcgm_pod_ip:9400/metrics" | grep "DCGM_FI_DEV_GPU_TEMP"
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

		state=$(kubectl get pods -n "$NS" -l "app.kubernetes.io/component=gpu-operator" \
			-o jsonpath='{.items[0].status.phase}')

		echo "Checking state of the GPU Operator, it is: '$state'"
		if [ "$state" = "Running" ]; then
			return 0
		fi
	done

	echo "Timeout reached, the GPU Operator is still not ready. See below for logs:"
	kubectl logs -n gpu-operator "$(kubectl get pods -n "$NS" -o json | jq -r '.items[0].metadata.name')"
	exit 1
}

test_restart_operator

exit $rc
