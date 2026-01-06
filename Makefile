# Copyright (c) 2021, NVIDIA CORPORATION.  All rights reserved.
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

BUILD_MULTI_ARCH_IMAGES ?= false
DOCKER ?= docker
GO_CMD ?= go
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))

##### Global variables #####
include $(CURDIR)/versions.mk

MODULE := github.com/NVIDIA/gpu-operator
BUILDER_IMAGE ?= golang:$(GOLANG_VERSION)
GOPROXY ?= https://proxy.golang.org,direct

ifeq ($(IMAGE_NAME),)
REGISTRY ?= nvcr.io/nvidia/cloud-native
IMAGE_NAME := $(REGISTRY)/gpu-operator
endif

IMAGE_TAG ?= $(VERSION)
IMAGE = $(IMAGE_NAME):$(IMAGE_TAG)
BUILDIMAGE ?= $(IMAGE_NAME):$(IMAGE_TAG)-build

OUT_IMAGE_NAME ?= $(IMAGE_NAME)
OUT_IMAGE_VERSION ?= $(VERSION)
OUT_IMAGE_TAG = $(OUT_IMAGE_VERSION)
OUT_IMAGE = $(OUT_IMAGE_NAME):$(OUT_IMAGE_TAG)

# CHANNELS define the bundle channels used in the bundle.
# Add a new line here if you would like to change its default config. (E.g CHANNELS = "preview,fast,stable")
# To re-generate a bundle for other specific channels without changing the standard setup, you can:
# - use the CHANNELS as arg of the bundle target (e.g make bundle CHANNELS=preview,fast,stable)
# - use environment variables to overwrite this value (e.g export CHANNELS="preview,fast,stable")
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif

# DEFAULT_CHANNEL defines the default channel used in the bundle.
# Add a new line here if you would like to change its default config. (E.g DEFAULT_CHANNEL = "stable")
# To re-generate a bundle for any other default channel without changing the default setup, you can:
# - use the DEFAULT_CHANNEL as arg of the bundle target (e.g make bundle DEFAULT_CHANNEL=stable)
# - use environment variables to overwrite this value (e.g export DEFAULT_CHANNEL="stable")
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

# BUNDLE_IMAGE defines the image:tag used for the bundle.
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMAGE=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMAGE ?= gpu-operator-bundle:$(VERSION)

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

GOOS ?= linux
VERSION_PKG = github.com/NVIDIA/gpu-operator/internal/info

PWD = $(shell pwd)
CLIENT_GEN = $(PWD)/bin/client-gen
CONTROLLER_GEN = $(PWD)/bin/controller-gen
KUSTOMIZE = $(PWD)/bin/kustomize
GCOV2LCOV ?= $(PWD)/bin/gcov2lcov

# Run against the configured Kubernetes cluster in ~/.kube/config
run: generate check manifests
	go run ./cmd/gpu-operator/...

# Install CRDs into a cluster
install: manifests install-tools
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

# Uninstall CRDs from a cluster
uninstall: manifests install-tools
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

# Deploy gpu-operator in the configured Kubernetes cluster in ~/.kube/config
deploy: manifests generate-env install-tools
	cd config/manager && $(KUSTOMIZE) edit set image gpu-operator=${IMAGE}
	$(KUSTOMIZE) build config/default | kubectl apply -f -

generate-env:
	./hack/prepare-env.sh

# UnDeploy gpu-operator from the configured Kubernetes cluster in ~/.kube/config
undeploy:
	$(KUSTOMIZE) build config/default | kubectl delete -f -

# Generate manifests e.g. CRD, RBAC etc.
manifests: install-tools
	@echo "- Generating CRDs from the codebase"
	$(CONTROLLER_GEN) rbac:roleName=gpu-operator-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

# Generate code
generate: install-tools
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

generate-clientset: install-tools
	$(CLIENT_GEN) --go-header-file=$(CURDIR)/hack/boilerplate.go.txt \
		--clientset-name "versioned" \
		--output-dir $(CURDIR)/api \
		--output-pkg $(MODULE)/api \
		--input-base $(CURDIR)/api \
		--input nvidia/v1,nvidia/v1alpha1

# Generate bundle manifests and metadata, then validate generated files.
.PHONY: bundle
bundle: manifests install-tools
	operator-sdk generate kustomize manifests -q
	cd config/manager && $(KUSTOMIZE) edit set image gpu-operator=$(IMAGE)
	$(KUSTOMIZE) build config/manifests | operator-sdk generate bundle -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)
	operator-sdk bundle validate ./bundle

# Build the bundle image.
build-bundle-image:
	$(DOCKER) build \
	--build-arg DEFAULT_CHANNEL=$(DEFAULT_CHANNEL) \
	--build-arg GIT_COMMIT=$(GIT_COMMIT) \
	-f docker/bundle.Dockerfile -t $(BUNDLE_IMAGE) .

# Push the bundle image.
push-bundle-image: build-bundle-image
	$(DOCKER) push $(BUNDLE_IMAGE)

# Define local and dockerized golang targets

CMDS := $(patsubst ./cmd/%/,%,$(sort $(dir $(wildcard ./cmd/*/))))
CMD_TARGETS := $(patsubst %,cmd-%, $(CMDS))

CHECK_TARGETS := lint license-check validate-modules validate-generated-assets
MAKE_TARGETS := build check coverage cmds $(CMD_TARGETS) $(CHECK_TARGETS)
DOCKER_TARGETS := $(patsubst %,docker-%, $(MAKE_TARGETS))
.PHONY: $(MAKE_TARGETS) $(DOCKER_TARGETS)

# Generate an image for containerized builds
# Note: This image is local only
.PHONY: .build-image .pull-build-image .push-build-image
.build-image: docker/Dockerfile.devel
	if [ x"$(SKIP_IMAGE_BUILD)" = x"" ]; then \
		$(DOCKER) build \
			--progress=plain \
			--build-arg GOLANG_VERSION="$(GOLANG_VERSION)" \
			--tag $(BUILDIMAGE) \
			-f $(^) \
			docker; \
	fi

.pull-build-image:
	$(DOCKER) pull $(BUILDIMAGE)

.push-build-image:
	$(DOCKER) push $(BUILDIMAGE)

$(DOCKER_TARGETS): docker-%: .build-image
	@echo "Running 'make $(*)' in docker container $(BUILDIMAGE)"
	$(DOCKER) run \
		--rm \
		-e GOLANGCI_LINT_CACHE=/tmp/.cache \
		-e GOCACHE=/tmp/.cache \
		-v $(PWD):$(PWD) \
		-w $(PWD) \
		--user $$(id -u):$$(id -g) \
		$(BUILDIMAGE) \
			make $(*)

check: $(CHECK_TARGETS)

license-check:
	@echo ">> checking license header"
	@licRes=$$(for file in $$(find . -type f -iname '*.go' ! -path './vendor/*') ; do \
               awk 'NR<=5' $$file | grep -Eq "(Copyright|generated|GENERATED)" || echo $$file; \
       done); \
       if [ -n "$${licRes}" ]; then \
               echo "license header checking failed:"; echo "$${licRes}"; \
               exit 1; \
       fi

# Apply go fmt to the codebase
fmt:
	go list -f '{{.Dir}}' $(MODULE)/... \
		| xargs gofmt -s -l -d

# Apply goimports -local github.com/NVIDIA/gpu-operator to the codebase
goimports:
	find . -name \*.go -not -name "zz_generated.deepcopy.go" -not -path "./vendor/*" \
 		-exec goimports -local $(MODULE) -w {} \;

lint:
	golangci-lint run ./...

BUILD_FLAGS = -ldflags "-s -w -X $(VERSION_PKG).gitCommit=$(GIT_COMMIT) -X $(VERSION_PKG).version=$(VERSION)"
build:
	go build $(BUILD_FLAGS) ./...

cmds: $(CMD_TARGETS)
$(CMD_TARGETS): cmd-%:
	CGO_ENABLED=0 GOOS=$(GOOS) \
		go build $(BUILD_FLAGS) $(COMMAND_BUILD_OPTIONS) $(MODULE)/cmd/$(*)

sync-crds:
	@echo "- Syncing CRDs into Helm and OLM packages..."
	cp $(PROJECT_DIR)/config/crd/bases/* $(PROJECT_DIR)/deployments/gpu-operator/crds
	cp $(PROJECT_DIR)/config/crd/bases/* $(PROJECT_DIR)/bundle/manifests

TOOLS_DIR := $(PROJECT_DIR)/tools
E2E_TESTS_DIR := $(PROJECT_DIR)/tests/e2e
validate-modules:
	@echo "- Verifying that the dependencies have expected content..."
	go mod verify
	@echo "- Checking for any unused/missing packages in go.mod..."
	go mod tidy
	@git diff --exit-code -- go.sum go.mod
	@echo "- Checking if the vendor dir is in sync..."
	go mod vendor
	@git diff --exit-code -- vendor
	@echo "- [tools] Verifying that the dependencies have expected content..."
	go -C $(TOOLS_DIR) mod verify
	@echo "- [tools] Checking for any unused/missing packages in go.mod..."
	go -C $(TOOLS_DIR) mod tidy
	@git diff --exit-code -- $(TOOLS_DIR)/go.sum $(TOOLS_DIR)/go.mod

validate-csv: cmds
	./gpuop-cfg validate csv --input=./bundle/manifests/gpu-operator-certified.clusterserviceversion.yaml

validate-helm-values: cmds
	helm template gpu-operator deployments/gpu-operator --show-only templates/clusterpolicy.yaml --set gds.enabled=true | \
		sed '/^--/d' | \
		./gpuop-cfg validate clusterpolicy --input="-"

validate-generated-assets: manifests generate generate-clientset sync-crds
	@echo "- Verifying that the generated code and manifests are in-sync..."
	@git diff --exit-code -- api config bundle deployments

COVERAGE_FILE := coverage.out
unit-test: build
	go list -f {{.Dir}} $(MODULE)/... | grep -v /tests/e2e \
		| xargs go test -v -coverprofile=$(COVERAGE_FILE)

coverage: unit-test
	cat $(COVERAGE_FILE) | grep -v "_mock.go" > $(COVERAGE_FILE).no-mocks
	go tool cover -func=$(COVERAGE_FILE).no-mocks

cov-report: coverage install-tools
	$(GCOV2LCOV) -infile $(COVERAGE_FILE) -outfile lcov.info

##### Public rules #####
PUSH_TARGETS := push-image
BUILD_TARGETS := build-image
TEST_TARGETS := test

ifneq ($(BUILD_MULTI_ARCH_IMAGES),true)
include $(CURDIR)/native-only.mk
else
include $(CURDIR)/multi-arch.mk
endif

ALL_TARGETS := $(PUSH_TARGETS) $(BUILD_TARGETS) $(TEST_TARGETS) docker-image
.PHONY: $(ALL_TARGETS)

build-%: DOCKERFILE = $(CURDIR)/docker/Dockerfile

build-image:
		$(DOCKER) build --pull \
		$(DOCKER_BUILD_OPTIONS) \
		$(DOCKER_BUILD_PLATFORM_OPTIONS) \
		--tag $(IMAGE) \
		--build-arg VERSION="$(VERSION)" \
		--build-arg BUILDER_IMAGE="$(BUILDER_IMAGE)" \
		--build-arg GOLANG_VERSION="$(GOLANG_VERSION)" \
		--build-arg GIT_COMMIT="$(GIT_COMMIT)" \
		--build-arg GOPROXY="$(GOPROXY)" \
		--file $(DOCKERFILE) $(CURDIR)

# Provide a utility target to build the images to allow for use in external tools.
# This includes https://github.com/openshift-psap/ci-artifacts
docker-image: OUT_IMAGE ?= $(IMAGE_NAME):$(IMAGE_TAG)

install-tools:
	@echo Installing tools from tools.go
	export GOBIN=$(PROJECT_DIR)/bin && \
	grep '^\s*_' tools/tools.go | awk '{print $$2}' | xargs -tI % $(GO_CMD) install -mod=readonly -modfile=tools/go.mod %
