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

// Package predicates holds predicates the upgrade controller registers on the
// k8s-operator-libs upgrade state manager.
package predicates

import (
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"

	"github.com/NVIDIA/k8s-operator-libs/pkg/consts"
	"github.com/NVIDIA/k8s-operator-libs/pkg/upgrade"

	driverconfig "github.com/NVIDIA/gpu-operator/internal/config"
)

// DriverPodRestartOnly returns the upgrade controller's RestartOnlyPredicate: it allows an
// out-of-sync driver pod to be restarted in place when the running pod spec and the desired
// DaemonSet template spec have the same DRIVER_CONFIG_DIGEST, i.e. the install-relevant
// config is unchanged (e.g. only a helm.sh/chart label changed). If either digest is missing,
// it returns false and the node takes the full upgrade flow.
func DriverPodRestartOnly(log logr.Logger) upgrade.RestartOnlyPredicate {
	return func(running, desired *corev1.PodSpec) (bool, error) {
		desiredDigest := driverconfig.DriverConfigDigestFromPodSpec(desired)
		runningDigest := driverconfig.DriverConfigDigestFromPodSpec(running)
		if desiredDigest == "" || runningDigest == "" {
			log.V(consts.LogLevelDebug).Info("driver config digest missing; taking full upgrade flow",
				"desiredDigest", desiredDigest, "runningDigest", runningDigest)
			return false, nil
		}
		restartOnly := desiredDigest == runningDigest
		log.V(consts.LogLevelDebug).Info("evaluated driver config digest for restart-only routing",
			"desiredDigest", desiredDigest, "runningDigest", runningDigest, "restartOnly", restartOnly)
		return restartOnly, nil
	}
}
