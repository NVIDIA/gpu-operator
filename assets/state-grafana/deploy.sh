#!/usr/bin/env bash

prom_url=`oc get secrets -n openshift-monitoring grafana-datasources -o go-template='{{index .data "prometheus.yaml"}}' | base64 --decode | jq '.datasources[0].url'`
prom_user="internal"
prom_pass=`oc get secrets -n openshift-monitoring grafana-datasources -o go-template='{{index .data "prometheus.yaml"}}' | base64 --decode | jq '.datasources[0].basicAuthPassword'`

oc new-project scale-ci-grafana
oc process -f grafana.yml -p "PROMETHEUS_URL=${prom_url}" -p "PROMETHEUS_USER=${prom_user}" -p "PROMETHEUS_PASSWORD=${prom_pass}" | oc create -f -
