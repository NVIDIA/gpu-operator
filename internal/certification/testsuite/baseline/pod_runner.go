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

package baseline

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/NVIDIA/gpu-operator/internal/certification"
	"github.com/NVIDIA/gpu-operator/internal/certification/k8s"
)

type podTestOpts struct {
	testName       string
	podName        string
	imageDesc      string
	successMessage string
	pod            *corev1.Pod
}

func runPodTest(ctx context.Context, clients *k8s.Clients, cfg certification.Config, opts podTestOpts) *certification.TestResult {
	result := certification.NewTestResult(opts.testName)
	defer result.Finalize()

	timeout := cfg.ParseTimeout()

	// Clean up any existing pod from previous runs
	_ = clients.DeletePod(ctx, cfg.Namespace, opts.podName)

	if err := clients.CreatePod(ctx, cfg.Namespace, opts.pod); err != nil {
		result.Fail("failed to create test pod: %v", err)
		return result
	}

	defer func() {
		_ = clients.DeletePod(ctx, cfg.Namespace, opts.podName)
	}()

	result.AddDetail("created pod %s with image %s", opts.podName, opts.imageDesc)

	var phase corev1.PodPhase
	err := clients.WaitForCondition(ctx, timeout, func(ctx context.Context) (bool, error) {
		p, err := clients.GetPod(ctx, cfg.Namespace, opts.podName)
		if err != nil {
			return false, nil
		}

		phase = p.Status.Phase
		switch phase {
		case corev1.PodSucceeded:
			return true, nil
		case corev1.PodFailed:
			return true, fmt.Errorf("pod %s failed", opts.podName)
		}
		return false, nil
	})

	if err != nil {
		result.Fail("waiting for pod %s: %v", opts.podName, err)
		return result
	}

	if phase == corev1.PodSucceeded {
		result.AddDetail("%s", opts.successMessage)
	} else {
		result.Fail("pod ended with phase: %s", phase)
	}

	return result
}
