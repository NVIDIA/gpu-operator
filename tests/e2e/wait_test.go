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

package e2e

import (
	"context"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

const (
	pollInterval = 5 * time.Second

	operandReadyTimeout = 45 * time.Minute
	podDeletionTimeout  = 45 * time.Minute
	gpuWorkloadTimeout  = 45 * time.Minute
	helmInstallTimeout  = 5 * time.Minute
)

func waitFor(ctx context.Context, timeout time.Duration, description string, check func(g gomega.Gomega)) {
	ginkgo.GinkgoHelper()
	ginkgo.By(description)
	gomega.Eventually(ctx, check).WithTimeout(timeout).WithPolling(pollInterval).Should(gomega.Succeed())
}

// waitForNotFound keeps polling while get succeeds or fails with an error other than NotFound.
func waitForNotFound(ctx context.Context, timeout time.Duration, description string, get func(ctx context.Context) error) {
	ginkgo.GinkgoHelper()
	waitFor(ctx, timeout, description, func(g gomega.Gomega) {
		g.Expect(get(ctx)).To(gomega.MatchError(apierrors.IsNotFound, "a NotFound error"))
	})
}
