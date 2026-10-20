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
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/onsi/ginkgo/v2"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/NVIDIA/gpu-operator/tests/e2e/framework"
	"github.com/NVIDIA/gpu-operator/tests/e2e/helpers"
)

const helmTimeout = 5 * time.Minute

const uninstallTimeout = 2 * helmTimeout

const runLabel = "e2e-run"

func operatorChartValues() []string {
	return []string{
		fmt.Sprintf("operator.repository=%s", TestContext.OperatorRepository),
		fmt.Sprintf("operator.image=%s", TestContext.OperatorImage),
		fmt.Sprintf("operator.version=%s", TestContext.OperatorVersion),
		fmt.Sprintf("validator.repository=%s", TestContext.ValidatorRepository),
		fmt.Sprintf("validator.image=%s", TestContext.ValidatorImage),
		fmt.Sprintf("validator.version=%s", TestContext.ValidatorVersion),
	}
}

type Install struct {
	Clients     Clients
	Operator    *helpers.OperatorClient
	releaseName string

	failureLogsSaved bool
}

func NewInstall(chartValues ...string) *Install {
	install := &Install{}

	ginkgo.BeforeAll(func(ctx context.Context) {
		install.Clients = NewClients()
		install.Operator = &helpers.OperatorClient{
			Chart:        TestContext.HelmChart,
			ChartVersion: TestContext.HelmChartVersion,
			Namespace:    TestContext.OperatorNamespace,
			Kubeconfig:   framework.TestContext.KubeConfig,
		}
		err := framework.WaitForNamespacesDeleted(ctx, install.Clients.Kube, []string{TestContext.OperatorNamespace}, framework.DefaultNamespaceDeletionTimeout)
		framework.ExpectNoError(err, "namespace %s was not deleted", TestContext.OperatorNamespace)

		install.releaseName = fmt.Sprintf("gpu-operator-%s", framework.RandomSuffix())
		ginkgo.DeferCleanup(install.uninstall, ginkgo.NodeTimeout(uninstallTimeout))

		_, err = framework.CreateNamespace(ctx, TestContext.OperatorNamespace, install.Clients.Kube, nil)
		framework.ExpectNoError(err, "failed to create namespace %s", TestContext.OperatorNamespace)
		_, err = install.Operator.Install(ctx, append(operatorChartValues(), chartValues...), helpers.ChartOptions{
			ReleaseName: install.releaseName,
			Timeout:     helmTimeout,
			ValuesFile:  TestContext.ValuesFile,
			Wait:        true,
		})
		framework.ExpectNoError(err, "failed to install chart %s", TestContext.HelmChart)
	})

	ginkgo.AfterEach(func(ctx context.Context) {
		if install.Clients.Kube != nil {
			install.failureLogsSaved = saveLogsOnFailure(ctx, install.Clients, TestContext.OperatorNamespace)
		}
	})

	return install
}

func (install *Install) uninstall(ctx context.Context) {
	ginkgo.GinkgoHelper()

	if !install.failureLogsSaved {
		saveLogsOnFailure(ctx, install.Clients, TestContext.OperatorNamespace)
	}

	var cleanupErrs []error
	if err := install.Operator.Uninstall(ctx, install.releaseName); err != nil {
		cleanupErrs = append(cleanupErrs, fmt.Errorf("failed to uninstall release %s: %w", install.releaseName, err))
		if TestContext.LogDir != "" {
			saveLogs(ctx, install.Clients, filepath.Join(containerLogDir(), "uninstall"), TestContext.OperatorNamespace)
		}
	}

	namespaceClient := install.Clients.Kube.CoreV1().Namespaces()
	namespace, err := namespaceClient.Get(ctx, TestContext.OperatorNamespace, metav1.GetOptions{})
	if client.IgnoreNotFound(err) != nil {
		cleanupErrs = append(cleanupErrs, fmt.Errorf("failed to get namespace %s: %w", TestContext.OperatorNamespace, err))
	}
	if err == nil && namespace.Labels[runLabel] == string(framework.RunID) {
		err = namespaceClient.Delete(ctx, TestContext.OperatorNamespace, metav1.DeleteOptions{
			Preconditions: &metav1.Preconditions{UID: &namespace.UID},
		})
		if client.IgnoreNotFound(err) != nil {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("failed to delete namespace %s: %w", TestContext.OperatorNamespace, err))
		}
	}
	framework.ExpectNoError(errors.Join(cleanupErrs...))
}
