#!/bin/bash

if [[ "${SKIP_MIGRATION}" == "true" ]]; then
    echo "Skipping migration: SKIP_MIGRATION=${SKIP_MIGRATION}"
    exit 0
fi

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
source ${SCRIPT_DIR}/.definitions.sh

# Import the check definitions
source ${SCRIPT_DIR}/checks.sh

OPERATOR_REPOSITORY=$(dirname "${OPERATOR_IMAGE}")
OPERATOR_OPTIONS="${OPERATOR_OPTIONS:-} --set operator.repository=${OPERATOR_REPOSITORY} --set validator.repository=${OPERATOR_REPOSITORY}"
if [[ -n "${OPERATOR_VERSION}" ]]; then
    OPERATOR_OPTIONS="${OPERATOR_OPTIONS} --set operator.version=${OPERATOR_VERSION} --set validator.version=${OPERATOR_VERSION}"
fi
OPERATOR_OPTIONS="${OPERATOR_OPTIONS} --set operator.defaultRuntime=${CONTAINER_RUNTIME}"

get_helm_release_name() {
    ${HELM} list -n "${TEST_NAMESPACE}" | grep gpu-operator | awk '{print $1}'
}

wait_for_legacy_driver_daemonset_deleted() {
    local elapsed_time=0

    echo "Waiting for ClusterPolicy-owned driver DaemonSet to be deleted"
    while :; do
        daemonset_count=$(kubectl get daemonset -l app=nvidia-driver-daemonset -n "${TEST_NAMESPACE}" --no-headers 2>/dev/null | wc -l)
        if [[ "${daemonset_count}" -eq 0 ]]; then
            break
        fi

        if [[ "${elapsed_time}" -gt 300 ]]; then
            echo "timeout reached waiting for legacy driver DaemonSet deletion"
            kubectl get daemonset -n "${TEST_NAMESPACE}" -o wide
            exit 1
        fi

        sleep 5
        elapsed_time=$((${elapsed_time} + 5))
    done
}

wait_for_orphaned_legacy_driver_pod() {
    local pod_name=$1
    local elapsed_time=0

    echo "Waiting for legacy driver pod/${pod_name} to become orphaned"
    while :; do
        owner_count=$(kubectl get pod "${pod_name}" -n "${TEST_NAMESPACE}" -o json | jq '.metadata.ownerReferences | length')
        if [[ "${owner_count}" -eq 0 ]]; then
            echo "legacy driver pod/${pod_name} is orphaned"
            break
        fi

        if [[ "${elapsed_time}" -gt 300 ]]; then
            echo "timeout reached waiting for legacy driver pod to become orphaned"
            kubectl get pod "${pod_name}" -n "${TEST_NAMESPACE}" -o yaml
            exit 1
        fi

        sleep 5
        elapsed_time=$((${elapsed_time} + 5))
    done
}

wait_for_default_nvidiadriver() {
    local elapsed_time=0

    echo "Waiting for default NVIDIADriver to be rendered"
    while :; do
        default_count=$(kubectl get nvidiadriver -o json 2>/dev/null | jq '[.items[] | select(.spec.default == true)] | length')
        if [[ "${default_count}" -eq 1 ]]; then
            break
        fi

        if [[ "${elapsed_time}" -gt 300 ]]; then
            echo "timeout reached waiting for default NVIDIADriver"
            kubectl get nvidiadriver || true
            exit 1
        fi

        sleep 5
        elapsed_time=$((${elapsed_time} + 5))
    done
}

wait_for_nvidiadriver_owner_labels() {
    local driver_name=$1
    local elapsed_time=0
    local gpu_node_count

    gpu_node_count=$(kubectl get node -l nvidia.com/gpu.present=true --no-headers | wc -l)
    echo "Waiting for ${gpu_node_count} GPU node(s) to be owned by NVIDIADriver/${driver_name}"

    while :; do
        owned_count=$(kubectl get nodes -l "nvidia.com/gpu.present=true,nvidia.com/gpu-operator.driver.owner=${driver_name}" --no-headers | wc -l)
        if [[ "${owned_count}" -eq "${gpu_node_count}" ]]; then
            break
        fi

        if [[ "${elapsed_time}" -gt 300 ]]; then
            echo "timeout reached waiting for NVIDIADriver owner labels"
            kubectl get nodes -l nvidia.com/gpu.present=true -o json |
                jq -r '.items[] | [.metadata.name, (.metadata.labels["nvidia.com/gpu-operator.driver.owner"] // "-")] | @tsv'
            exit 1
        fi

        sleep 5
        elapsed_time=$((${elapsed_time} + 5))
    done
}

wait_for_nvidiadriver_daemonset() {
    local driver_name=$1
    local elapsed_time=0

    echo "Waiting for NVIDIADriver-owned driver DaemonSet"
    while :; do
        daemonset_count=$(kubectl get daemonset -l "app.kubernetes.io/component=nvidia-driver" -n "${TEST_NAMESPACE}" -o json |
            jq --arg driver_name "${driver_name}" '[.items[] | select(.spec.template.spec.nodeSelector["nvidia.com/gpu-operator.driver.owner"] == $driver_name)] | length')
        if [[ "${daemonset_count}" -gt 0 ]]; then
            break
        fi

        if [[ "${elapsed_time}" -gt 300 ]]; then
            echo "timeout reached waiting for NVIDIADriver-owned driver DaemonSet"
            kubectl get daemonset -n "${TEST_NAMESPACE}" -o yaml
            exit 1
        fi

        sleep 5
        elapsed_time=$((${elapsed_time} + 5))
    done
}

wait_for_legacy_driver_pod_deleted() {
    local pod_name=$1
    local elapsed_time=0

    echo "Waiting for orphaned legacy driver pod/${pod_name} to be deleted by the upgrade flow"
    while :; do
        if ! kubectl get pod "${pod_name}" -n "${TEST_NAMESPACE}" >/dev/null 2>&1; then
            break
        fi

        if [[ "${elapsed_time}" -gt 300 ]]; then
            echo "timeout reached waiting for orphaned legacy driver pod deletion"
            print_driver_upgrade_debug
            kubectl get pod "${pod_name}" -n "${TEST_NAMESPACE}" -o yaml || true
            exit 1
        fi

        print_driver_upgrade_debug
        sleep 5
        elapsed_time=$((${elapsed_time} + 5))
    done
}

legacy_driver_pod=$(kubectl get pod -l app=nvidia-driver-daemonset -n "${TEST_NAMESPACE}" -o jsonpath='{.items[0].metadata.name}')
if [[ -z "${legacy_driver_pod}" ]]; then
    echo "legacy ClusterPolicy driver pod not found"
    kubectl get pods -n "${TEST_NAMESPACE}" -o wide
    exit 1
fi

operator_name=$(get_helm_release_name)
if [[ -z "${operator_name}" ]]; then
    echo "GPU Operator Helm release not found in namespace ${TEST_NAMESPACE}"
    ${HELM} list -n "${TEST_NAMESPACE}"
    exit 1
fi

echo "Migrating Helm release/${operator_name} from ClusterPolicy driver management to NVIDIADriver"
${HELM} upgrade "${operator_name}" "${PROJECT_DIR}/deployments/gpu-operator" \
    -n "${TEST_NAMESPACE}" \
    --reuse-values \
    ${OPERATOR_OPTIONS:-} \
    --set driver.nvidiaDriverCRD.enabled=true \
    --set driver.nvidiaDriverCRD.deployDefaultCR=true \
    --wait

wait_for_legacy_driver_daemonset_deleted
wait_for_orphaned_legacy_driver_pod "${legacy_driver_pod}"
wait_for_default_nvidiadriver
wait_for_nvidiadriver_owner_labels default
wait_for_nvidiadriver_daemonset default

wait_for_legacy_driver_pod_deleted "${legacy_driver_pod}"
wait_for_driver_upgrade_done
check_nvidia_driver_pods_ready
