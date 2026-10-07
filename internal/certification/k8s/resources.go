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
	"io"
	"net/http"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

// CreateOrUpdateConfigMap creates or updates a ConfigMap (if already present).
func (c *Clients) CreateOrUpdateConfigMap(ctx context.Context, namespace, name string, data map[string]string) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: data,
	}
	_, err := c.k8s.CoreV1().ConfigMaps(namespace).Create(ctx, cm, metav1.CreateOptions{})
	if errors.IsAlreadyExists(err) {
		_, err = c.k8s.CoreV1().ConfigMaps(namespace).Update(ctx, cm, metav1.UpdateOptions{})
	}
	return err
}

func (c *Clients) DeleteConfigMap(ctx context.Context, namespace, name string) error {
	return c.k8s.CoreV1().ConfigMaps(namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

func (c *Clients) GetServiceInternalTrafficPolicy(ctx context.Context, namespace, serviceName string) (string, error) {
	svc, err := c.k8s.CoreV1().Services(namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	if svc.Spec.InternalTrafficPolicy == nil {
		return "", nil
	}

	return string(*svc.Spec.InternalTrafficPolicy), nil
}

// GetServiceDirect makes an HTTP GET request directly to a Service via cluster DNS.
// This only works when running in-cluster.
func (c *Clients) GetServiceDirect(ctx context.Context, namespace, serviceName string, port int, path string) ([]byte, error) {
	url := fmt.Sprintf("http://%s.%s.svc:%d%s", serviceName, namespace, port, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}

func (c *Clients) WaitForCondition(ctx context.Context, timeout time.Duration, condition func(ctx context.Context) (bool, error)) error {
	return wait.PollUntilContextTimeout(ctx, pollInterval, timeout, true, condition)
}
