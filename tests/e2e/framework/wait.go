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

package framework

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	waitForPollInterval     = 5 * time.Second
	waitForProgressInterval = time.Minute
)

type checkFailure struct {
	message  string
	location string
}

func (failure *checkFailure) Error() string {
	return failure.message
}

func (failure *checkFailure) FormattedGomegaError() string {
	return fmt.Sprintf("The check failed at %s with:\n%s", failure.location, failure.message)
}

func (failure *checkFailure) reason() string {
	firstLine, _, _ := strings.Cut(failure.message, "\n")
	return firstLine
}

func runCheck(check func(g gomega.Gomega)) (failure *checkFailure) {
	defer func() {
		if recovered := recover(); recovered != nil {
			recoveredFailure, isCheckFailure := recovered.(*checkFailure)
			if !isCheckFailure {
				panic(recovered)
			}
			failure = recoveredFailure
		}
	}()
	check(gomega.NewGomega(func(message string, callerSkip ...int) {
		skip := 0
		if len(callerSkip) > 0 {
			skip = callerSkip[0]
		}
		_, file, line, _ := runtime.Caller(skip + 1)
		panic(&checkFailure{message: message, location: fmt.Sprintf("%s:%d", file, line)})
	}))
	return nil
}

func WaitFor(ctx context.Context, timeout time.Duration, description string, check func(g gomega.Gomega)) time.Duration {
	ginkgo.GinkgoHelper()
	ginkgo.By(description)
	waitStart := time.Now()
	lastProgressReport := waitStart
	gomega.Eventually(ctx, func() error {
		failure := runCheck(check)
		if failure == nil {
			return nil
		}
		if time.Since(lastProgressReport) >= waitForProgressInterval {
			lastProgressReport = time.Now()
			ginkgo.GinkgoWriter.Printf("Still waiting after %s: %s\n", time.Since(waitStart).Round(time.Second), failure.reason())
		}
		return failure
	}).WithTimeout(timeout).WithPolling(waitForPollInterval).Should(gomega.Succeed())
	return time.Since(waitStart)
}

func WaitForPodNotFound(ctx context.Context, kubeClient kubernetes.Interface, namespace, podName string, timeout time.Duration) {
	ginkgo.GinkgoHelper()
	elapsed := WaitFor(ctx, timeout, fmt.Sprintf("Waiting for pod %s/%s to be deleted", namespace, podName), func(g gomega.Gomega) {
		_, err := kubeClient.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		g.Expect(err).To(gomega.MatchError(apierrors.IsNotFound, "a NotFound error"), "pod %s/%s still exists", namespace, podName)
	})
	ginkgo.GinkgoWriter.Printf("Pod %s/%s has been deleted (%s)\n", namespace, podName, elapsed.Round(time.Second))
}

func WaitForPodOrphaned(ctx context.Context, kubeClient kubernetes.Interface, namespace, podName string, timeout time.Duration) {
	ginkgo.GinkgoHelper()
	podDeleted := false
	elapsed := WaitFor(ctx, timeout, fmt.Sprintf("Waiting for pod %s/%s to be orphaned", namespace, podName), func(g gomega.Gomega) {
		pod, err := kubeClient.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			podDeleted = true
			return
		}
		g.Expect(err).NotTo(gomega.HaveOccurred(), "failed to get pod %s/%s", namespace, podName)
		g.Expect(pod.OwnerReferences).To(gomega.BeEmpty(), "pod %s/%s is still owned by %s", namespace, podName, describeOwners(pod.OwnerReferences))
	})
	if podDeleted {
		ginkgo.GinkgoWriter.Printf("Pod %s/%s has been deleted (%s)\n", namespace, podName, elapsed.Round(time.Second))
		return
	}
	ginkgo.GinkgoWriter.Printf("Pod %s/%s is orphaned (%s)\n", namespace, podName, elapsed.Round(time.Second))
}

func describeOwners(ownerReferences []metav1.OwnerReference) string {
	ownerNames := make([]string, 0, len(ownerReferences))
	for _, ownerReference := range ownerReferences {
		ownerNames = append(ownerNames, ownerReference.Kind+"/"+ownerReference.Name)
	}
	return strings.Join(ownerNames, ", ")
}
