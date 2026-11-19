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
	"testing"

	promcli "github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

func TestOperatorMetricsExposedOnlyAfterRegistration(t *testing.T) {
	globalBefore, err := metrics.Registry.Gather()
	require.NoError(t, err)
	m := InitOperatorMetrics()
	globalAfter, err := metrics.Registry.Gather()
	require.NoError(t, err)
	require.Len(t, globalAfter, len(globalBefore), "standby construction must not register metrics")

	registry := promcli.NewRegistry()
	const name = "gpu_operator_reconciliation_status"

	metricFamilies, err := registry.Gather()
	require.NoError(t, err)
	for _, family := range metricFamilies {
		require.NotEqual(t, name, family.GetName(), "standby must not expose reconciliation status")
	}

	m.register(registry)
	metricFamilies, err = registry.Gather()
	require.NoError(t, err)
	for _, family := range metricFamilies {
		if family.GetName() == name {
			require.Len(t, family.GetMetric(), 1)
			require.Zero(t, family.GetMetric()[0].GetGauge().GetValue())
			return
		}
	}
	t.Fatal("leader did not expose reconciliation status after registration")
}
