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

package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/onsi/ginkgo/v2"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/NVIDIA/gpu-operator/tests/e2e/framework"
	e2elog "github.com/NVIDIA/gpu-operator/tests/e2e/framework/logs"
	"github.com/NVIDIA/gpu-operator/tests/e2e/helpers"
)

// runLabel marks the namespaces this run created; framework.CreateNamespace sets it too.
const runLabel = "e2e-run"

// operatorChartValues returns the --set values that point the chart at the operator and validator under test.
func operatorChartValues() []string {
	return []string{
		fmt.Sprintf("operator.repository=%s", tcfg.operatorRepository),
		fmt.Sprintf("operator.image=%s", tcfg.operatorImage),
		fmt.Sprintf("operator.version=%s", tcfg.operatorVersion),
		fmt.Sprintf("validator.repository=%s", tcfg.validatorRepository),
		fmt.Sprintf("validator.image=%s", tcfg.validatorImage),
		fmt.Sprintf("validator.version=%s", tcfg.validatorVersion),
	}
}

type operatorInstall struct {
	clients     clusterClients
	operator    *helpers.OperatorClient
	releaseName string
}

// newOperatorInstall registers, in the calling Ordered container, a BeforeAll that installs the chart
// with chartValues on top of operatorChartValues(), a DeferCleanup that uninstalls it and deletes the
// namespace, and an AfterEach that saves pod logs when a spec failed.
func newOperatorInstall(chartValues ...string) *operatorInstall {
	install := &operatorInstall{}

	ginkgo.BeforeAll(func(ctx context.Context) {
		install.clients = newClusterClients()
		install.operator = &helpers.OperatorClient{
			Chart:        tcfg.helmChart,
			ChartVersion: tcfg.helmChartVersion,
			Namespace:    tcfg.namespace,
			Kubeconfig:   framework.TestContext.KubeConfig,
		}
		// The previous Ordered container's uninstall does not wait for its namespace to be gone.
		err := framework.WaitForNamespacesDeleted(ctx, install.clients.kube, []string{tcfg.namespace}, framework.DefaultNamespaceDeletionTimeout)
		framework.ExpectNoError(err, "namespace %s was not deleted", tcfg.namespace)

		// Registered before the namespace is created, so a namespace whose create response was lost
		// is still removed, and before Install so a release whose --wait timed out is still
		// uninstalled.
		install.releaseName = fmt.Sprintf("gpu-operator-%s", framework.RandomSuffix())
		ginkgo.DeferCleanup(install.uninstall)

		_, err = framework.CreateNamespace(ctx, tcfg.namespace, install.clients.kube, map[string]string{
			runLabel: string(framework.RunID),
		})
		framework.ExpectNoError(err, "failed to create namespace %s", tcfg.namespace)
		_, err = install.operator.Install(ctx, append(operatorChartValues(), chartValues...), helpers.ChartOptions{
			ReleaseName: install.releaseName,
			Timeout:     helmInstallTimeout,
			ValuesFile:  tcfg.valuesFile,
			Wait:        true,
		})
		framework.ExpectNoError(err, "failed to install chart %s", tcfg.helmChart)
	})

	ginkgo.AfterEach(func(ctx context.Context) {
		if install.clients.kube != nil {
			saveLogsOnFailure(ctx, install.clients, tcfg.namespace, gpuWorkloadNamespace)
		}
	})

	return install
}

// uninstall removes the release and deletes the namespace without waiting for it to be gone;
// the next install waits for that instead. The namespace is deleted only when it carries this
// run's label, so a namespace the run did not create is left alone.
func (install *operatorInstall) uninstall(ctx context.Context) {
	ginkgo.GinkgoHelper()

	uninstallErr := install.operator.Uninstall(ctx, install.releaseName)

	namespaces := install.clients.kube.CoreV1().Namespaces()
	namespace, err := namespaces.Get(ctx, tcfg.namespace, metav1.GetOptions{})
	framework.ExpectNoError(client.IgnoreNotFound(err), "failed to get namespace %s", tcfg.namespace)
	if err == nil && namespace.Labels[runLabel] == string(framework.RunID) {
		err = namespaces.Delete(ctx, tcfg.namespace, metav1.DeleteOptions{
			Preconditions: &metav1.Preconditions{UID: &namespace.UID},
		})
		framework.ExpectNoError(client.IgnoreNotFound(err), "failed to delete namespace %s", tcfg.namespace)
	}
	framework.ExpectNoError(uninstallErr, "failed to uninstall release %s", install.releaseName)
}

// saveLogsOnFailure writes the logs of every pod in the given namespaces to a directory under
// -log-dir named after the failed spec's top-level container, which CI uploads when the run fails.
func saveLogsOnFailure(ctx context.Context, clients clusterClients, namespaces ...string) {
	if !ginkgo.CurrentSpecReport().Failed() || tcfg.logDir == "" {
		return
	}
	specLogDir := filepath.Join(tcfg.logDir, ginkgo.CurrentSpecReport().ContainerHierarchyTexts[0])
	if err := os.MkdirAll(specLogDir, 0o750); err != nil {
		e2elog.Logf("WARN: failed to create log dir %s: %v", specLogDir, err)
		return
	}

	for _, namespace := range namespaces {
		pods, err := clients.pods.List(ctx, namespace, "")
		if err != nil {
			e2elog.Logf("WARN: failed to list pods in %s: %v", namespace, err)
			continue
		}

		var podSummary strings.Builder
		for _, pod := range pods {
			fmt.Fprintf(&podSummary, "%s\t%s\tready=%t\n", pod.Name, pod.Status.Phase, helpers.IsPodReady(&pod))
			for _, container := range slices.Concat(pod.Spec.InitContainers, pod.Spec.Containers) {
				containerLogs, err := clients.pods.Logs(ctx, &pod, container.Name, false)
				if err != nil {
					containerLogs = fmt.Sprintf("failed to read logs: %v", err)
				}
				logPath := filepath.Join(specLogDir, fmt.Sprintf("%s_%s_%s.log", namespace, pod.Name, container.Name))
				if err := os.WriteFile(logPath, []byte(containerLogs), 0o600); err != nil {
					e2elog.Logf("WARN: failed to write %s: %v", logPath, err)
				}
			}
		}

		summaryPath := filepath.Join(specLogDir, fmt.Sprintf("%s_pods.txt", namespace))
		if err := os.WriteFile(summaryPath, []byte(podSummary.String()), 0o600); err != nil {
			e2elog.Logf("WARN: failed to write %s: %v", summaryPath, err)
		}
	}
}
