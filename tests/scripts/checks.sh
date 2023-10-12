#!/bin/bash

check_pod_ready() {
	local pod_label=$1
	local current_time=0
	while :; do
		echo "Checking $pod_label pod"
		kubectl get pods -lapp=$pod_label -n ${TEST_NAMESPACE}

		echo "Checking $pod_label pod readiness"
		is_pod_ready=$(kubectl get pods -lapp=$pod_label -n ${TEST_NAMESPACE} -ojsonpath='{range .items[*]}{.status.conditions[?(@.type=="Ready")].status}{"\n"}{end}' 2>/dev/null || echo "terminated")

		if [ "${is_pod_ready}" = "True" ]; then
			# Check if the pod is not in terminating state
			is_pod_terminating=$(kubectl get pods -lapp=$pod_label -n ${TEST_NAMESPACE} -o jsonpath='{.items[0].metadata.deletionGracePeriodSeconds}' 2>/dev/null || echo "terminated")
			if [ "${is_pod_terminating}" != "" ]; then
				echo "pod $pod_label is in terminating state..."
			else
				echo "Pod $pod_label is ready"
				break;
			fi
		fi

		if [[ "${current_time}" -gt $((60 * 45)) ]]; then
			echo "timeout reached"
			exit 1;
		fi

		# Echo useful information on stdout
		kubectl get pods -n ${TEST_NAMESPACE}

		echo "Sleeping 5 seconds"
		current_time=$((${current_time} + 5))
		sleep 5
	done
}

check_pod_deleted() {
	local pod_label=$1
	local current_time=0
	while :; do
		echo "Checking $pod_label pod"
		kubectl get pods -lapp=$pod_label -n ${TEST_NAMESPACE}

		echo "Checking if $pod_label pod has been deleted"
		# note: $(kubectl get pods <options> -o jsonpath='.items' | jq length) does not work for older kubectl clients
		num_pods=$(kubectl get pods -lapp=$pod_label -n ${TEST_NAMESPACE} -o json | jq '.items' | jq length)

		if [ "${num_pods}" = 0 ]; then
			echo "Pod $pod_label has been deleted"
			break;
		else
			echo "Pod $pod_label has not been deleted"
		fi

		if [[ "${current_time}" -gt $((60 * 45)) ]]; then
			echo "timeout reached"
			exit 1;
		fi

		# Echo useful information on stdout
		kubectl get pods -n ${TEST_NAMESPACE}

		echo "Sleeping 5 seconds"
		current_time=$((${current_time} + 5))
		sleep 5
	done
}

check_no_restarts() {
	local pod_label=$1
	restartCount=$(kubectl get pod -lapp=$pod_label -n ${TEST_NAMESPACE} -o jsonpath='{.items[*].status.containerStatuses[0].restartCount}')
	if [ $restartCount -gt 1 ]; then
		echo "$pod_label restarted multiple times: $restartCount"
		kubectl logs -p -lapp=$pod_label --all-containers -n ${TEST_NAMESPACE}
		exit 1
	fi
	echo "Repeated restarts not observed for pod $pod_label"
	return 0
}

# This function kills the operator and waits for the operator to be back in a running state
# Timeout is 100 seconds
test_restart_operator() {
	local ns=${1}
	local runtime=${2}

	if [[ x"${runtime}" == x"containerd" ]]; then
		# The operator is the only container that has the string '"gpu-operator"'
		# TODO: This requires permissions on containerd.sock
		sudo crictl rm --force "$(sudo crictl ps --name gpu-operator | awk '{if(NR>1)print $1}')"
	else
		# The operator is the only container that has the string '"gpu-operator"'
	    docker kill "$(docker ps --format '{{.ID}} {{.Command}}' | grep "gpu-operator" | cut -f 1 -d ' ')"
	fi

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

	# Ensure the log directory exists
	mkdir -p ${log_dir}

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

# TODO: deduplicate the logic found in this file by moving the duplicate to a common method and parameterizing the labels to select on
check_nvidia_driver_pods_ready() {
	local current_time=0
	while :; do
		echo "Checking nvidia driver pod"
		kubectl get pods -l "app.kubernetes.io/component=nvidia-driver" -n ${TEST_NAMESPACE}

		echo "Checking nvidia driver pod readiness"
		is_pod_ready=$(kubectl get pods -l "app.kubernetes.io/component=nvidia-driver" -n ${TEST_NAMESPACE} -ojsonpath='{range .items[*]}{.status.conditions[?(@.type=="Ready")].status}{"\n"}{end}' 2>/dev/null || echo "terminated")

		if [ "${is_pod_ready}" = "True" ]; then
			# Check if the pod is not in terminating state
			is_pod_terminating=$(kubectl get pods -l "app.kubernetes.io/component=nvidia-driver" -n ${TEST_NAMESPACE} -ojsonpath='{.items[0].metadata.deletionGracePeriodSeconds}' 2>/dev/null || echo "terminated")
			if [ "${is_pod_terminating}" != "" ]; then
				echo "nvidia driver pod is in terminating state..."
			else
				echo "nvidia driver pod is ready"
				break;
			fi
		fi

		if [[ "${current_time}" -gt $((60 * 45)) ]]; then
			echo "timeout reached"
			exit 1;
		fi

		# Echo useful information on stdout
		kubectl get pods -n ${TEST_NAMESPACE}

		echo "Sleeping 5 seconds"
		current_time=$((${current_time} + 5))
		sleep 5
	done
}

check_no_driver_pod_restarts() {
	restartCount=$(kubectl get pod -l "app.kubernetes.io/component=nvidia-driver" -n ${TEST_NAMESPACE} -o jsonpath='{.items[*].status.containerStatuses[0].restartCount}')
	if [ $restartCount -gt 1 ]; then
		echo "nvidia driver pod restarted multiple times: $restartCount"
		kubectl logs -p -l "app.kubernetes.io/component=nvidia-driver" --all-containers -n ${TEST_NAMESPACE}
		exit 1
	fi
	echo "Repeated restarts not observed for the nvidia driver pod"
	return 0
}

wait_for_driver_upgrade_done() {
	gpu_node_count=$(kubectl get node -l nvidia.com/gpu.present --no-headers | wc -l)
	local current_time=0
	echo "waiting for the gpu driver upgrade to complete"
	while :; do
		local upgraded_count=0
		for node in $(kubectl get nodes -o NAME); do
			upgrade_state=$(kubectl get $node -ojsonpath='{.metadata.labels.nvidia\.com/gpu-driver-upgrade-state}')
			if [ "${upgrade_state}" = "upgrade-done" ]; then
				upgraded_count=$((${upgraded_count} + 1))
			fi
		done
		if [[ $upgraded_count -eq $gpu_node_count ]]; then
			echo "gpu driver upgrade completed successfully"
			break;
		else
			echo "gpu driver still in progress. $upgraded_count/$gpu_node_count node(s) upgraded"
		fi

		if [[ "${current_time}" -gt $((60 * 45)) ]]; then
			echo "timeout reached"
			exit 1;
		fi

		echo "current state of driver upgrade"
		kubectl get node -l nvidia.com/gpu.present \
			-o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.metadata.labels.nvidia\.com/gpu-driver-upgrade-state}{"\n"}{end}'

		
		echo "Sleeping 5 seconds"
		current_time=$((${current_time} + 5))
		sleep 5
	done
}
