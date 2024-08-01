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

package clusterinfo

import (
	"context"
	"fmt"
	"maps"
	"strings"

	configv1 "github.com/openshift/api/config/v1"
	ocpconfigv1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	imagesv1 "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/discovery"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/NVIDIA/gpu-operator/internal/consts"
)

// Interface to the clusterinfo package
type Interface interface {
	GetContainerRuntime() (string, error)
	GetKubernetesVersion() (string, error)
	GetOpenshiftVersion() (string, error)
	GetRHCOSVersions(map[string]string) ([]string, error)
	GetOpenshiftDriverToolkitImages() map[string]string
	GetOpenshiftProxySpec() (*configv1.ProxySpec, error)
	GetKernelVersions(map[string]string) ([]string, error)
}

const (
	nfdOSTreeVersionLabelKey = "feature.node.kubernetes.io/system-os_release.OSTREE_VERSION"
	nfdKernelLabelKey        = "feature.node.kubernetes.io/kernel-version.full"
)

type clusterInfo struct {
	ctx     context.Context
	config  *rest.Config
	oneshot bool

	containerRuntime             string
	kubernetesVersion            string
	openshiftVersion             string
	rhcosVersions                []string
	openshiftDriverToolkitImages map[string]string
	kernelVersions               []string
	proxySpec                    *configv1.ProxySpec
}

// New creates a new instance of clusterinfo API
func New(ctx context.Context, opts ...Option) (Interface, error) {
	l := &clusterInfo{
		ctx: ctx,
	}
	for _, opt := range opts {
		opt(l)
	}
	if l.config == nil {
		l.config = config.GetConfigOrDie()
	}

	if !l.oneshot {
		return l, nil
	}

	// The 'oneshot' option is configured. Get cluster information now and store
	// it in the struct. This information will be used when clients request
	// cluster information.
	containerRuntime, err := getContainerRuntime(ctx, l.config)
	if err != nil {
		return nil, fmt.Errorf("failed to get container runtime: %w", err)
	}
	l.containerRuntime = containerRuntime

	kubernetesVersion, err := getKubernetesVersion(l.config)
	if err != nil {
		return nil, fmt.Errorf("failed to get kubernetes version: %w", err)
	}
	l.kubernetesVersion = kubernetesVersion

	openshiftVersion, err := getOpenshiftVersion(ctx, l.config)
	if err != nil {
		return nil, fmt.Errorf("failed to get openshift version: %w", err)
	}
	l.openshiftVersion = openshiftVersion

	l.rhcosVersions, err = getRHCOSVersions(ctx, l.config, map[string]string{
		consts.GPUPresentLabel: "true",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list rhcos versions: %w", err)
	}

	l.openshiftDriverToolkitImages = getOpenshiftDTKImages(ctx, l.config)

	l.kernelVersions, err = getKernelVersions(ctx, l.config, map[string]string{
		consts.GPUPresentLabel: "true",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get kernel versions: %w", err)
	}

	return l, nil
}

// Option is a function that configures the clusterInfo library
type Option func(*clusterInfo)

// WithKubernetesConfig provides an option to set the k8s config used by the library
func WithKubernetesConfig(config *rest.Config) Option {
	return func(l *clusterInfo) {
		l.config = config
	}
}

// WithOneShot provides an option to get the cluster information once during initialization
// of the clusterInfo library. If false, cluster information is fetched every time a client
// requests information via the interface.
func WithOneShot(oneshot bool) Option {
	return func(l *clusterInfo) {
		l.oneshot = oneshot
	}
}

func (l *clusterInfo) GetContainerRuntime() (string, error) {
	if l.oneshot {
		return l.containerRuntime, nil
	}

	return getContainerRuntime(l.ctx, l.config)
}

// GetKubernetesVersion returns the k8s version detected in the cluster
func (l *clusterInfo) GetKubernetesVersion() (string, error) {
	if l.oneshot {
		return l.kubernetesVersion, nil
	}

	return getKubernetesVersion(l.config)
}

// GetOpenshiftVersion returns the OpenShift version detected in the cluster.
// An empty string, "", is returned if it is determined we are not running on OpenShift.
func (l *clusterInfo) GetOpenshiftVersion() (string, error) {
	if l.oneshot {
		return l.openshiftVersion, nil
	}

	return getOpenshiftVersion(l.ctx, l.config)
}

// GetRHCOSVersions returns the list of RedHat CoreOS versions used in the Openshift Cluster
func (l *clusterInfo) GetRHCOSVersions(labelSelector map[string]string) ([]string, error) {
	if l.oneshot {
		return l.rhcosVersions, nil
	}

	return getRHCOSVersions(l.ctx, l.config, labelSelector)
}

func getRHCOSVersions(ctx context.Context, config *rest.Config, selector map[string]string) ([]string, error) {
	logger := log.FromContext(ctx)
	var rhcosVersions []string

	nodeSelector := map[string]string{
		consts.GPUPresentLabel: "true",
	}

	// merge defaultSelector with user-input selector
	maps.Copy(nodeSelector, selector)

	k8sClient, err := corev1client.NewForConfig(config)
	if err != nil {
		logger.Error(err, "failed to build k8s core v1 client")
		return nil, err
	}

	nodeList, err := k8sClient.Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(nodeSelector).String(),
	})

	if err != nil {
		logger.Error(err, "failed to list Nodes")
		return nil, err
	}

	for _, node := range nodeList.Items {
		node := node

		nodeLabels := node.GetLabels()
		if rhcosVersion, ok := nodeLabels[nfdOSTreeVersionLabelKey]; ok {
			rhcosVersions = append(rhcosVersions, rhcosVersion)
		}
	}

	return rhcosVersions, nil
}

// GetOpenshiftDriverToolkitImages returns a map of the Openshift DriverToolkit Images available for use in the
// openshift cluster
func (l *clusterInfo) GetOpenshiftDriverToolkitImages() map[string]string {
	if l.oneshot {
		return l.openshiftDriverToolkitImages
	}

	return getOpenshiftDTKImages(l.ctx, l.config)
}

func (l *clusterInfo) GetKernelVersions(labelSelector map[string]string) ([]string, error) {
	if l.oneshot {
		return l.kernelVersions, nil
	}

	return getKernelVersions(l.ctx, l.config, labelSelector)
}

func (l *clusterInfo) GetOpenshiftProxySpec() (*configv1.ProxySpec, error) {
	if l.oneshot {
		return l.proxySpec, nil
	}

	return getOpenshiftProxySpec(l.ctx, l.config)
}

func getContainerRuntime(ctx context.Context, config *rest.Config) (string, error) {
	logger := log.FromContext(ctx)

	k8sClient, err := corev1client.NewForConfig(config)
	if err != nil {
		logger.Error(err, "failed to build k8s core v1 client")
		return "", err
	}

	ocpVersion, err := getOpenshiftVersion(ctx, config)
	if err != nil {
		logger.Error(err, "failed to retrieve")
	}
	if ocpVersion != "" {
		return consts.CRIO, nil
	}

	nodeSelector := map[string]string{
		consts.GPUPresentLabel: "true",
	}

	list, err := k8sClient.Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(nodeSelector).String(),
	})
	if err != nil {
		return "", fmt.Errorf("unable to list nodes prior to checking container runtime: %w", err)
	}

	var runtime string
	for _, node := range list.Items {
		rt, err := getRuntimeString(node)
		if err != nil {
			logger.Info(fmt.Sprintf("Unable to get runtime info for node %s: %v", node.Name, err))
			continue
		}
		runtime = rt
		if runtime == consts.Containerd {
			// default to containerd if >=1 node running containerd
			break
		}
	}

	if runtime == "" {
		logger.Info("Unable to get runtime info from the cluster, defaulting to containerd")
		runtime = consts.Containerd
	}
	return runtime, nil
}

func getKubernetesVersion(config *rest.Config) (string, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return "", fmt.Errorf("error building discovery client: %w", err)
	}

	info, err := discoveryClient.ServerVersion()
	if err != nil {
		return "", fmt.Errorf("unable to fetch server version information: %v", err)
	}

	return info.GitVersion, nil
}

func getOpenshiftVersion(ctx context.Context, config *rest.Config) (string, error) {
	client, err := ocpconfigv1.NewForConfig(config)
	if err != nil {
		return "", err
	}

	v, err := client.ClusterVersions().Get(ctx, "version", metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			// not an OpenShift cluster
			return "", nil
		}
		return "", err
	}

	for _, condition := range v.Status.History {
		if condition.State != "Completed" {
			continue
		}

		ocpV := strings.Split(condition.Version, ".")
		if len(ocpV) > 1 {
			return ocpV[0] + "." + ocpV[1], nil
		}
		return ocpV[0], nil
	}

	return "", fmt.Errorf("failed to find Completed Cluster Version")
}

func getOpenshiftDTKImages(ctx context.Context, c *rest.Config) map[string]string {
	var rhcosDriverToolkitImages map[string]string
	logger := log.FromContext(ctx)

	name := "driver-toolkit"
	namespace := consts.OpenshiftNamespace

	ocpImageClient, err := imagesv1.NewForConfig(c)
	if err != nil {
		logger.Error(err, "failed to build openshift image stream client")
		return nil
	}

	imgStream, err := ocpImageClient.ImageStreams(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("ocpHasDriverToolkitImageStream: driver-toolkit imagestream not found",
				"Name", name,
				"Namespace", namespace)
		}
		logger.Error(err, "Couldn't get the driver-toolkit imagestream")
		return nil
	}

	for _, tag := range imgStream.Spec.Tags {
		if tag.Name == "" {
			logger.Info("WARNING: ocpHasDriverToolkitImageStream: driver-toolkit imagestream is broken, see RHBZ#2015024")
			continue
		}
		if tag.Name == "latest" || tag.From == nil {
			continue
		}

		if rhcosDriverToolkitImages == nil {
			rhcosDriverToolkitImages = map[string]string{}
		}

		logger.Info("ocpHasDriverToolkitImageStream: tag", tag.Name, tag.From.Name)
		rhcosDriverToolkitImages[tag.Name] = tag.From.Name
	}

	// TODO: Add code to update operator metrics
	// TODO: Add code to ensure OCP Namespace Monitoring setting

	return rhcosDriverToolkitImages
}

func getKernelVersions(ctx context.Context, config *rest.Config, selector map[string]string) ([]string, error) {
	logger := log.FromContext(ctx)
	var kernelVersions []string

	nodeSelector := map[string]string{
		consts.GPUPresentLabel: "true",
	}

	// merge defaultSelector with user-input selector
	maps.Copy(nodeSelector, selector)

	k8sClient, err := corev1client.NewForConfig(config)
	if err != nil {
		logger.Error(err, "failed to build k8s core v1 client")
		return nil, err
	}

	nodeList, err := k8sClient.Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(nodeSelector).String(),
	})
	if err != nil {
		logger.Error(err, "failed to list nodes")
		return nil, err
	}

	for _, node := range nodeList.Items {
		node := node

		nodeLabels := node.GetLabels()
		if kernelVersion, ok := nodeLabels[nfdKernelLabelKey]; ok {
			kernelVersions = append(kernelVersions, kernelVersion)
		}
	}

	return kernelVersions, nil
}

func getOpenshiftProxySpec(ctx context.Context, cfg *rest.Config) (*configv1.ProxySpec, error) {
	logger := log.FromContext(ctx)

	client, err := ocpconfigv1.NewForConfig(cfg)
	if err != nil {
		logger.Error(err, "error instantiating openshift config client")
	}

	proxy, err := client.Proxies().Get(ctx, "cluster", metav1.GetOptions{})
	if err != nil {
		logger.Error(err, "error retrieving proxies for openshift cluster")
		return nil, err
	}
	return &proxy.Spec, nil
}

func getRuntimeString(node corev1.Node) (string, error) {
	// ContainerRuntimeVersion string will look like <runtime>://<x.y.z>
	runtimeVer := node.Status.NodeInfo.ContainerRuntimeVersion
	var runtime string
	switch {
	case strings.HasPrefix(runtimeVer, "docker"):
		runtime = consts.Docker
	case strings.HasPrefix(runtimeVer, "containerd"):
		runtime = consts.Containerd
	case strings.HasPrefix(runtimeVer, "cri-o"):
		runtime = consts.CRIO
	default:
		return "", fmt.Errorf("runtime not recognized: %s", runtimeVer)
	}
	return runtime, nil
}
