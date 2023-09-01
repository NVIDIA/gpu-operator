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
	"strings"

	configv1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

// Interface to the clusterinfo package
type Interface interface {
	GetKubernetesVersion() (string, error)
	GetOpenshiftVersion() (string, error)
}

type clusterInfo struct {
	config            *rest.Config
	kubernetesVersion string
	openshiftVersion  string
}

// New creates a new instance of clusterinfo API
func New(opts ...Option) (Interface, error) {
	l := &clusterInfo{}
	for _, opt := range opts {
		opt(l)
	}
	if l.config == nil {
		l.config = config.GetConfigOrDie()
	}

	kubernetesVersion, err := getKubernetesVersion(l.config)
	if err != nil {
		return nil, fmt.Errorf("failed to get kubernetes version: %v", err)
	}
	l.kubernetesVersion = kubernetesVersion

	openshiftVersion, err := getOpenshiftVersion(l.config)
	if err != nil {
		return nil, fmt.Errorf("failed to get openshift version: %v", err)
	}
	l.openshiftVersion = openshiftVersion

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

// GetKubernetesVersion returns the k8s version detected in the cluster
func (l *clusterInfo) GetKubernetesVersion() (string, error) {
	return l.kubernetesVersion, nil
}

// GetOpenShiftVersion returns the OpenShift version detected in the cluster.
// An empty string, "", is returned if it is determined we are not running on OpenShift.
func (l *clusterInfo) GetOpenshiftVersion() (string, error) {
	return l.openshiftVersion, nil
}

func getKubernetesVersion(config *rest.Config) (string, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return "", fmt.Errorf("error building discovery client: %v", err)
	}

	info, err := discoveryClient.ServerVersion()
	if err != nil {
		return "", fmt.Errorf("unable to fetch server version information: %v", err)
	}

	return info.GitVersion, nil
}

func getOpenshiftVersion(config *rest.Config) (string, error) {
	client, err := configv1.NewForConfig(config)
	if err != nil {
		return "", err
	}

	v, err := client.ClusterVersions().Get(context.TODO(), "version", metav1.GetOptions{})
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
