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

package state

import (
	"github.com/prometheus/client_golang/prometheus"

	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// StateDurationSeconds records the time spent per reconcile in each state,
// labeled by the owning controller's CRD kind and the state name.
var StateDurationSeconds = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Namespace: "gpu_operator",
		Name:      "state_seconds",
		Help:      "Time spent per reconcile in each state",
		Buckets:   []float64{0.01, 0.05, 0.1, 0.5, 1, 5, 15, 60, 300},
	},
	[]string{"controller", "state"},
)

func init() {
	metrics.Registry.MustRegister(StateDurationSeconds)
}
