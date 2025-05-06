#!/bin/bash

source ${SCRIPT_DIR}/.definitions.sh

collect_logs() {
  # Ensure the log directory exists
  mkdir -p ${LOG_DIR}

  pods="$(kubectl get --all-namespaces pods -o json | jq '.items[] | {name: .metadata.name, ns: .metadata.namespace}' | jq -s -c .)"
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
}