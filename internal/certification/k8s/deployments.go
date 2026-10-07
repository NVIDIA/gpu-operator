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

package k8s

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

func (c *Clients) CreateDeployment(ctx context.Context, namespace string, deployment *appsv1.Deployment) error {
	_, err := c.k8s.AppsV1().Deployments(namespace).Create(ctx, deployment, metav1.CreateOptions{})
	return err
}

func (c *Clients) DeleteDeployment(ctx context.Context, namespace, name string) error {
	return c.k8s.AppsV1().Deployments(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

func (c *Clients) GetDeployment(ctx context.Context, namespace, name string) (*appsv1.Deployment, error) {
	return c.k8s.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
}

func (c *Clients) WaitForDeploymentAvailable(ctx context.Context, namespace, name string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(ctx, pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		deploy, err := c.k8s.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}

		if deploy.Spec.Replicas == nil {
			return false, nil
		}

		return deploy.Status.AvailableReplicas == *deploy.Spec.Replicas, nil
	})
}

func (c *Clients) GetDaemonSetContainerEnv(ctx context.Context, namespace, appLabel string) ([]corev1.EnvVar, error) {
	daemonsets, err := c.k8s.AppsV1().DaemonSets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s", appLabel),
	})
	if err != nil {
		return nil, err
	}

	if len(daemonsets.Items) == 0 {
		return nil, fmt.Errorf("no daemonset found with label app=%s", appLabel)
	}

	containers := daemonsets.Items[0].Spec.Template.Spec.Containers
	if len(containers) == 0 {
		return nil, fmt.Errorf("daemonset %s has no containers", daemonsets.Items[0].Name)
	}

	return containers[0].Env, nil
}

func (c *Clients) GetDaemonSetContainerImage(ctx context.Context, namespace, appLabel, containerName string) (string, error) {
	daemonsets, err := c.k8s.AppsV1().DaemonSets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s", appLabel),
	})
	if err != nil {
		return "", err
	}

	if len(daemonsets.Items) == 0 {
		return "", fmt.Errorf("no daemonset found with label app=%s", appLabel)
	}

	for _, container := range daemonsets.Items[0].Spec.Template.Spec.Containers {
		if container.Name == containerName {
			return container.Image, nil
		}
	}

	return "", fmt.Errorf("container %s not found in daemonset", containerName)
}

func GetEnvValue(envVars []corev1.EnvVar, name string) string {
	for _, env := range envVars {
		if env.Name == name {
			return env.Value
		}
	}
	return ""
}
