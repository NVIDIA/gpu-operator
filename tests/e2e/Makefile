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

TARGET_DRIVER_VERSION ?= $(shell sed -n 's/^: $${TARGET_DRIVER_VERSION:="\(.*\)"}$$/\1/p' $(CURDIR)/../scripts/.definitions.sh)

LOG_DIR ?= /tmp/logs

GINKGO_LABEL_FILTER ?= clusterpolicy

.PHONY: e2e-test
e2e-test:
	@if [ -z ${KUBECONFIG} ]; then \
		echo "[ERR] KUBECONFIG is missing, must be set"; \
		exit 1; \
	fi
	$(GO_CMD) test -v -timeout 0 . -args \
		-kubeconfig=$(KUBECONFIG) \
		$(if $(TEST_NAMESPACE),-namespace=$(TEST_NAMESPACE)) \
		-operator-repository=$(OPERATOR_REPOSITORY) \
		-operator-image=$(OPERATOR_IMAGE) \
		-operator-version=$(OPERATOR_VERSION) \
		-helm-chart=$(HELM_CHART) \
		$(if $(HELM_CHART_VERSION),-helm-chart-version=$(HELM_CHART_VERSION)) \
		$(if $(VALUES_FILE),-values-file=$(VALUES_FILE)) \
		$(if $(CONTAINER_RUNTIME),-container-runtime=$(CONTAINER_RUNTIME)) \
		$(if $(TARGET_DRIVER_VERSION),-target-driver-version=$(TARGET_DRIVER_VERSION)) \
		-log-dir=$(LOG_DIR) \
		-ginkgo.timeout=65m \
		-ginkgo.fail-on-empty \
		-ginkgo.focus="gpu-operator-e2e" \
		-ginkgo.label-filter="$(GINKGO_LABEL_FILTER)"
