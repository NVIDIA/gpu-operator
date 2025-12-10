#!/usr/bin/env bash

set -o nounset
set -x

# Set ENABLE_EXTENDED_DIAGNOSTICS=true to use a debug container for complete nvidia-bug-report collection
ENABLE_EXTENDED_DIAGNOSTICS=${ENABLE_EXTENDED_DIAGNOSTICS:-false}
DEBUG_CONTAINER_IMAGE=${DEBUG_CONTAINER_IMAGE:-ghcr.io/nvidia/gpu-operator-debug:latest}
DEBUG_TIMEOUT_SECONDS=${DEBUG_TIMEOUT_SECONDS:-60}

# Noise patterns from kubectl debug output that should be filtered
KUBECTL_NOISE_PATTERN="^Targeting\|^Defaulting\|^Unable\|^warning:\|^All commands\|^If you don"

# Filter out kubectl informational messages from output
filter_kubectl_noise() {
    grep -v "${KUBECTL_NOISE_PATTERN}" || true
}

# Append a section header to the bug report
append_section_header() {
    local file="$1"
    local title="$2"
    
    {
        echo ""
        echo "____________________________________________"
        echo ""
        echo "${title}"
        echo ""
    } >> "${file}"
}

# Collect diagnostic output using debug container and append to bug report
# Args: $1=pod_name, $2=node_name, $3=command, $4=command_args, $5=output_file
collect_debug_diagnostic() {
    local pod_name="$1"
    local node_name="$2"
    local cmd="$3"
    local cmd_args="$4"
    local output_file="$5"
    
    append_section_header "${output_file}" "${cmd} ${cmd_args} output (via must-gather extended diagnostics)"
    
    # Use -i to attach stdin (required to capture output)
    if ! timeout "${DEBUG_TIMEOUT_SECONDS}" $K debug -n "${OPERATOR_NAMESPACE}" "${pod_name}" \
        --image="${DEBUG_CONTAINER_IMAGE}" \
        --target=nvidia-driver-ctr \
        --profile=sysadmin \
        -i \
        -- ${cmd} ${cmd_args} 2>/dev/null | filter_kubectl_noise >> "${output_file}"; then
        echo "Warning: Failed to collect ${cmd} from ${node_name} (timed out or failed)" >&2
        echo "(collection failed or timed out after ${DEBUG_TIMEOUT_SECONDS}s)" >> "${output_file}"
    fi
}


K=kubectl
if ! $K version > /dev/null; then
    K=oc

    if ! $K version > /dev/null; then
        echo "FATAL: neither 'kubectl' nor 'oc' appear to be working properly. Exiting ..."
        exit 1
    fi
fi

if [[ "$0" == "/usr/bin/gather" ]]; then
    echo "Running as must-gather plugin image"
    export ARTIFACT_DIR=/must-gather
else
    if [ -z "${ARTIFACT_DIR:-}" ]; then
        export ARTIFACT_DIR="/tmp/nvidia-gpu-operator_$(date +%Y%m%d_%H%M)"
    fi
    echo "Using ARTIFACT_DIR=${ARTIFACT_DIR}"
fi

mkdir -p "${ARTIFACT_DIR}"

echo

exec 1> >(tee "${ARTIFACT_DIR}/must-gather.log")
exec 2> "${ARTIFACT_DIR}/must-gather.stderr.log"

if [[ "$0" == "/usr/bin/gather" ]]; then
    echo "NVIDIA GPU Operator" > "${ARTIFACT_DIR}/version"
    echo "${VERSION:-N/A}" >> "${ARTIFACT_DIR}/version"
fi

ocp_cluster=$($K get clusterversion/version --ignore-not-found -oname || true)

if [[ "$ocp_cluster" ]]; then
    echo "Running in OpenShift."
    echo "Get the cluster version"
    $K get clusterversion/version -oyaml > "${ARTIFACT_DIR}/openshift_version.yaml"
fi

echo
echo "#"
echo "# KubeVirt HyperConverged Resources"
echo "#"
echo

HYPERCONVERGED_RESOURCE=$($K get hyperconvergeds.hco.kubevirt.io -A -oname --ignore-not-found)

if [[ "$HYPERCONVERGED_RESOURCE" ]]; then
    echo "Get HyperConverged YAML"
    $K get hyperconvergeds.hco.kubevirt.io -A -oyaml > $ARTIFACT_DIR/hyperconverged.yaml
else
    echo "HyperConverged resource(s) not found in the cluster."
fi

echo "Get the operator namespaces"
OPERATOR_POD_NAME=$($K get pods -lapp=gpu-operator -oname -A)

if [ -z "$OPERATOR_POD_NAME" ]; then
    echo "FATAL: could not find the GPU Operator Pod ..."
    exit 1
fi

OPERATOR_NAMESPACE=$($K get pods -lapp=gpu-operator -A -ojsonpath='{.items[].metadata.namespace}' --ignore-not-found)

echo "Using '$OPERATOR_NAMESPACE' as operator namespace"
echo ""

echo
echo "#"
echo "# KubeVirt Resources"
echo "#"
echo

KUBEVIRT_RESOURCE=$($K get kubevirts.kubevirt.io -A -oname --ignore-not-found)

if [[ "$KUBEVIRT_RESOURCE" ]]; then
    echo "Get KubeVirt YAML"
    $K get kubevirts.kubevirt.io -A -oyaml > $ARTIFACT_DIR/kubevirt.yaml
else
    echo "KubeVirt resource(s) not found in the cluster."
fi

echo "#"
echo "# ClusterPolicy"
echo "#"
echo

CLUSTER_POLICY_NAME=$($K get clusterpolicies.nvidia.com -oname)

if [[ "${CLUSTER_POLICY_NAME}" ]]; then
    echo "Get ${CLUSTER_POLICY_NAME}"
    $K get -oyaml "${CLUSTER_POLICY_NAME}" > "${ARTIFACT_DIR}/cluster_policy.yaml"
else
    echo "Mark the ClusterPolicy as missing"
    touch "${ARTIFACT_DIR}/cluster_policy.missing"
fi

echo
echo "#"
echo "# Nodes and machines"
echo "#"
echo

if [ "$ocp_cluster" ]; then
    echo "Get all the machines"
    $K get machines -A > "${ARTIFACT_DIR}/all_machines.list"
fi

echo "Get the labels of the nodes with NVIDIA PCI cards"

GPU_PCI_LABELS=(feature.node.kubernetes.io/pci-10de.present feature.node.kubernetes.io/pci-0302_10de.present feature.node.kubernetes.io/pci-0300_10de.present)

gpu_pci_nodes=""
for label in "${GPU_PCI_LABELS[@]}"; do
    gpu_pci_nodes="$gpu_pci_nodes $($K get nodes -l$label -oname)"
done

if [ -z "$gpu_pci_nodes" ]; then
    echo "FATAL: could not find nodes with NVIDIA PCI labels"
    exit 0
fi

for node in $(echo "$gpu_pci_nodes"); do
    echo "${node}" | cut -d/ -f2 >> "${ARTIFACT_DIR}/gpu_nodes.labels"
    $K get "${node}" '-ojsonpath={.metadata.labels}' \
        | sed 's|,|,- |g' \
        | tr ',' '\n' \
        | sed 's/{"/- /' \
        | tr : = \
        | sed 's/"//g' \
        | sed 's/}/\n/' \
              >> "${ARTIFACT_DIR}/gpu_nodes.labels"
    echo "" >> "${ARTIFACT_DIR}/gpu_nodes.labels"
done

echo "Get the GPU nodes (status)"
$K get nodes -l nvidia.com/gpu.present=true -o wide > "${ARTIFACT_DIR}/gpu_nodes.status"

echo "Get the GPU nodes (description)"
$K describe nodes -l nvidia.com/gpu.present=true > "${ARTIFACT_DIR}/gpu_nodes.descr"

echo ""
echo "#"
echo "# Operator Pod"
echo "#"
echo

echo "Get the GPU Operator Pod (status)"
$K get "${OPERATOR_POD_NAME}" \
    -owide \
    -n "${OPERATOR_NAMESPACE}" \
    > "${ARTIFACT_DIR}/gpu_operator_pod.status"

echo "Get the GPU Operator Pod (yaml)"
$K get "${OPERATOR_POD_NAME}" \
    -oyaml \
    -n "${OPERATOR_NAMESPACE}" \
    > "${ARTIFACT_DIR}/gpu_operator_pod.yaml"

echo "Get the GPU Operator Pod logs"
$K logs "${OPERATOR_POD_NAME}" \
    -n "${OPERATOR_NAMESPACE}" \
    > "${ARTIFACT_DIR}/gpu_operator_pod.log"

$K logs "${OPERATOR_POD_NAME}" \
    -n "${OPERATOR_NAMESPACE}" \
    --previous \
    > "${ARTIFACT_DIR}/gpu_operator_pod.previous.log"

echo ""
echo "#"
echo "# Operand Pods"
echo "#"
echo ""

echo "Get the Pods in ${OPERATOR_NAMESPACE} (status)"
$K get pods -owide \
    -n "${OPERATOR_NAMESPACE}" \
    > "${ARTIFACT_DIR}/gpu_operand_pods.status"

echo "Get the Pods in ${OPERATOR_NAMESPACE} (yaml)"
$K get pods -oyaml \
    -n "${OPERATOR_NAMESPACE}" \
    > "${ARTIFACT_DIR}/gpu_operand_pods.yaml"

echo "Get the GPU Operator Pods Images"
$K get pods -n "${OPERATOR_NAMESPACE}" \
    -o=jsonpath='{range .items[*]}{"\n"}{.metadata.name}{":\t"}{range .spec.containers[*]}{.image}{" "}{end}{end}' \
    > "${ARTIFACT_DIR}/gpu_operand_pod_images.txt"

echo "Get the description and logs of the GPU Operator Pods"

for pod in $($K get pods -n "${OPERATOR_NAMESPACE}" -oname); 
do
    if ! $K get "${pod}" -n "${OPERATOR_NAMESPACE}" -ojsonpath='{.metadata.labels}' | grep -E --quiet '(nvidia|gpu)'; then
        echo "Skipping $pod, not a NVIDA/GPU Pod ..."
        continue
    fi
    pod_name=$(echo "$pod" | cut -d/ -f2)

    if [ "${pod}" == "${OPERATOR_POD_NAME}" ]; then
        echo "Skipping operator pod $pod_name ..."
        continue
    fi

    $K logs "${pod}" \
        -n "${OPERATOR_NAMESPACE}" \
        --all-containers --prefix \
        > "${ARTIFACT_DIR}/gpu_operand_pod_$pod_name.log"

    $K logs "${pod}" \
        -n "${OPERATOR_NAMESPACE}" \
        --all-containers --prefix \
        --previous \
        > "${ARTIFACT_DIR}/gpu_operand_pod_$pod_name.previous.log"

    $K describe "${pod}" \
        -n "${OPERATOR_NAMESPACE}" \
        > "${ARTIFACT_DIR}/gpu_operand_pod_$pod_name.descr"
done

echo ""
echo "#"
echo "# Operand DaemonSets"
echo "#"
echo ""

echo "Get the DaemonSets in $OPERATOR_NAMESPACE (status)"

$K get ds \
    -n "${OPERATOR_NAMESPACE}" \
    > "${ARTIFACT_DIR}/gpu_operand_ds.status"

echo "Get the DaemonSets in $OPERATOR_NAMESPACE (yaml)"

$K get ds -oyaml \
    -n "${OPERATOR_NAMESPACE}" \
    > "${ARTIFACT_DIR}/gpu_operand_ds.yaml"

echo "Get the description of the GPU Operator DaemonSets"

for ds in $($K get ds -n "${OPERATOR_NAMESPACE}" -oname);
do
    if ! $K get "${ds}" -n "${OPERATOR_NAMESPACE}" -ojsonpath='{.metadata.labels}' | grep -E --quiet '(nvidia|gpu)'; then
        echo "Skipping ${ds}, not a NVIDA/GPU DaemonSet ..."
        continue
    fi
    $K describe "${ds}" \
        -n "${OPERATOR_NAMESPACE}" \
        > "${ARTIFACT_DIR}/gpu_operand_ds_$(echo "$ds" | cut -d/ -f2).descr"
done

echo ""
echo "#"
echo "# nvidia-bug-report.sh"
echo "#"
echo ""

if [[ "${ENABLE_EXTENDED_DIAGNOSTICS}" == "true" ]]; then
    echo "==============================================================================="
    echo "WARNING: Extended diagnostics enabled."
    echo ""
    echo "This will pull and run an external debug container (${DEBUG_CONTAINER_IMAGE})"
    echo "with privileged access to collect system information (dmidecode, lspci)."
    echo ""
    echo "By enabling this option, you acknowledge:"
    echo "  - An external container image will be pulled and executed in your cluster"
    echo "  - The debug container requires privileged access (sysadmin profile)"
    echo "  - System hardware information will be collected and included in the bug report"
    echo ""
    echo "To disable, unset ENABLE_EXTENDED_DIAGNOSTICS or set it to false."
    echo "==============================================================================="
    echo ""
fi

for pod in $($K get pods -lopenshift.driver-toolkit -oname -n "${OPERATOR_NAMESPACE}"; $K get pods -lapp=nvidia-driver-daemonset -oname -n "${OPERATOR_NAMESPACE}"; $K get pods -lapp=nvidia-vgpu-manager-daemonset -oname -n "${OPERATOR_NAMESPACE}");
do
    pod_nodename=$($K get "${pod}" -ojsonpath={.spec.nodeName} -n "${OPERATOR_NAMESPACE}")
    pod_name=$(basename "${pod}")
    echo "Saving nvidia-bug-report from ${pod_nodename} ..."

    # Collect standard nvidia-bug-report from driver container
    if ! $K exec -n "${OPERATOR_NAMESPACE}" "${pod}" -- bash -c 'cd /tmp && nvidia-bug-report.sh' >&2; then
        echo "Failed to collect nvidia-bug-report from ${pod_nodename}"
        continue
    fi

    # Clean up any existing temp file to avoid permission issues
    rm -f /tmp/nvidia-bug-report.log.gz
    
    if ! $K cp "${OPERATOR_NAMESPACE}"/"${pod_name}":/tmp/nvidia-bug-report.log.gz /tmp/nvidia-bug-report.log.gz 2>/dev/null; then
        echo "Failed to save nvidia-bug-report from ${pod_nodename}"
        continue
    fi


    mv /tmp/nvidia-bug-report.log.gz "${ARTIFACT_DIR}/nvidia-bug-report_${pod_nodename}.log.gz"

    if [[ "${ENABLE_EXTENDED_DIAGNOSTICS}" == "true" ]]; then
        echo "Collecting extended diagnostics (dmidecode/lspci) from ${pod_nodename}..."
        
        bug_report_file="${ARTIFACT_DIR}/nvidia-bug-report_${pod_nodename}.log"
        
        # Decompress the bug report to append data
        if ! gunzip "${bug_report_file}.gz" 2>&1; then
            echo "Warning: Failed to decompress bug report for ${pod_nodename}, skipping extended diagnostics"
            continue
        fi
        
        append_section_header "${bug_report_file}" "*** EXTENDED DIAGNOSTICS (from debug container) ***"
        
        collect_debug_diagnostic "${pod_name}" "${pod_nodename}" "dmidecode" "" "${bug_report_file}"
        collect_debug_diagnostic "${pod_name}" "${pod_nodename}" "lspci" "-vvv" "${bug_report_file}"
        
        # Recompress the bug report
        if ! gzip "${bug_report_file}" 2>&1; then
            echo "Warning: Failed to recompress bug report for ${pod_nodename}"
        fi
    else
        echo "NOTE: For extended diagnostics (dmidecode/lspci), set ENABLE_EXTENDED_DIAGNOSTICS=true"
    fi
done

echo ""
echo "#"
echo "# All done!"
if [[ "$0" != "/usr/bin/gather" ]]; then
    echo "# Logs saved into ${ARTIFACT_DIR}."
fi
echo "#"
