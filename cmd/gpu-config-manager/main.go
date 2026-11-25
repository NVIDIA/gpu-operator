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

// gpu-config-manager writes GPU settings from configuration documents at
// each driver load, while no workload can use the GPUs.
package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/sirupsen/logrus"

	"github.com/NVIDIA/gpu-operator/internal/driver"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/applier"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/apply"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/apply/nvmllib"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/config"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/decision"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/kube"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/loadid"
	"github.com/NVIDIA/gpu-operator/internal/gpuconfig/statefile"
)

const (
	envNodeName        = "NODE_NAME"
	envNamespace       = "OPERATOR_NAMESPACE"
	envStack           = "GPU_CONFIG_STACK"
	envClusterDocument = "GPU_CONFIG_CLUSTER_DOCUMENT"
	envPoolLabelKey    = "POOL_LABEL_KEY"

	defaultClusterDocument = "gpu-config"
	defaultPoolLabelKey    = "nvidia.com/gpu-config.pool"
)

const (
	// apiTimeout bounds each API request, so that none can stall a decision
	// while it holds the state file lock.
	apiTimeout = 30 * time.Second
	// documentSyncTimeout bounds the first list of the documents. Exiting
	// then lets the kubelet restart the container with the reason visible.
	documentSyncTimeout = 2 * time.Minute
)

type settings struct {
	nodeName, namespace, clusterDocument, poolLabelKey string
	stack                                              decision.Stack
}

func main() {
	log := logrus.New()
	log.SetOutput(os.Stderr)
	if err := run(context.Background(), log); err != nil {
		log.WithError(err).Error("gpu-config-manager failed")
		os.Exit(1)
	}
}

func readSettings(getenv func(string) string) (settings, error) {
	s := settings{
		nodeName:        getenv(envNodeName),
		namespace:       getenv(envNamespace),
		stack:           decision.Stack(getenv(envStack)),
		clusterDocument: getenv(envClusterDocument),
		poolLabelKey:    getenv(envPoolLabelKey),
	}
	var errs []error
	if s.nodeName == "" {
		errs = append(errs, fmt.Errorf("%s is required", envNodeName))
	}
	if s.namespace == "" {
		errs = append(errs, fmt.Errorf("%s is required", envNamespace))
	}
	if s.stack != decision.ContainerStack && s.stack != decision.DRAStack {
		errs = append(errs, fmt.Errorf("%s must be %q or %q, got %q", envStack, decision.ContainerStack, decision.DRAStack, s.stack))
	}
	if s.clusterDocument == "" {
		s.clusterDocument = defaultClusterDocument
	}
	if s.poolLabelKey == "" {
		s.poolLabelKey = defaultPoolLabelKey
	}
	return s, errors.Join(errs...)
}

// documentNames returns the node, pool and cluster document names, with ""
// for a document the node can't have.
func documentNames(s settings, node *corev1.Node, log logrus.FieldLogger) []string {
	nodeDoc, ok := config.NodeDocumentName(s.nodeName)
	if !ok {
		log.WithField("node", s.nodeName).Warn("the node name is too long for a node document")
		nodeDoc = ""
	}
	poolDoc := ""
	if v := node.Labels[s.poolLabelKey]; v != "" {
		if poolDoc, ok = config.PoolDocumentName(v); !ok {
			log.WithFields(logrus.Fields{"label": s.poolLabelKey, "value": v}).Warn("the pool label value can't name a ConfigMap; the node has no pool document")
		}
	}
	return []string{nodeDoc, poolDoc, s.clusterDocument}
}

func servedDRAVersion(ctx context.Context, mapper meta.RESTMapper, sleep func(context.Context, time.Duration) error, log logrus.FieldLogger) (string, error) {
	var version string
	err := applier.RetryAPI(ctx, sleep, log, "discovering resource.k8s.io", func() error {
		var err error
		version, err = kube.ServedDRAVersion(mapper)
		return err
	})
	return version, err
}

func run(ctx context.Context, log logrus.FieldLogger) error {
	s, err := readSettings(os.Getenv)
	if err != nil {
		return err
	}
	restConfig, err := ctrlconfig.GetConfig()
	if err != nil {
		return fmt.Errorf("loading the Kubernetes client configuration: %w", err)
	}
	c, watchClient, err := kube.NewClients(restConfig, apiTimeout, client.Options{Scheme: kube.NewScheme()})
	if err != nil {
		return fmt.Errorf("creating the Kubernetes client: %w", err)
	}
	draVersion, err := servedDRAVersion(ctx, c.RESTMapper(), sleep, log)
	if err != nil {
		return err
	}
	log.WithField("version", draVersion).Info("resource.k8s.io version for claims and slices")
	kc := &kube.Client{C: c, DRAVersion: draVersion}

	// The pool label is read once per container: every decision at a new
	// driver load runs in a container started for it.
	var node *corev1.Node
	if err := applier.RetryAPI(ctx, sleep, log, "reading node "+s.nodeName, func() error {
		node, err = kc.GetNode(ctx, s.nodeName)
		return err
	}); err != nil {
		return err
	}

	a := &applier.Applier{
		Config: applier.Config{
			NodeName:       s.nodeName,
			Namespace:      s.namespace,
			Stack:          s.stack,
			ValidationsDir: statefile.DefaultDir,
			PollInterval:   applier.DefaultPollInterval,
			PublishJitter:  applier.DefaultPublishJitter,
		},
		Store:   statefile.New(statefile.DefaultDir),
		Cluster: kc,
		LoadID:  func() (uint64, bool, error) { return loadid.Read(loadid.ModulePath) },
		OpenNVML: func(driverRoot string) (apply.Library, error) {
			path, err := driver.Root(driverRoot).GetDriverLibraryPath()
			if err != nil {
				return nil, err
			}
			return nvmllib.New(path), nil
		},
		Sleep:  sleep,
		Jitter: jitter,
		Log:    log,
	}

	names := documentNames(s, node, log)
	docs, err := kube.NewDocuments(names, kube.ConfigMapListWatch(c, watchClient, s.namespace), a.DocumentChanged)
	if err != nil {
		return err
	}
	a.Documents = docs
	go docs.Run(ctx)
	log.WithFields(logrus.Fields{"namespace": s.namespace, "documents": names, "timeout": documentSyncTimeout}).Info("waiting for the document watches to sync")
	if err := docs.WaitForSync(ctx, documentSyncTimeout); err != nil {
		return fmt.Errorf("the document watches didn't sync: %w", err)
	}
	log.WithFields(logrus.Fields{"node": s.nodeName, "stack": s.stack, "documents": names}).Info("started")
	return a.Run(ctx)
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func jitter(maxDelay time.Duration) time.Duration {
	if maxDelay <= 0 {
		return 0
	}
	return rand.N(maxDelay)
}
