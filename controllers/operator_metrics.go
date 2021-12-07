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
}

const (
	reconciliationStatusSuccess                  = 1
	reconciliationStatusNotReady                 = 0
	reconciliationStatusClusterPolicyUnavailable = -1
	reconciliationStatusClusterOperatorError     = -2

	openshiftDriverToolkitEnabled     = 1
	openshiftDriverToolkitDisabled    = 0
	openshiftDriverToolkitNotPossible = -1
)

func initOperatorMetrics(n *ClusterPolicyController) *OperatorMetrics {
	m := &OperatorMetrics{
		gpuNodesTotal: promcli.NewGauge(
			promcli.GaugeOpts{
				Name: "gpu_operator_gpu_nodes_total",
				Help: "Number of nodes with GPUs",
			},
		),
		reconciliationLastSuccess: promcli.NewGauge(
			promcli.GaugeOpts{
				Name: "gpu_operator_reconciliation_last_success_ts_seconds",
				Help: "Timestamp (in seconds) of the last reconciliation loop success",
			},
		),
		reconciliationStatus: promcli.NewGauge(
			promcli.GaugeOpts{
				Name: "gpu_operator_reconciliation_status",
				Help: fmt.Sprintf("%d if the reconciliation is currently successful, %d if the operands are not ready, %d if the cluster policy is unavailable, %d if an error occurred within the operator.",
					reconciliationStatusSuccess,
					reconciliationStatusNotReady,
					reconciliationStatusClusterPolicyUnavailable,
					reconciliationStatusClusterOperatorError),
			},
		),
		reconciliationTotal: promcli.NewCounter(
			promcli.CounterOpts{
				Name: "gpu_operator_reconciliation_total",
				Help: "Total number of reconciliation",
			},
		),
		reconciliationFailed: promcli.NewCounter(
			promcli.CounterOpts{
				Name: "gpu_operator_reconciliation_failed_total",
				Help: "Number of failed reconciliation",
			},
		),
		reconciliationHasNFDLabels: promcli.NewGauge(
			promcli.GaugeOpts{
				Name: "gpu_operator_reconciliation_has_nfd_labels",
				Help: "1 if NFD mandatory kernel labels have been found, 0 otherwise",
			},
		),

		openshiftDriverToolkitEnabled: promcli.NewGauge(
			promcli.GaugeOpts{
				Name: "gpu_operator_openshift_driver_toolkit_enabled",
				Help: "1 if OCP DriverToolkit is enabled, -1 if requested but could not be enabled, 0 if not requested",
			},
		),
		openshiftDriverToolkitNfdTooOld: promcli.NewGauge(
			promcli.GaugeOpts{
				Name: "gpu_operator_openshift_driver_toolkit_nfd_too_old",
				Help: "1 if OCP DriverToolkit is enabled but NFD doesn't expose OSTREE labels, 0 otherwise",
			},
		),
		openshiftDriverToolkitIsMissing: promcli.NewGauge(
			promcli.GaugeOpts{
				Name: "gpu_operator_openshift_driver_toolkit_imagestream_missing",
				Help: "1 if OCP DriverToolkit is enabled but its imagestream is not available, 0 otherwise",
			},
		),
		openshiftDriverToolkitRhcosTagsMissing: promcli.NewGauge(
			promcli.GaugeOpts{
				Name: "gpu_operator_openshift_driver_toolkit_rhcos_tags_missing",
				Help: "1 if OCP DriverToolkit is enabled but some of the RHCOS tags are missing, 0 otherwise",
			},
		),
		openshiftDriverToolkitIsBroken: promcli.NewGauge(
			promcli.GaugeOpts{
				Name: "gpu_operator_openshift_driver_toolkit_imagestream_broken",
				Help: "1 if OCP DriverToolkit is enabled but its imagestream is broken (rhbz#2015024), 0 otherwise",
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
	)

	return m
}
