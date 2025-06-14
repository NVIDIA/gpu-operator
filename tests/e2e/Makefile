# Copyright (c) 2023, NVIDIA CORPORATION.  All rights reserved.
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

GO_CMD ?= go

OPERATOR_REPOSITORY ?= nvcr.io/nvidia
OPERATOR_IMAGE ?= gpu-operator
OPERATOR_VERSION ?= v23.9.0

HELM_CHART ?= $(CURDIR)/../../deployments/gpu-operator

.PHONY: e2e-test
e2e-test:
	@if [ -z ${KUBECONFIG} ]; then \
		echo "[ERR] KUBECONFIG is missing, must be set"; \
		exit 1; \
	fi
	$(GO_CMD) test -v . -args \
		-kubeconfig=$(KUBECONFIG) \
		-operator-repository=$(OPERATOR_REPOSITORY) \
		-operator-image=$(OPERATOR_IMAGE) \
		-operator-version=$(OPERATOR_VERSION) \
		-validator-repository=$(OPERATOR_REPOSITORY) \
		-validator-image=$(OPERATOR_IMAGE) \
		-validator-version=$(OPERATOR_VERSION) \
		-helm-chart=$(HELM_CHART) \
		-ginkgo.focus="gpu-operator-e2e"
