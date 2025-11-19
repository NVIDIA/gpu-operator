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

package helpers

import (
	"context"
	"fmt"
	"os"
	"time"

	helm "github.com/mittwald/go-helm-client"
	helmValues "github.com/mittwald/go-helm-client/values"
)

type OperatorClientOption func(client *OperatorClient)

type OperatorClient struct {
	helmClient helm.Client
	chart      string
	namespace  string
	kubeconfig string
}

func NewOperatorClient(opts ...OperatorClientOption) (*OperatorClient, error) {
	operatorClient := &OperatorClient{}

	for _, option := range opts {
		option(operatorClient)
	}

	helmOptions := &helm.KubeConfClientOptions{
		Options: &helm.Options{
			Namespace:        operatorClient.namespace,
			RepositoryCache:  os.TempDir() + "/.helmcache",
			RepositoryConfig: os.TempDir() + "/.helmrepo",
		},
	}

	kubeconfigBytes, err := os.ReadFile(operatorClient.kubeconfig)
	if err != nil {
		return nil, err
	}
	helmOptions.KubeConfig = kubeconfigBytes

	helmClient, err := helm.NewClientFromKubeConf(helmOptions)
	if err != nil {
		return nil, err
	}
	operatorClient.helmClient = helmClient

	return operatorClient, nil
}

func WithChart(chart string) OperatorClientOption {
	return func(operatorClient *OperatorClient) {
		operatorClient.chart = chart
	}
}

func WithKubeConfig(kubeconfig string) OperatorClientOption {
	return func(operatorClient *OperatorClient) {
		operatorClient.kubeconfig = kubeconfig
	}
}

func WithNamespace(namespace string) OperatorClientOption {
	return func(operatorClient *OperatorClient) {
		operatorClient.namespace = namespace
	}
}

type ChartOptions struct {
	CleanupOnFail bool
	GenerateName  bool
	ReleaseName   string
	Timeout       time.Duration
	Wait          bool
}

func (op *OperatorClient) Install(ctx context.Context, params []string, chartOpts ChartOptions) (string, error) {
	values := helmValues.Options{
		Values: params,
	}

	chartSpec := helm.ChartSpec{
		ChartName:     op.chart,
		Namespace:     op.namespace,
		GenerateName:  chartOpts.GenerateName,
		Wait:          chartOpts.Wait,
		Timeout:       chartOpts.Timeout,
		CleanupOnFail: chartOpts.CleanupOnFail,
		ValuesOptions: values,
	}

	if !chartOpts.GenerateName {
		if len(chartOpts.ReleaseName) == 0 {
			return "", fmt.Errorf("release name must be provided when the GenerateName chart option is unset")
		}
		chartSpec.ReleaseName = chartOpts.ReleaseName
	}

	release, err := op.helmClient.InstallChart(ctx, &chartSpec, nil)

	if err != nil {
		return "", fmt.Errorf("error installing operator: %w", err)
	}

	return release.Name, err
}

func (op *OperatorClient) Uninstall(releaseName string) error {
	return op.helmClient.UninstallReleaseByName(releaseName)
}

