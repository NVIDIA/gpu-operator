#!/bin/bash

check_pod_ready() {
	local pod_label=$1
	# SECONDS counts from shell start, so record a baseline and measure against it.
	local start_time=${SECONDS}
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

		if [[ $((SECONDS - start_time)) -gt $((60 * 45)) ]]; then
			echo "timeout reached"
			exit 1;
		fi

		# Echo useful information on stdout
		kubectl get pods -n ${TEST_NAMESPACE}

		echo "Sleeping 5 seconds"
		sleep 5
	done
}

check_pod_deleted() {
	local pod_label=$1
	# SECONDS counts from shell start, so record a baseline and measure against it.
	local start_time=${SECONDS}
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

		if [[ $((SECONDS - start_time)) -gt $((60 * 45)) ]]; then
			echo "timeout reached"
			exit 1;
		fi

		# Echo useful information on stdout
		kubectl get pods -n ${TEST_NAMESPACE}

		echo "Sleeping 5 seconds"
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

	# Killing the operator container mutates the node, so it is dispatched to the
	# node itself. node-exec.sh runs it either over SSH or locally.
	local checks_script_dir
	checks_script_dir="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
	"${checks_script_dir}"/node-exec.sh restart-operator-container "${runtime}"

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

# Regenerate the describe and log files for every pod passed in. The log files
# are rewritten in full rather than tailed, so that the artifact left behind
# holds the complete output of every container.
collect_pod_logs() {
	local log_dir=$1
	local pods=$2

	for pod in $(echo "$pods" | jq -r .[].name); do
		ns=$(echo "$pods" | jq -r ".[] | select(.name == \"$pod\") | .ns")
		echo "Generating logs for pod: ${pod} ns: ${ns}"
		echo "------------------------------------------------" >> "${log_dir}/${pod}.describe"
		kubectl -n "${ns}" describe pods "${pod}" >> "${log_dir}/${pod}.describe"
		kubectl -n "${ns}" logs "${pod}" --all-containers=true > "${log_dir}/${pod}.logs" || true
	done
}

check_gpu_pod_ready() {
	local log_dir=$1
	# SECONDS counts from shell start, so record a baseline and measure against it.
	local start_time=${SECONDS}
	local elapsed=0
	# Regenerating every pod's describe and log files is the expensive part of
	# this loop, so it runs on its own slower cadence while the readiness check
	# below keeps polling every 5 seconds. Track the time at which the next
	# regeneration is due rather than testing the elapsed time for divisibility,
	# which would skip regenerations when an iteration runs long.
	local next_collection=0

	# Ensure the log directory exists
	mkdir -p ${log_dir}

	while :; do
		pods="$(kubectl get --all-namespaces pods -o json | jq '.items[] | {name: .metadata.name, ns: .metadata.namespace}' | jq -s -c .)"
		status=$(kubectl get pods gpu-operator-test -o json | jq -r .status.phase)
		if [ "${status}" = "Succeeded" ]; then
			echo "GPU pod terminated successfully"
			rc=0
			collect_pod_logs "${log_dir}" "${pods}"
			break;
		fi

		elapsed=$((SECONDS - start_time))
		if [[ "${elapsed}" -gt $((60 * 45)) ]]; then
			echo "timeout reached"
			# Collect once more so that the artifact reflects the state at the
			# timeout rather than the state at the last scheduled collection.
			collect_pod_logs "${log_dir}" "${pods}"
			exit 1
		fi

		# Echo useful information on stdout
		kubectl get pods --all-namespaces

		if [[ "${elapsed}" -ge "${next_collection}" ]]; then
			collect_pod_logs "${log_dir}" "${pods}"
			next_collection=$((elapsed + 30))
		fi

		echo "Generating cluster logs"
		echo "------------------------------------------------" >> "${log_dir}/cluster.logs"
		kubectl get --all-namespaces pods >> "${log_dir}/cluster.logs"

		echo "Sleeping 5 seconds"
		sleep 5;
	done
}

# TODO: deduplicate the logic found in this file by moving the duplicate to a common method and parameterizing the labels to select on
check_nvidia_driver_pods_ready() {
	# SECONDS counts from shell start, so record a baseline and measure against it.
	local start_time=${SECONDS}
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

		if [[ $((SECONDS - start_time)) -gt $((60 * 45)) ]]; then
			echo "timeout reached"
			exit 1;
		fi

		# Echo useful information on stdout
		kubectl get pods -n ${TEST_NAMESPACE}

		echo "Sleeping 5 seconds"
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

print_driver_upgrade_debug() {
	echo "current state of driver upgrade"
	kubectl get node -l nvidia.com/gpu.present \
		-o custom-columns=NODE:.metadata.name,OWNER:.metadata.labels.nvidia\\.com/gpu-operator\\.driver\\.owner,UPGRADE_STATE:.metadata.labels.nvidia\\.com/gpu-driver-upgrade-state --no-headers

	echo ""
	echo "driver pods"
	kubectl get pods -l "app.kubernetes.io/component=nvidia-driver" -n ${TEST_NAMESPACE} -o wide || true

	echo ""
	echo "gpu operator operands"
	kubectl get pods -n ${TEST_NAMESPACE} -o wide || true

	echo ""
	echo "driver daemonsets"
	kubectl get daemonsets -l "app.kubernetes.io/component=nvidia-driver" -n ${TEST_NAMESPACE} -o wide || true

	echo ""
	echo "NVIDIADriver status"
	local nvidiadriver_status
	if nvidiadriver_status=$(kubectl get nvidiadriver -o json 2>/dev/null); then
		echo "${nvidiadriver_status}" | jq -r '
			(["NAME", "DEFAULT", "STATE", "REASON", "MESSAGE"] | @tsv),
			(
				.items[]
				| (.status.conditions // []) as $conditions
				| ($conditions[-1] // {}) as $latest
				| [
					.metadata.name,
					(if .spec.default == true then "true" else "-" end),
					(.status.state // "-"),
					($latest.reason // "-"),
					($latest.message // "-")
				]
				| @tsv
			)
		'
	fi
}

wait_for_driver_upgrade_done() {
	gpu_node_count=$(kubectl get node -l nvidia.com/gpu.present --no-headers | wc -l)
	# SECONDS counts from shell start, so record a baseline and measure against it.
	local start_time=${SECONDS}
	local elapsed=0
	# Next elapsed time at which the full debug dump is due. Iterations can take
	# much longer than the nominal sleep, so track a due time instead of testing
	# the elapsed time for divisibility, which would skip dumps entirely.
	local next_debug=0
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

		elapsed=$((SECONDS - start_time))
		if [[ "${elapsed}" -gt $((60 * 45)) ]]; then
			echo "timeout reached"
			print_driver_upgrade_debug
			exit 1;
		fi

		if [[ "${elapsed}" -ge "${next_debug}" ]]; then
			print_driver_upgrade_debug
			next_debug=$((elapsed + 30))
		else
			kubectl get node -l nvidia.com/gpu.present \
				-o custom-columns=NODE:.metadata.name,OWNER:.metadata.labels.nvidia\\.com/gpu-operator\\.driver\\.owner,UPGRADE_STATE:.metadata.labels.nvidia\\.com/gpu-driver-upgrade-state --no-headers
		fi
		
		echo "Sleeping 5 seconds"
		sleep 5
	done
}
