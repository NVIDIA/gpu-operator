/**
# Copyright (c) NVIDIA CORPORATION.  All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
**/

package controllers

import (
	"fmt"

	promcli "github.com/prometheus/client_golang/prometheus"

	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// OperatorMetrics defines the Prometheus metrics exposed for the
// operator status
type OperatorMetrics struct {
	gpuNodesTotal promcli.Gauge

	reconciliationLastSuccess  promcli.Gauge
	reconciliationStatus       promcli.Gauge
	reconciliationTotal        promcli.Counter
	reconciliationFailed       promcli.Counter
	reconciliationHasNFDLabels promcli.Gauge

	openshiftDriverToolkitEnabled          promcli.Gauge
	openshiftDriverToolkitNfdTooOld        promcli.Gauge
	openshiftDriverToolkitIsMissing        promcli.Gauge
	openshiftDriverToolkitRhcosTagsMissing promcli.Gauge
	openshiftDriverToolkitIsBroken         promcli.Gauge

	driverAutoUpgradeEnabled promcli.Gauge
	upgradesInProgress       promcli.Gauge
	upgradesDone             promcli.Gauge
	upgradesFailed           promcli.Gauge
	upgradesAvailable        promcli.Gauge
	upgradesPending          promcli.Gauge
}

const (
	reconciliationStatusSuccess                  = 1
	reconciliationStatusNotReady                 = 0
	reconciliationStatusClusterPolicyUnavailable = -1
	reconciliationStatusClusterOperatorError     = -2

	openshiftDriverToolkitEnabled     = 1
	openshiftDriverToolkitDisabled    = 0
	openshiftDriverToolkitNotPossible = -1

	driverAutoUpgradeEnabled  = 1
	driverAutoUpgradeDisabled = 0

	// operatorMetricsNamespace is the name of the namespace used for the GPU Operator metrics.
	operatorMetricsNamespace = "gpu_operator"
)

func initOperatorMetrics() *OperatorMetrics {
	m := &OperatorMetrics{
		gpuNodesTotal: promcli.NewGauge(
			promcli.GaugeOpts{
				Namespace: operatorMetricsNamespace,
				Name:      "gpu_nodes_total",
				Help:      "Number of nodes with GPUs",
			},
		),
		reconciliationLastSuccess: promcli.NewGauge(
			promcli.GaugeOpts{
				Namespace: operatorMetricsNamespace,
				Name:      "reconciliation_last_success_ts_seconds",
				Help:      "Timestamp (in seconds) of the last reconciliation loop success",
			},
		),
		reconciliationStatus: promcli.NewGauge(
			promcli.GaugeOpts{
				Namespace: operatorMetricsNamespace,
				Name:      "reconciliation_status",
				Help: fmt.Sprintf("%d if the reconciliation is currently successful, %d if the operands are not ready, %d if the cluster policy is unavailable, %d if an error occurred within the operator.",
					reconciliationStatusSuccess,
					reconciliationStatusNotReady,
					reconciliationStatusClusterPolicyUnavailable,
					reconciliationStatusClusterOperatorError),
			},
		),
		reconciliationTotal: promcli.NewCounter(
			promcli.CounterOpts{
				Namespace: operatorMetricsNamespace,
				Name:      "reconciliation_total",
				Help:      "Total number of reconciliation",
			},
		),
		reconciliationFailed: promcli.NewCounter(
			promcli.CounterOpts{
				Namespace: operatorMetricsNamespace,
				Name:      "reconciliation_failed_total",
				Help:      "Number of failed reconciliation",
			},
		),
		reconciliationHasNFDLabels: promcli.NewGauge(
			promcli.GaugeOpts{
				Namespace: operatorMetricsNamespace,
				Name:      "reconciliation_has_nfd_labels",
				Help:      "1 if NFD mandatory kernel labels have been found, 0 otherwise",
			},
		),

		openshiftDriverToolkitEnabled: promcli.NewGauge(
			promcli.GaugeOpts{
				Namespace: operatorMetricsNamespace,
				Name:      "openshift_driver_toolkit_enabled",
				Help:      "1 if OCP DriverToolkit is enabled, -1 if requested but could not be enabled, 0 if not requested",
			},
		),
		openshiftDriverToolkitNfdTooOld: promcli.NewGauge(
			promcli.GaugeOpts{
				Namespace: operatorMetricsNamespace,
				Name:      "openshift_driver_toolkit_nfd_too_old",
				Help:      "1 if OCP DriverToolkit is enabled but NFD doesn't expose OSTREE labels, 0 otherwise",
			},
		),
		openshiftDriverToolkitIsMissing: promcli.NewGauge(
			promcli.GaugeOpts{
				Namespace: operatorMetricsNamespace,
				Name:      "openshift_driver_toolkit_imagestream_missing",
				Help:      "1 if OCP DriverToolkit is enabled but its imagestream is not available, 0 otherwise",
			},
		),
		openshiftDriverToolkitRhcosTagsMissing: promcli.NewGauge(
			promcli.GaugeOpts{
				Namespace: operatorMetricsNamespace,
				Name:      "openshift_driver_toolkit_rhcos_tags_missing",
				Help:      "1 if OCP DriverToolkit is enabled but some of the RHCOS tags are missing, 0 otherwise",
			},
		),
		openshiftDriverToolkitIsBroken: promcli.NewGauge(
			promcli.GaugeOpts{
				Namespace: operatorMetricsNamespace,
				Name:      "openshift_driver_toolkit_imagestream_broken",
				Help:      "1 if OCP DriverToolkit is enabled but its imagestream is broken (rhbz#2015024), 0 otherwise",
			},
		),
		driverAutoUpgradeEnabled: promcli.NewGauge(
			promcli.GaugeOpts{
				Namespace: operatorMetricsNamespace,
				Name:      "driver_auto_upgrade_enabled",
				Help:      "1 if driver auto upgrade is enabled 0 if not",
			},
		),
		upgradesInProgress: promcli.NewGauge(
			promcli.GaugeOpts{
				Namespace: operatorMetricsNamespace,
				Name:      "nodes_upgrades_in_progress",
				Help:      "Total number of nodes on which the gpu operator pods are being upgraded",
			},
		),
		upgradesDone: promcli.NewGauge(
			promcli.GaugeOpts{
				Namespace: operatorMetricsNamespace,
				Name:      "nodes_upgrades_done",
				Help:      "Total number of nodes on which the gpu operator pods are successfully upgraded",
			},
		),
		upgradesFailed: promcli.NewGauge(
			promcli.GaugeOpts{
				Namespace: operatorMetricsNamespace,
				Name:      "nodes_upgrades_failed",
				Help:      "Total number of nodes on which the gpu operator pod upgrades have failed",
			},
		),
		upgradesAvailable: promcli.NewGauge(
			promcli.GaugeOpts{
				Namespace: operatorMetricsNamespace,
				Name:      "nodes_upgrades_available",
				Help:      "Total number of nodes on which the gpu operator pod upgrades can be done",
			},
		),
		upgradesPending: promcli.NewGauge(
			promcli.GaugeOpts{
				Namespace: operatorMetricsNamespace,
				Name:      "nodes_upgrades_pending",
				Help:      "Total number of nodes on which the gpu operator pod upgrades are pending",
			},
		),
	}

	metrics.Registry.MustRegister(
		m.gpuNodesTotal,

		m.reconciliationLastSuccess,
		m.reconciliationStatus,
		m.reconciliationTotal,
		m.reconciliationFailed,
		m.reconciliationHasNFDLabels,

		m.openshiftDriverToolkitEnabled,
		m.openshiftDriverToolkitNfdTooOld,
		m.openshiftDriverToolkitIsMissing,
		m.openshiftDriverToolkitRhcosTagsMissing,
		m.openshiftDriverToolkitIsBroken,

		m.driverAutoUpgradeEnabled,
		m.upgradesInProgress,
		m.upgradesDone,
		m.upgradesAvailable,
		m.upgradesFailed,
		m.upgradesPending,
	)

	return m
}
