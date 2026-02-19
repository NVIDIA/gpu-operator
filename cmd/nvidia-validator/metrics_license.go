/*
 * Copyright (c) 2024, NVIDIA CORPORATION.  All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/NVIDIA/gpu-operator/internal/licenseinfo"
	log "github.com/sirupsen/logrus"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func (nm *NodeMetrics) watchLicenseAnnotations() {
	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		log.Errorf("metrics: License: error getting cluster config - %v", err)
		return
	}

	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		log.Errorf("metrics: License: error getting k8s client - %v", err)
		return
	}

	log.Infof("metrics: License: starting publisher for annotation %s", licenseinfo.AnnotationKey)
	ticker := time.NewTicker(licenseStatusCheckDelaySeconds * time.Second)
	defer ticker.Stop()

	var lastPayload string
	for {
		select {
		case <-nm.ctx.Done():
			log.Info("metrics: License: context cancelled, stopping publisher")
			return
		default:
		}

		payload, buildErr := nm.buildLicenseAnnotation(kubeClient)
		if buildErr != nil {
			log.Warningf("metrics: License: %v", buildErr)
		}

		if payload == lastPayload {
			<-ticker.C
			continue
		}

		if err := patchLicenseAnnotation(nm.ctx, kubeClient, payload); err != nil {
			log.Errorf("metrics: License: failed to update node annotation: %v", err)
		} else {
			lastPayload = payload
		}

		<-ticker.C
	}
}

func (nm *NodeMetrics) buildLicenseAnnotation(kubeClient kubernetes.Interface) (string, error) {
	node, err := getNode(nm.ctx, kubeClient)
	if err != nil {
		return "", fmt.Errorf("unable to fetch node %s: %w", nodeNameFlag, err)
	}

	workloadConfig := node.GetLabels()[gpuWorkloadConfigLabelKey]
	if workloadConfig != gpuWorkloadConfigVMVgpu {
		return "", nil
	}

	now := time.Now()
	snapshot, snapErr := collectLicenseSnapshot(nm.ctx, now)
	payload, marshalErr := snapshot.Marshal()
	if marshalErr != nil {
		return "", marshalErr
	}

	if snapErr != nil {
		return payload, snapErr
	}
	return payload, nil
}

func patchLicenseAnnotation(ctx context.Context, kubeClient kubernetes.Interface, serializedSnapshot string) error {
	var annotations map[string]interface{}
	if serializedSnapshot == "" {
		annotations = map[string]interface{}{
			licenseinfo.AnnotationKey: nil,
		}
	} else {
		annotations = map[string]interface{}{
			licenseinfo.AnnotationKey: serializedSnapshot,
		}
	}

	patch := map[string]interface{}{
		"metadata": map[string]interface{}{
			"annotations": annotations,
		},
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("failed to marshal annotation patch: %w", err)
	}

	_, err = kubeClient.CoreV1().Nodes().Patch(ctx, nodeNameFlag, types.MergePatchType, patchBytes, meta_v1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("failed to patch node %s: %w", nodeNameFlag, err)
	}
	return nil
}
