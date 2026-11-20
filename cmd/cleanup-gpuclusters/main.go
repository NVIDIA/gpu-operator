/*
Copyright (c), NVIDIA CORPORATION.  All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v3"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/client/config"

	gpuv1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1"
	nvidiav1alpha1 "github.com/NVIDIA/gpu-operator/api/nvidia/v1alpha1"
	"github.com/NVIDIA/gpu-operator/internal/info"
)

var logger = log.New()

func main() {
	var debug bool
	var gpuClusterName, clusterPolicyName, releaseName, releaseNamespace string
	var all bool

	c := cli.Command{}
	c.Name = "cleanup-gpuclusters"
	c.Usage = "Delete stack CRs and wait until they are gone"
	c.Version = info.GetVersionString()
	c.Flags = []cli.Flag{
		&cli.BoolFlag{
			Name:        "debug",
			Aliases:     []string{"d"},
			Usage:       "Enable debug-level logging",
			Destination: &debug,
			Sources:     cli.EnvVars("DEBUG"),
		},
		&cli.StringFlag{
			Name:        "gpucluster-name",
			Usage:       "Name of the chart-managed GPUCluster CR to delete",
			Destination: &gpuClusterName,
			Sources:     cli.EnvVars("GPUCLUSTER_NAME"),
		},
		&cli.StringFlag{Name: "clusterpolicy-name", Usage: "Name of the chart-managed ClusterPolicy CR to delete", Destination: &clusterPolicyName},
		&cli.StringFlag{Name: "release-name", Usage: "Delete only CRs owned by this Helm release", Destination: &releaseName},
		&cli.StringFlag{Name: "release-namespace", Usage: "Namespace of the Helm release", Destination: &releaseNamespace},
		&cli.BoolFlag{Name: "all", Usage: "Delete all stack CRs before removing their CRDs", Destination: &all},
	}
	c.Before = func(ctx context.Context, cli *cli.Command) (context.Context, error) {
		logLevel := log.InfoLevel
		if debug {
			logLevel = log.DebugLevel
		}
		logger.SetLevel(logLevel)
		return ctx, nil
	}
	c.Action = func(ctx context.Context, _ *cli.Command) error {
		if !all && clusterPolicyName == "" && gpuClusterName == "" {
			return fmt.Errorf("specify a stack CR name or --all")
		}
		if (releaseName == "") != (releaseNamespace == "") {
			return fmt.Errorf("--release-name and --release-namespace must be specified together")
		}
		return runDeleteStacks(ctx, clusterPolicyName, gpuClusterName, releaseName, releaseNamespace, all)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	err := c.Run(ctx, os.Args)
	if err != nil {
		log.Errorf("%v", err)
		log.Exit(1)
	}
}

// runDeleteStacks keeps the operator alive until the stack CR finalizers complete.
func runDeleteStacks(ctx context.Context, clusterPolicyName, gpuClusterName, releaseName, releaseNamespace string, all bool) error {
	scheme := runtime.NewScheme()
	if err := gpuv1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("failed to add ClusterPolicy types to scheme: %w", err)
	}
	if err := nvidiav1alpha1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("failed to add GPUCluster types to scheme: %w", err)
	}
	restConfig, err := ctrlconfig.GetConfig()
	if err != nil {
		return fmt.Errorf("failed to get kubeconfig: %w", err)
	}
	k8sClient, err := ctrlclient.New(restConfig, ctrlclient.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	// Helm controls the wait for the pre-delete Job through --timeout.
	return deleteStacks(ctx, k8sClient, clusterPolicyName, gpuClusterName, releaseName, releaseNamespace, all)
}

func deleteStacks(ctx context.Context, c ctrlclient.Client, clusterPolicyName, gpuClusterName, releaseName, releaseNamespace string, all bool) error {
	var targets []ctrlclient.Object
	if all {
		// CRD deletion would delete every instance anyway. Run their finalizers
		// while the operator and its RBAC are still available.
		for _, list := range []ctrlclient.ObjectList{&nvidiav1alpha1.GPUClusterList{}, &gpuv1.ClusterPolicyList{}} {
			if err := c.List(ctx, list); meta.IsNoMatchError(err) {
				continue
			} else if err != nil {
				return fmt.Errorf("list %T for cleanup: %w", list, err)
			}
			if err := meta.EachListItem(list, func(obj runtime.Object) error {
				targets = append(targets, obj.(ctrlclient.Object))
				return nil
			}); err != nil {
				return err
			}
		}
	} else {
		for _, target := range []ctrlclient.Object{
			&nvidiav1alpha1.GPUCluster{ObjectMeta: metav1.ObjectMeta{Name: gpuClusterName}},
			&gpuv1.ClusterPolicy{ObjectMeta: metav1.ObjectMeta{Name: clusterPolicyName}},
		} {
			if target.GetName() == "" {
				continue
			}
			err := c.Get(ctx, ctrlclient.ObjectKeyFromObject(target), target)
			if meta.IsNoMatchError(err) || apierrors.IsNotFound(err) {
				continue
			}
			if err != nil {
				return fmt.Errorf("get %T %s: %w", target, target.GetName(), err)
			}
			annotations := target.GetAnnotations()
			if releaseName != "" && (annotations["meta.helm.sh/release-name"] != releaseName ||
				annotations["meta.helm.sh/release-namespace"] != releaseNamespace) {
				continue
			}
			targets = append(targets, target)
		}
	}
	// Request deletion of both stacks before waiting, so neither source can
	// bring up replacement operands after the other passes its finalizer.
	for _, target := range targets {
		if !target.GetDeletionTimestamp().IsZero() {
			continue
		}
		uid := target.GetUID()
		logger.Infof("Deleting %T %s", target, target.GetName())
		err := c.Delete(ctx, target, ctrlclient.PropagationPolicy(metav1.DeletePropagationBackground), ctrlclient.Preconditions{UID: &uid})
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete %T %s: %w", target, target.GetName(), err)
		}
	}
	for _, target := range targets {
		uid := target.GetUID()
		key := ctrlclient.ObjectKeyFromObject(target)
		logger.Infof("Waiting for %T %s to be deleted", target, key.Name)
		err := wait.PollUntilContextCancel(ctx, 5*time.Second, true, func(ctx context.Context) (bool, error) {
			err := c.Get(ctx, key, target)
			if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
				return true, nil
			}
			if err != nil {
				logger.Warnf("Failed to get %T %s while waiting for deletion, retrying: %v", target, key.Name, err)
				return false, nil
			}
			return target.GetUID() != uid, nil
		})
		if err != nil {
			return fmt.Errorf("wait for %T %s deletion: %w", target, key.Name, err)
		}
	}
	return nil
}
