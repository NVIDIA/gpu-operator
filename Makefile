# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= 0.0.1

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

# BUNDLE_IMG defines the image:tag used for the bundle.
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= gpu-operator-bundle:$(VERSION)

# Image URL to use all building/pushing image targets
IMG ?= gpu-operator:$(VERSION)
# Produce CRDs that work back to Kubernetes 1.11 (no version conversion)
CRD_OPTIONS ?= "crd:trivialVersions=true,preserveUnknownFields=false"

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

all: gpu-operator

# Run tests
ENVTEST_ASSETS_DIR=$(shell pwd)/testbin
test: generate fmt vet manifests
	mkdir -p ${ENVTEST_ASSETS_DIR}
	test -f ${ENVTEST_ASSETS_DIR}/setup-envtest.sh || curl -sSLo ${ENVTEST_ASSETS_DIR}/setup-envtest.sh https://raw.githubusercontent.com/kubernetes-sigs/controller-runtime/v0.7.0/hack/setup-envtest.sh
	source ${ENVTEST_ASSETS_DIR}/setup-envtest.sh; fetch_envtest_tools $(ENVTEST_ASSETS_DIR); setup_envtest_env $(ENVTEST_ASSETS_DIR); go test ./... -coverprofile cover.out

unit-test:
	go test ./... -coverprofile cover.out

# Build gpu-operator binary
gpu-operator: generate fmt vet
	go build -o bin/gpu-operator main.go

# Run against the configured Kubernetes cluster in ~/.kube/config
run: generate fmt vet manifests
	go run ./main.go

# Install CRDs into a cluster
install: manifests kustomize
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

# Uninstall CRDs from a cluster
uninstall: manifests kustomize
	$(KUSTOMIZE) build config/crd | kubectl delete -f -

# Deploy gpu-operator in the configured Kubernetes cluster in ~/.kube/config
deploy: manifests kustomize
	cd config/manager && $(KUSTOMIZE) edit set image gpu-operator=${IMG}
	$(KUSTOMIZE) build config/default | kubectl apply -f -

# UnDeploy gpu-operator from the configured Kubernetes cluster in ~/.kube/config
undeploy:
	$(KUSTOMIZE) build config/default | kubectl delete -f -

# Generate manifests e.g. CRD, RBAC etc.
manifests: controller-gen
	$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=gpu-operator-role webhook paths="./..." output:crd:artifacts:config=config/crd/bases

lint:
	golint -set_exit_status ./...

# Run go fmt against code
fmt:
	go fmt ./...

# Run go vet against code
vet:
	go vet ./...

assign:
	ineffassign ./...

misspell:
	misspell .

# Generate code
generate: controller-gen
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

devel-image:
	docker build -t $(IMG) -f docker/Dockerfile.devel .

# Push the docker image
docker-push:
	docker push ${IMG}

# Download controller-gen locally if necessary
CONTROLLER_GEN = $(shell pwd)/bin/controller-gen
controller-gen:
	$(call go-get-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen@v0.4.1)

# Download kustomize locally if necessary
KUSTOMIZE = $(shell pwd)/bin/kustomize
kustomize:
	$(call go-get-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v3@v3.8.7)

# go-get-tool will 'go get' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-get-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
echo "Downloading $(2)" ;\
GOBIN=$(PROJECT_DIR)/bin go get $(2) ;\
rm -rf $$TMP_DIR ;\
}
endef

# Generate bundle manifests and metadata, then validate generated files.
.PHONY: bundle
bundle: manifests kustomize
	operator-sdk generate kustomize manifests -q
	cd config/manager && $(KUSTOMIZE) edit set image gpu-opertor=$(IMG)
	$(KUSTOMIZE) build config/manifests | operator-sdk generate bundle -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)
	operator-sdk bundle validate ./bundle

# Build the bundle image.
.PHONY: bundle-build
bundle-build:
	docker build -f docker/bundle.Dockerfile -t $(BUNDLE_IMG) .


CUDA_IMAGE ?= nvidia/cuda
CUDA_VERSION ?= 11.2.1
GOLANG_VERSION ?= 1.15
BUILDER_IMAGE ?= golang:$(GOLANG_VERSION)
DOCKER   ?= docker
ifeq ($(IMAGE),)
REGISTRY ?= nvcr.io/nvidia/cloud-native
IMAGE := $(REGISTRY)/gpu-operator
endif

##### Public rules #####
DEFAULT_PUSH_TARGET := ubi8
TARGETS := ubi8

PUSH_TARGETS := $(patsubst %,push-%, $(TARGETS))
BUILD_TARGETS := $(patsubst %,build-%, $(TARGETS))
TEST_TARGETS := $(patsubst %,test-%, $(TARGETS))

ALL_TARGETS := $(TARGETS) $(PUSH_TARGETS) $(BUILD_TARGETS) $(TEST_TARGETS)
.PHONY: $(ALL_TARGETS)

$(PUSH_TARGETS): push-%: validator-push-%
	$(DOCKER) push "$(IMAGE):$(VERSION)-$(*)"

# For the default push target we also push a short tag equal to the version.
# We skip this for the development release
RELEASE_DEVEL_TAG ?= devel
ifneq ($(strip $(VERSION)),$(RELEASE_DEVEL_TAG))
push-$(DEFAULT_PUSH_TARGET): push-short
endif
push-short:
	$(DOCKER) tag "$(IMAGE):$(VERSION)-$(DEFAULT_PUSH_TARGET)" "$(IMAGE):$(VERSION)"
	$(DOCKER) push "$(IMAGE):$(VERSION)"

build-ubi8: DOCKERFILE := docker/Dockerfile
build-ubi8: BASE_DIST := ubi8

$(TARGETS): %: build-%
$(BUILD_TARGETS): build-%: validator-build-%
	$(DOCKER) build --pull \
		--tag $(IMAGE):$(VERSION)-$(*) \
		--build-arg BASE_DIST="$(BASE_DIST)" \
		--build-arg CUDA_IMAGE="$(CUDA_IMAGE)" \
		--build-arg CUDA_VERSION="$(CUDA_VERSION)" \
		--build-arg VERSION="$(VERSION)" \
		--build-arg BUILDER_IMAGE="$(BUILDER_IMAGE)" \
		--build-arg GOLANG_VERSION="$(GOLANG_VERSION)" \
		--file $(DOCKERFILE) .

validator-%:
	make -C validator IMAGE=$(IMAGE)-validator $(*)

# Provide a utility target to build the images to allow for use in external tools.
# This includes https://github.com/openshift-psap/ci-artifacts
.PHONY: docker-image
docker-image: $(DEFAULT_PUSH_TARGET)
	docker tag $(IMAGE):$(VERSION)-$(DEFAULT_PUSH_TARGET) $(OUT_IMAGE)
