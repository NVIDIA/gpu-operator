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

BUILD_MULTI_ARCH_IMAGES ?= no
DOCKER ?= docker
BUILDX  =
ifeq ($(BUILD_MULTI_ARCH_IMAGES),true)
BUILDX = buildx
endif

##### Global variables #####
include $(CURDIR)/versions.mk

CUDA_IMAGE ?= nvcr.io/nvidia/cuda
BUILDER_IMAGE ?= golang:$(GOLANG_VERSION)

ifeq ($(IMAGE_NAME),)
REGISTRY ?= nvcr.io/nvidia/cloud-native
IMAGE_NAME := $(REGISTRY)/gpu-operator-validator
endif

BUILD_DIR ?= ../

IMAGE_VERSION := $(VERSION)
IMAGE_TAG ?= $(IMAGE_VERSION)-$(DIST)
IMAGE = $(IMAGE_NAME):$(IMAGE_TAG)

OUT_IMAGE_NAME ?= $(IMAGE_NAME)
OUT_IMAGE_VERSION ?= $(VERSION)
OUT_IMAGE_TAG = $(OUT_IMAGE_VERSION)-$(DIST)
OUT_IMAGE = $(OUT_IMAGE_NAME):$(OUT_IMAGE_TAG)

##### Public rules #####
DISTRIBUTIONS := ubi8 ubuntu20.04
DEFAULT_PUSH_TARGET := ubi8

PUSH_TARGETS := $(patsubst %,push-%, $(DISTRIBUTIONS))
BUILD_TARGETS := $(patsubst %,build-%, $(DISTRIBUTIONS))
TEST_TARGETS := $(patsubst %,test-%, $(DISTRIBUTIONS))

ifneq ($(BUILD_MULTI_ARCH_IMAGES),true)
include $(CURDIR)/native-only.mk
else
include $(CURDIR)/multi-arch.mk
endif

ALL_TARGETS := $(DISTRIBUTIONS) $(PUSH_TARGETS) $(BUILD_TARGETS) $(TEST_TARGETS) docker-image validator
.PHONY: $(ALL_TARGETS)

GOOS ?= linux
VERSION_PKG = github.com/NVIDIA/gpu-operator/internal/info

validator:
	CGO_ENABLED=0 GOOS=$(GOOS) \
		go build -ldflags "-s -w -X $(VERSION_PKG).gitCommit=$(GIT_COMMIT) -X $(VERSION_PKG).version=$(VERSION)" -o validator .

# For the default push target we also push a short tag equal to the version.
# We skip this for the development release
DEVEL_RELEASE_IMAGE_VERSION ?= devel
ifneq ($(strip $(VERSION)),$(DEVEL_RELEASE_IMAGE_VERSION))
push-$(DEFAULT_PUSH_TARGET): push-short
endif

push-%: DIST = $(*)
push-short: DIST = $(DEFAULT_PUSH_TARGET)

build-%: DIST = $(*)
build-%: DOCKERFILE = $(CURDIR)/Dockerfile

$(DISTRIBUTIONS): %: build-%:
$(BUILD_TARGETS): build-%:
	DOCKER_BUILDKIT=1 \
		$(DOCKER) $(BUILDX) build --pull \
		$(DOCKER_BUILD_OPTIONS) \
		$(DOCKER_BUILD_PLATFORM_OPTIONS) \
		--tag $(IMAGE) \
		--build-arg BASE_DIST="$(DIST)" \
		--build-arg CUDA_IMAGE="$(CUDA_IMAGE)" \
		--build-arg CUDA_VERSION="$(CUDA_VERSION)" \
		--build-arg VERSION="$(VERSION)" \
		--build-arg GIT_COMMIT="$(GIT_COMMIT)" \
		--build-arg BUILDER_IMAGE="$(BUILDER_IMAGE)" \
		--build-arg CUDA_SAMPLE_IMAGE=nvcr.io/nvidia/k8s/cuda-sample:vectoradd-cuda$(CUDA_SAMPLES_VERSION)-$(*) \
		--build-arg GOLANG_VERSION="$(GOLANG_VERSION)" \
		--build-arg CVE_UPDATES="$(CVE_UPDATES)" \
		--file $(DOCKERFILE) $(BUILD_DIR)

# Provide a utility target to build the images to allow for use in external tools.
# This includes https://github.com/openshift-psap/ci-artifacts
docker-image: OUT_IMAGE ?= $(IMAGE_NAME):$(IMAGE_TAG)
docker-image: $(DEFAULT_PUSH_TARGET)
	$(DOCKER) tag $(IMAGE_NAME):$(IMAGE_TAG) $(OUT_IMAGE)

