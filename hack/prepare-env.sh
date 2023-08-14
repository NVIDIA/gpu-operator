#! /bin/bash

dest=config/manager/.env

env=$(cat bundle/manifests/gpu-operator-certified.clusterserviceversion.yaml \
| yq '.spec.install.spec.deployments[].spec.template.spec.containers[].env[] | with_entries(select(.value != "OPERATOR_NAMESPACE"))' \
| jq 'select( .name != null and .value != null) | .name, "=", .value, ";"' -r )
env=${env//$'\n'/}
echo $env > ${dest}
sed -i 's/;/\n/g' ${dest}