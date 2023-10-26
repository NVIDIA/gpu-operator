#!/usr/bin/env bash

set -o nounset
set -x

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
    echo "Using ARTIFACT_DIR=$ARTIFACT_DIR"
fi

mkdir -p "$ARTIFACT_DIR"

echo

exec 1> >(tee $ARTIFACT_DIR/must-gather.log)
exec 2> $ARTIFACT_DIR/must-gather.stderr.log

if [[ "$0" == "/usr/bin/gather" ]]; then
    echo "NVIDIA GPU Operator" > $ARTIFACT_DIR/version
    echo "${VERSION:-N/A}" >> $ARTIFACT_DIR/version
fi

ocp_cluster=$($K get clusterversion/version --ignore-not-found -oname || true)

if [[ "$ocp_cluster" ]]; then
    echo "Running in OpenShift."
    echo "Get the cluster version"
    $K get clusterversion/version -oyaml > $ARTIFACT_DIR/openshift_version.yaml
fi

echo "Get the operator namespaces"
OPERATOR_POD_NAME=$($K get pods -lapp=gpu-operator -oname -A)

if [ -z "$OPERATOR_POD_NAME" ]; then
    echo "FATAL: could not find the GPU Operator Pod ..."
    exit 1
fi

OPERATOR_NAMESPACE=$($K get pods -lapp=gpu-operator -A -ojsonpath={.items[].metadata.namespace} --ignore-not-found)

echo "Using '$OPERATOR_NAMESPACE' as operator namespace"
echo ""

echo "#"
echo "# ClusterPolicy"
echo "#"
echo

CLUSTER_POLICY_NAME=$($K get clusterpolicy -oname)

if [[ "$CLUSTER_POLICY_NAME" ]]; then
    echo "Get $CLUSTER_POLICY_NAME"
    $K get -oyaml $CLUSTER_POLICY_NAME > $ARTIFACT_DIR/cluster_policy.yaml
else
    echo "Mark the ClusterPolicy as missing"
    touch $ARTIFACT_DIR/cluster_policy.missing
fi

echo
echo "#"
echo "# Nodes and machines"
echo "#"
echo

if [ "$ocp_cluster" ]; then
    echo "Get all the machines"
    $K get machines -A > $ARTIFACT_DIR/all_machines.list
fi

echo "Get the labels of the nodes with NVIDIA PCI cards"

GPU_PCI_LABELS=(feature.node.kubernetes.io/pci-10de.present feature.node.kubernetes.io/pci-0302_10de.present feature.node.kubernetes.io/pci-0300_10de.present)

gpu_pci_nodes=""
for label in ${GPU_PCI_LABELS[@]}; do
    gpu_pci_nodes="$gpu_pci_nodes $($K get nodes -l$label -oname)"
done

if [ -z "$gpu_pci_nodes" ]; then
    echo "FATAL: could not find nodes with NVIDIA PCI labels"
    exit 0
fi

for node in $(echo "$gpu_pci_nodes"); do
    echo "$node" | cut -d/ -f2 >> $ARTIFACT_DIR/gpu_nodes.labels
    $K get $node '-ojsonpath={.metadata.labels}' \
        | sed 's|,|,- |g' \
        | tr ',' '\n' \
        | sed 's/{"/- /' \
        | tr : = \
        | sed 's/"//g' \
        | sed 's/}/\n/' \
              >> $ARTIFACT_DIR/gpu_nodes.labels
    echo "" >> $ARTIFACT_DIR/gpu_nodes.labels
done

echo "Get the GPU nodes (status)"
$K get nodes -l nvidia.com/gpu.present=true > $ARTIFACT_DIR/gpu_nodes.status

echo "Get the GPU nodes (description)"
$K describe nodes -l nvidia.com/gpu.present=true > $ARTIFACT_DIR/gpu_nodes.descr

echo ""
echo "#"
echo "# Operator Pod"
echo "#"
echo

echo "Get the GPU Operator Pod (status)"
$K get $OPERATOR_POD_NAME \
    -owide \
    -n $OPERATOR_NAMESPACE \
    > $ARTIFACT_DIR/gpu_operator_pod.status

echo "Get the GPU Operator Pod (yaml)"
$K get $OPERATOR_POD_NAME \
    -oyaml \
    -n $OPERATOR_NAMESPACE \
    > $ARTIFACT_DIR/gpu_operator_pod.yaml

echo "Get the GPU Operator Pod logs"
$K logs $OPERATOR_POD_NAME \
    -n $OPERATOR_NAMESPACE \
    > "$ARTIFACT_DIR/gpu_operator_pod.log"

$K logs $OPERATOR_POD_NAME \
    -n $OPERATOR_NAMESPACE \
    --previous \
    > "$ARTIFACT_DIR/gpu_operator_pod.previous.log"

echo ""
echo "#"
echo "# Operand Pods"
echo "#"
echo ""

echo "Get the Pods in $OPERATOR_NAMESPACE (status)"
$K get pods -owide \
    -n $OPERATOR_NAMESPACE \
    > $ARTIFACT_DIR/gpu_operand_pods.status

echo "Get the Pods in $OPERATOR_NAMESPACE (yaml)"
$K get pods -oyaml \
    -n $OPERATOR_NAMESPACE \
    > $ARTIFACT_DIR/gpu_operand_pods.yaml

echo "Get the GPU Operator Pods Images"
$K get pods -n $OPERATOR_NAMESPACE \
    -o=jsonpath='{range .items[*]}{"\n"}{.metadata.name}{":\t"}{range .spec.containers[*]}{.image}{" "}{end}{end}' \
    > $ARTIFACT_DIR/gpu_operand_pod_images.txt

echo "Get the description and logs of the GPU Operator Pods"

for pod in $($K get pods -n $OPERATOR_NAMESPACE -oname);
do
    if ! $K get $pod -n $OPERATOR_NAMESPACE -ojsonpath={.metadata.labels} | egrep --quiet '(nvidia|gpu)'; then
        echo "Skipping $pod, not a NVIDA/GPU Pod ..."
        continue
    fi
    pod_name=$(echo "$pod" | cut -d/ -f2)

    if [ $pod == $OPERATOR_POD_NAME ]; then
        echo "Skipping operator pod $pod_name ..."
        continue
    fi

    $K logs $pod \
        -n $OPERATOR_NAMESPACE \
        --all-containers --prefix \
        > $ARTIFACT_DIR/gpu_operand_pod_$pod_name.log

    $K logs $pod \
        -n $OPERATOR_NAMESPACE \
        --all-containers --prefix \
        --previous \
        > $ARTIFACT_DIR/gpu_operand_pod_$pod_name.previous.log

    $K describe $pod \
        -n $OPERATOR_NAMESPACE \
        > $ARTIFACT_DIR/gpu_operand_pod_$pod_name.descr
done

echo ""
echo "#"
echo "# Operand DaemonSets"
echo "#"
echo ""

echo "Get the DaemonSets in $OPERATOR_NAMESPACE (status)"

$K get ds \
    -n $OPERATOR_NAMESPACE \
    > $ARTIFACT_DIR/gpu_operand_ds.status


echo "Get the DaemonSets in $OPERATOR_NAMESPACE (yaml)"

$K get ds -oyaml \
    -n $OPERATOR_NAMESPACE \
    > $ARTIFACT_DIR/gpu_operand_ds.yaml

echo "Get the description of the GPU Operator DaemonSets"

for ds in $($K get ds -n $OPERATOR_NAMESPACE -oname);
do
    if ! $K get $ds -n $OPERATOR_NAMESPACE -ojsonpath={.metadata.labels} | egrep --quiet '(nvidia|gpu)'; then
        echo "Skipping $ds, not a NVIDA/GPU DaemonSet ..."
        continue
    fi
    $K describe $ds \
        -n $OPERATOR_NAMESPACE \
        > $ARTIFACT_DIR/gpu_operand_ds_$(echo "$ds" | cut -d/ -f2).descr
done

echo ""
echo "#"
echo "# nvidia-bug-report.sh"
echo "#"
echo ""

for pod in $($K get pods -lopenshift.driver-toolkit -oname -n $OPERATOR_NAMESPACE; $K get pods -lapp=nvidia-driver-daemonset -oname -n $OPERATOR_NAMESPACE; $K get pods -lapp=nvidia-vgpu-manager-daemonset -oname -n $OPERATOR_NAMESPACE);
do
    pod_nodename=$($K get $pod -ojsonpath={.spec.nodeName} -n $OPERATOR_NAMESPACE)
    echo "Saving nvidia-bug-report from ${pod_nodename} ..."

    $K exec -n $OPERATOR_NAMESPACE $pod -- bash -c 'cd /tmp && nvidia-bug-report.sh' >&2 || \
        (echo "Failed to collect nvidia-bug-report from ${pod_nodename}" && continue)

    $K cp $OPERATOR_NAMESPACE/$(basename $pod):/tmp/nvidia-bug-report.log.gz /tmp/nvidia-bug-report.log.gz || \
        (echo "Failed to save nvidia-bug-report from ${pod_nodename}" && continue)

    mv /tmp/nvidia-bug-report.log.gz $ARTIFACT_DIR/nvidia-bug-report_${pod_nodename}.log.gz
done

echo ""
echo "#"
echo "# All done!"
if [[ "$0" != "/usr/bin/gather" ]]; then
    echo "# Logs saved into ${ARTIFACT_DIR}."
fi
echo "#"
