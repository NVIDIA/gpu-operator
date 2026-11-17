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

package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/decision"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/gate"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/kube"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/loadid"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/statefile"
)

const (
	// gpuConfigComponent is the validator component that holds the node's
	// allocators until the GPU configuration manager has decided for the
	// current driver load.
	gpuConfigComponent = "gpu-config"

	envGPUConfigStack           = "GPU_CONFIG_STACK"
	envGPUConfigNoModuleTimeout = "GPU_CONFIG_NO_MODULE_TIMEOUT"
)

// GPUConfig is the gate in front of the node's allocators.
type GPUConfig struct {
	ctx    context.Context
	getenv func(string) string
}

func (g *GPUConfig) gateConfig() (gate.Config, error) {
	stack := decision.Stack(g.getenv(envGPUConfigStack))
	if stack != decision.ContainerStack && stack != decision.DRAStack {
		return gate.Config{}, fmt.Errorf("%s must be %q or %q, got %q", envGPUConfigStack, decision.ContainerStack, decision.DRAStack, stack)
	}
	bound := gate.DefaultNoModuleBound
	if v := g.getenv(envGPUConfigNoModuleTimeout); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return gate.Config{}, fmt.Errorf("%s must be a positive duration, got %q", envGPUConfigNoModuleTimeout, v)
		}
		bound = d
	}
	return gate.Config{
		NodeName:         nodeNameFlag,
		Namespace:        namespaceFlag,
		ApplierDaemonSet: decision.ApplierDaemonSet(stack),
		Interval:         gate.DefaultInterval,
		NoModuleBound:    bound,
	}, nil
}

func (g *GPUConfig) validate() error {
	cfg, err := g.gateConfig()
	if err != nil {
		return err
	}
	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		return fmt.Errorf("error getting cluster config: %w", err)
	}
	c, err := client.New(kubeConfig, client.Options{Scheme: kube.NewScheme()})
	if err != nil {
		return fmt.Errorf("error getting k8s client: %w", err)
	}

	log.Infof("waiting for the GPU configuration manager %s if it is enabled on this node", cfg.ApplierDaemonSet)
	gt := &gate.Gate{
		Config:  cfg,
		Store:   statefile.New(outputDirFlag),
		Cluster: &kube.Client{C: c},
		LoadID:  func() (uint64, bool, error) { return loadid.Read(loadid.ModulePath) },
		Now:     time.Now,
		Sleep:   sleepContext,
		Jitter: func(maxDelay time.Duration) time.Duration {
			if maxDelay <= 0 {
				return 0
			}
			return rand.N(maxDelay)
		},
		Infof: log.Infof,
		Warnf: log.Warnf,
	}
	reason, err := gt.Run(g.ctx)
	if err != nil {
		return err
	}
	log.Infof("passed: %s; allocators may start", reason)
	return nil
}

func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
