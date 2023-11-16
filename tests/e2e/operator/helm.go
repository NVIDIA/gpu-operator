package operator

import (
	"context"
	"fmt"
	"os"
	"time"

	helm "github.com/mittwald/go-helm-client"
	helmValues "github.com/mittwald/go-helm-client/values"
)

// ClientOption is a function that can be used to set the fields of the operator helm Client
type ClientOption func(client *Client)

// Client represents the struct which holds the helm client
type Client struct {
	helmClient helm.Client
	chart      string
	namespace  string
	kubeconfig string
}

func NewClient(opts ...ClientOption) (*Client, error) {
	client := &Client{}

	for _, o := range opts {
		o(client)
	}

	opt := &helm.KubeConfClientOptions{
		Options: &helm.Options{
			Namespace:        client.namespace,
			RepositoryCache:  "/tmp/.helmcache",
			RepositoryConfig: "/tmp/.helmrepo",
		},
	}

	kubeconfigBytes, err := os.ReadFile(client.kubeconfig)
	if err != nil {
		return nil, err
	}
	opt.KubeConfig = kubeconfigBytes

	helmClient, err := helm.NewClientFromKubeConf(opt)
	if err != nil {
		return nil, err
	}
	client.helmClient = helmClient

	return client, nil
}

func WithChart(chart string) ClientOption {
	return func(c *Client) {
		c.chart = chart
	}
}

func WithKubeConfig(kubeconfig string) ClientOption {
	return func(c *Client) {
		c.kubeconfig = kubeconfig
	}
}

func WithNamespace(namespace string) ClientOption {
	return func(c *Client) {
		c.namespace = namespace
	}
}

type ChartOptions struct {
	CleanupOnFail bool
	GenerateName  bool
	ReleaseName   string
	Timeout       time.Duration
	Wait          bool
}

// Install deploys the helm chart
func (c *Client) Install(ctx context.Context, params []string, chartOpts ChartOptions) (string, error) {
	values := helmValues.Options{
		Values: params,
	}

	chartSpec := helm.ChartSpec{
		ChartName:     c.chart,
		Namespace:     c.namespace,
		GenerateName:  chartOpts.GenerateName,
		Wait:          chartOpts.Wait,
		Timeout:       chartOpts.Timeout,
		CleanupOnFail: chartOpts.CleanupOnFail,
		ValuesOptions: values,
	}

	if !chartOpts.GenerateName {
		if len(chartOpts.ReleaseName) == 0 {
			return "", fmt.Errorf("release name must be provided the GenerateName chart option is unset")
		}
		chartSpec.ReleaseName = chartOpts.ReleaseName
	}

	res, err := c.helmClient.InstallChart(ctx, &chartSpec, nil)

	if err != nil {
		return "", fmt.Errorf("error installing operator: %w", err)
	}

	return res.Name, err
}

func (c *Client) Uninstall(releaseName string) error {
	return c.helmClient.UninstallReleaseByName(releaseName)
}
