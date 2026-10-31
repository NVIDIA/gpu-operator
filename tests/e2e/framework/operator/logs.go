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

package operator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/onsi/ginkgo/v2"

	corev1 "k8s.io/api/core/v1"

	e2elog "github.com/NVIDIA/gpu-operator/tests/e2e/framework/logs"
	"github.com/NVIDIA/gpu-operator/tests/e2e/helpers"
)

func saveLogsOnFailure(ctx context.Context, clients Clients, namespaces ...string) bool {
	if !ginkgo.CurrentSpecReport().Failed() || TestContext.LogDir == "" {
		return false
	}
	saveLogs(ctx, clients, containerLogDir(), namespaces...)
	return true
}

func containerLogDir() string {
	return filepath.Join(TestContext.LogDir, ginkgo.CurrentSpecReport().ContainerHierarchyTexts[0])
}

func saveLogs(ctx context.Context, clients Clients, logDir string, namespaces ...string) {
	if err := os.MkdirAll(logDir, 0o750); err != nil {
		e2elog.Logf("WARN: failed to create log dir %s: %v", logDir, err)
		return
	}

	for _, namespace := range namespaces {
		pods, err := clients.Pods.List(ctx, namespace, "")
		if err != nil {
			e2elog.Logf("WARN: failed to list pods in %s: %v", namespace, err)
			continue
		}

		var podSummary strings.Builder
		for _, pod := range pods {
			fmt.Fprintf(&podSummary, "%s\t%s\tready=%t\n", pod.Name, pod.Status.Phase, helpers.IsPodReady(&pod))
			for _, container := range slices.Concat(pod.Spec.InitContainers, pod.Spec.Containers) {
				containerLogs, err := clients.Pods.Logs(ctx, &pod, &corev1.PodLogOptions{Container: container.Name})
				if err != nil {
					containerLogs = fmt.Sprintf("failed to read logs: %v", err)
				}
				logPath := filepath.Join(logDir, fmt.Sprintf("%s_%s_%s.log", namespace, pod.Name, container.Name))
				if err := os.WriteFile(logPath, []byte(containerLogs), 0o600); err != nil {
					e2elog.Logf("WARN: failed to write %s: %v", logPath, err)
				}
			}
		}

		summaryPath := filepath.Join(logDir, fmt.Sprintf("%s_pods.txt", namespace))
		if err := os.WriteFile(summaryPath, []byte(podSummary.String()), 0o600); err != nil {
			e2elog.Logf("WARN: failed to write %s: %v", summaryPath, err)
		}
	}
}
