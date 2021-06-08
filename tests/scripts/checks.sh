#! /bin/bash

check_pod_ready() {
	local pod_label=$1
	local current_time=0
	while :; do
		echo "Checking $pod_label pod"
		kubectl get pods -lapp=$pod_label -n gpu-operator-resources

		echo "Checking $pod_label pod readiness"
		is_pod_ready=$(kubectl get pods -lapp=$pod_label -n gpu-operator-resources -ojsonpath='{range .items[*]}{.status.conditions[?(@.type=="Ready")].status}{"\n"}{end}')

		if [ "${is_pod_ready}" = "True" ]; then
			echo "Pod $pod_label is ready"
			break;
		fi

		if [[ "${current_time}" -gt $((60 * 45)) ]]; then
			echo "timeout reached"
			exit 1;
		fi

		# Echo useful information on stdout
		kubectl get pods -n gpu-operator-resources

		echo "Sleeping 5 seconds"
		current_time=$((${current_time} + 5))
		sleep 5
	done
}

# This function kills the operator and waits for the operator to be back in a running state
# Timeout is 100 seconds
test_restart_operator() {
	local ns=${1}
	# The operator is the only container that has the string '"gpu-operator"'
	docker kill "$(docker ps --format '{{.ID}} {{.Command}}' | grep "gpu-operator" | cut -f 1 -d ' ')"

	for i in $(seq 1 10); do
		# Sleep a reasonable amount of time for k8s to update the container status to crashing
		sleep 10

		state=$(kubectl get pods -n "${ns}" -l "app.kubernetes.io/component=gpu-operator" \
			-o jsonpath='{.items[0].status.phase}')

		echo "Checking state of the GPU Operator, it is: '$state'"
		if [ "$state" = "Running" ]; then
			return 0
		fi
	done

	echo "Timeout reached, the GPU Operator is still not ready. See below for logs:"
	kubectl logs -n gpu-operator "$(kubectl get pods -n "${ns}" -o json | jq -r '.items[0].metadata.name')"
	exit 1
}

check_gpu_pod_ready() {
	local log_dir=$1
	local current_time=0
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
			echo "------------------------------------------------" >> "${log_dir}/${pod}.describe"
			kubectl -n "${ns}" describe pods "${pod}" >> "${log_dir}/${pod}.describe"
			kubectl -n "${ns}" logs "${pod}" --all-containers=true > "${log_dir}/${pod}.logs" || true
		done

		echo "Generating cluster logs"
		echo "------------------------------------------------" >> "${log_dir}/cluster.logs"
		kubectl get --all-namespaces pods >> "${log_dir}/cluster.logs"

		echo "Sleeping 5 seconds"
		current_time=$((${current_time} + 5))
		sleep 5;
	done
}