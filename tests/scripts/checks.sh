#!/bin/bash

check_pod_ready() {
	local pod_label=$1
	local deadline=$((SECONDS + 60 * 45))
	while :; do
		echo "Checking $pod_label pod"
		kubectl get pods -lapp="${pod_label}" -n "${TEST_NAMESPACE}" --request-timeout="${KUBECTL_REQUEST_TIMEOUT}" || true

		echo "Checking $pod_label pod readiness"
		is_pod_ready=$(kubectl get pods -lapp="${pod_label}" -n "${TEST_NAMESPACE}" --request-timeout="${KUBECTL_REQUEST_TIMEOUT}" -ojsonpath='{range .items[*]}{.status.conditions[?(@.type=="Ready")].status}{"\n"}{end}' 2>/dev/null || echo "terminated")

		if [ "${is_pod_ready}" = "True" ]; then
			is_pod_terminating=$(kubectl get pods -lapp="${pod_label}" -n "${TEST_NAMESPACE}" --request-timeout="${KUBECTL_REQUEST_TIMEOUT}" -o jsonpath='{.items[0].metadata.deletionGracePeriodSeconds}' 2>/dev/null || echo "terminated")
			if [ "${is_pod_terminating}" != "" ]; then
				echo "pod $pod_label is in terminating state..."
			else
				echo "Pod $pod_label is ready"
				break;
			fi
		fi

		if (( SECONDS > deadline )); then
			echo "timeout reached"
			exit 1;
		fi

		kubectl get pods -n "${TEST_NAMESPACE}" --request-timeout="${KUBECTL_REQUEST_TIMEOUT}" || true

		echo "Sleeping 5 seconds"
		sleep 5
	done
}

check_pod_deleted() {
	local pod_label=$1
	local deadline=$((SECONDS + 60 * 45))
	local pod_list
	while :; do
		echo "Checking $pod_label pod"
		kubectl get pods -lapp="${pod_label}" -n "${TEST_NAMESPACE}" --request-timeout="${KUBECTL_REQUEST_TIMEOUT}" || true

		echo "Checking if $pod_label pod has been deleted"
		if pod_list=$(kubectl get pods -lapp="${pod_label}" -n "${TEST_NAMESPACE}" -o name --ignore-not-found --request-timeout="${KUBECTL_REQUEST_TIMEOUT}"); then
			if [ -z "${pod_list}" ]; then
				echo "Pod $pod_label has been deleted"
				break;
			else
				echo "Pod $pod_label has not been deleted"
			fi
		else
			api_unreachable "checking whether pod $pod_label has been deleted"
		fi

		if (( SECONDS > deadline )); then
			echo "timeout reached"
			exit 1;
		fi

		kubectl get pods -n "${TEST_NAMESPACE}" --request-timeout="${KUBECTL_REQUEST_TIMEOUT}" || true

		echo "Sleeping 5 seconds"
		sleep 5
	done
}

check_no_restarts() {
	local pod_label=$1
	local restart_count
	restart_count=$(kubectl get pod -lapp="${pod_label}" -n "${TEST_NAMESPACE}" -o jsonpath='{.items[*].status.containerStatuses[0].restartCount}' --request-timeout="${KUBECTL_REQUEST_TIMEOUT}")
	if ! [[ "${restart_count}" =~ ^[0-9]+$ ]]; then
		echo "expected one restart count for ${pod_label}, got '${restart_count}'"
		exit 1
	fi
	if [ "${restart_count}" -gt 1 ]; then
		echo "$pod_label restarted multiple times: ${restart_count}"
		kubectl logs -p -lapp="${pod_label}" --all-containers -n "${TEST_NAMESPACE}" --request-timeout="${KUBECTL_LOG_TIMEOUT}" || true
		exit 1
	fi
	echo "Repeated restarts not observed for pod $pod_label"
	return 0
}

test_restart_operator() {
	local ns=${1}
	local runtime=${2}

	local checks_script_dir
	checks_script_dir="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
	"${checks_script_dir}"/node-exec.sh restart-operator-container "${runtime}"

	for _ in $(seq 1 10); do
		sleep 10

		local operator_phase
		operator_phase=$(kubectl get pods -n "${ns}" -l "app.kubernetes.io/component=gpu-operator" --request-timeout="${KUBECTL_REQUEST_TIMEOUT}" \
			-o jsonpath='{.items[0].status.phase}' || true)

		echo "Checking state of the GPU Operator, it is: '${operator_phase}'"
		if [ "${operator_phase}" = "Running" ]; then
			return 0
		fi
	done

	echo "Timeout reached, the GPU Operator is still not ready. See below for logs:"
	kubectl logs -n "${ns}" --request-timeout="${KUBECTL_LOG_TIMEOUT}" "$(kubectl get pods -n "${ns}" -o json --request-timeout="${KUBECTL_LOG_TIMEOUT}" | jq -r '.items[0].metadata.name')"
	exit 1
}

list_all_pods() {
	kubectl get pods --all-namespaces -o custom-columns=NS:.metadata.namespace,NAME:.metadata.name --no-headers --request-timeout="${KUBECTL_REQUEST_TIMEOUT}" || true
}

collect_pod_logs() {
	local log_dir=$1
	local namespaced_pods=$2
	local namespace pod_name

	while read -r namespace pod_name; do
		[[ -n "${pod_name}" ]] || continue
		echo "Generating logs for pod: ${pod_name} ns: ${namespace}"
		local artifact="${log_dir}/${namespace}_${pod_name}"
		echo "------------------------------------------------" >> "${artifact}.describe"
		kubectl -n "${namespace}" describe pods "${pod_name}" --request-timeout="${KUBECTL_LOG_TIMEOUT}" >> "${artifact}.describe" || true
		kubectl -n "${namespace}" logs "${pod_name}" --all-containers=true --request-timeout="${KUBECTL_LOG_TIMEOUT}" > "${artifact}.logs" || true
	done <<< "${namespaced_pods}"
}

check_gpu_pod_ready() {
	local log_dir=$1
	local deadline=$((SECONDS + 60 * 45))
	local next_collection=0

	mkdir -p "${log_dir}"

	while :; do
		local pod_phase
		pod_phase=$(kubectl get pods gpu-operator-test --request-timeout="${KUBECTL_REQUEST_TIMEOUT}" -o jsonpath='{.status.phase}' || true)
		if [ "${pod_phase}" = "Succeeded" ]; then
			echo "GPU pod terminated successfully"
			collect_pod_logs "${log_dir}" "$(list_all_pods)"
			break;
		fi

		if (( SECONDS > deadline )); then
			echo "timeout reached"
			collect_pod_logs "${log_dir}" "$(list_all_pods)"
			exit 1
		fi

		echo "Generating cluster logs"
		echo "------------------------------------------------" >> "${log_dir}/cluster.logs"
		kubectl get pods --all-namespaces --request-timeout="${KUBECTL_REQUEST_TIMEOUT}" | tee -a "${log_dir}/cluster.logs" || true

		if (( SECONDS >= next_collection )); then
			collect_pod_logs "${log_dir}" "$(list_all_pods)"
			next_collection=$((SECONDS + 30))
		fi

		echo "Sleeping 5 seconds"
		sleep 5;
	done
}

# TODO: deduplicate the logic found in this file by moving the duplicate to a common method and parameterizing the labels to select on
check_nvidia_driver_pods_ready() {
	local deadline=$((SECONDS + 60 * 45))
	while :; do
		echo "Checking nvidia driver pod"
		kubectl get pods -l "app.kubernetes.io/component=nvidia-driver" -n "${TEST_NAMESPACE}" --request-timeout="${KUBECTL_REQUEST_TIMEOUT}" || true

		echo "Checking nvidia driver pod readiness"
		is_pod_ready=$(kubectl get pods -l "app.kubernetes.io/component=nvidia-driver" -n "${TEST_NAMESPACE}" --request-timeout="${KUBECTL_REQUEST_TIMEOUT}" -ojsonpath='{range .items[*]}{.status.conditions[?(@.type=="Ready")].status}{"\n"}{end}' 2>/dev/null || echo "terminated")

		if [ "${is_pod_ready}" = "True" ]; then
			is_pod_terminating=$(kubectl get pods -l "app.kubernetes.io/component=nvidia-driver" -n "${TEST_NAMESPACE}" --request-timeout="${KUBECTL_REQUEST_TIMEOUT}" -ojsonpath='{.items[0].metadata.deletionGracePeriodSeconds}' 2>/dev/null || echo "terminated")
			if [ "${is_pod_terminating}" != "" ]; then
				echo "nvidia driver pod is in terminating state..."
			else
				echo "nvidia driver pod is ready"
				break;
			fi
		fi

		if (( SECONDS > deadline )); then
			echo "timeout reached"
			exit 1;
		fi

		kubectl get pods -n "${TEST_NAMESPACE}" --request-timeout="${KUBECTL_REQUEST_TIMEOUT}" || true

		echo "Sleeping 5 seconds"
		sleep 5
	done
}

check_no_driver_pod_restarts() {
	local restart_count
	restart_count=$(kubectl get pod -l "app.kubernetes.io/component=nvidia-driver" -n "${TEST_NAMESPACE}" -o jsonpath='{.items[*].status.containerStatuses[0].restartCount}' --request-timeout="${KUBECTL_REQUEST_TIMEOUT}")
	if ! [[ "${restart_count}" =~ ^[0-9]+$ ]]; then
		echo "expected one restart count for the nvidia driver pod, got '${restart_count}'"
		exit 1
	fi
	if [ "${restart_count}" -gt 1 ]; then
		echo "nvidia driver pod restarted multiple times: ${restart_count}"
		kubectl logs -p -l "app.kubernetes.io/component=nvidia-driver" --all-containers -n "${TEST_NAMESPACE}" --request-timeout="${KUBECTL_LOG_TIMEOUT}" || true
		exit 1
	fi
	echo "Repeated restarts not observed for the nvidia driver pod"
	return 0
}

api_unreachable() {
	echo "$(date -u '+%Y-%m-%dT%H:%M:%SZ') WARNING: cluster API unreachable while ${1}; treating as not ready and retrying"
}

kubectl_count() {
	local resource_lines
	resource_lines=$(kubectl "$@" --request-timeout="${KUBECTL_REQUEST_TIMEOUT}") || return 1
	echo "${resource_lines}" | grep -c . || true
}

print_driver_upgrade_debug() {
	echo "current state of driver upgrade"
	kubectl get node -l nvidia.com/gpu.present --request-timeout="${KUBECTL_REQUEST_TIMEOUT}" \
		-o custom-columns=NODE:.metadata.name,OWNER:.metadata.labels.nvidia\\.com/gpu-operator\\.driver\\.owner,UPGRADE_STATE:.metadata.labels.nvidia\\.com/gpu-driver-upgrade-state --no-headers || true

	echo ""
	echo "driver pods"
	kubectl get pods -l "app.kubernetes.io/component=nvidia-driver" -n "${TEST_NAMESPACE}" -o wide --request-timeout="${KUBECTL_REQUEST_TIMEOUT}" || true

	echo ""
	echo "gpu operator operands"
	kubectl get pods -n "${TEST_NAMESPACE}" -o wide --request-timeout="${KUBECTL_REQUEST_TIMEOUT}" || true

	echo ""
	echo "driver daemonsets"
	kubectl get daemonsets -l "app.kubernetes.io/component=nvidia-driver" -n "${TEST_NAMESPACE}" -o wide --request-timeout="${KUBECTL_REQUEST_TIMEOUT}" || true

	echo ""
	echo "NVIDIADriver status"
	local nvidiadriver_status
	if nvidiadriver_status=$(kubectl get nvidiadriver -o json --request-timeout="${KUBECTL_REQUEST_TIMEOUT}" 2>/dev/null); then
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
	local deadline=$((SECONDS + 60 * 45))
	local next_debug=0
	local node_list=""
	local node_count=""
	local upgraded_count=0
	local upgrade_state=""
	local gpu_node_count=""

	echo "waiting for the gpu driver upgrade to complete"
	while :; do
		upgraded_count=0

		if [[ "${gpu_node_count:-0}" -le 0 ]]; then
			if node_count=$(kubectl_count get node -l nvidia.com/gpu.present --no-headers); then
				if (( node_count > 0 )); then
					gpu_node_count="${node_count}"
				fi
			else
				api_unreachable "counting the GPU nodes"
			fi
		fi

		if node_list=$(kubectl get nodes -o NAME --request-timeout="${KUBECTL_REQUEST_TIMEOUT}"); then
			for node in ${node_list}; do
				if upgrade_state=$(kubectl get "$node" -ojsonpath='{.metadata.labels.nvidia\.com/gpu-driver-upgrade-state}' --request-timeout="${KUBECTL_REQUEST_TIMEOUT}"); then
					if [ "${upgrade_state}" = "upgrade-done" ]; then
						upgraded_count=$((upgraded_count + 1))
					fi
				else
					api_unreachable "reading the upgrade state of ${node}"
				fi
			done
		else
			api_unreachable "listing the nodes"
		fi

		if [[ "${gpu_node_count:-0}" -gt 0 ]] && [[ "${upgraded_count}" -eq "${gpu_node_count}" ]]; then
			echo "gpu driver upgrade completed successfully"
			break;
		else
			echo "gpu driver still in progress. $upgraded_count/${gpu_node_count:-unknown} node(s) upgraded"
		fi

		if (( SECONDS > deadline )); then
			echo "timeout reached"
			print_driver_upgrade_debug
			exit 1;
		fi

		if (( SECONDS >= next_debug )); then
			print_driver_upgrade_debug
			next_debug=$((SECONDS + 30))
		else
			kubectl get node -l nvidia.com/gpu.present --request-timeout="${KUBECTL_REQUEST_TIMEOUT}" \
				-o custom-columns=NODE:.metadata.name,OWNER:.metadata.labels.nvidia\\.com/gpu-operator\\.driver\\.owner,UPGRADE_STATE:.metadata.labels.nvidia\\.com/gpu-driver-upgrade-state --no-headers || true
		fi
		
		echo "Sleeping 5 seconds"
		sleep 5
	done
}
