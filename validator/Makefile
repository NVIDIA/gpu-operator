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

VERSION ?= v0.1.0

DOCKER ?= docker
CUDA_IMAGE ?= nvidia/cuda
CUDA_VERSION ?= 11.4.2
GOLANG_VERSION ?= 1.17
BUILDER_IMAGE ?= golang:$(GOLANG_VERSION)
BUILD_DIR ?= ../

ifeq ($(IMAGE),)
REGISTRY ?= nvcr.io/nvidia/cloud-native
IMAGE := $(REGISTRY)/gpu-operator-validator
endif

##### Public rules #####
DEFAULT_PUSH_TARGET := ubi8
TARGETS := ubi8 ubuntu20.04

PUSH_TARGETS := $(patsubst %, push-%, $(TARGETS))
BUILD_TARGETS := $(patsubst %, build-%, $(TARGETS))
TEST_TARGETS := $(patsubst %, test-%, $(TARGETS))

.PHONY: $(TARGETS) $(PUSH_TARGETS) $(BUILD_TARGETS) $(TEST_TARGETS)

all: $(TARGETS)

push-all: $(PUSH_TARGETS)
build-all: $(BUILD_TARGETS)

$(PUSH_TARGETS): push-%:
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

%-ubi8: BASE_DIST := ubi8

# Both ubi8 and build-ubi8 trigger a build of the relevant image
$(TARGETS): %: build-%
$(BUILD_TARGETS): build-%:
		$(DOCKER) build --pull \
			--build-arg BASE_DIST="$(BASE_DIST)" \
			--build-arg CUDA_IMAGE="$(CUDA_IMAGE)" \
			--build-arg CUDA_VERSION="$(CUDA_VERSION)" \
			--build-arg VERSION="$(VERSION)" \
			--build-arg BUILDER_IMAGE="$(BUILDER_IMAGE)" \
			--build-arg GOLANG_VERSION="$(GOLANG_VERSION)" \
			--build-arg CUDA_SAMPLE_IMAGE=nvcr.io/nvidia/k8s/cuda-sample:vectoradd-cuda11.2.1-$(*) \
			--tag $(IMAGE):$(VERSION)-$(*) \
			--file Dockerfile \
			$(BUILD_DIR)

# Provide a utility target to build the images to allow for use in external tools.
# This includes https://github.com/openshift-psap/ci-artifacts
.PHONY: docker-image
docker-image: OUT_IMAGE ?= $(IMAGE):$(VERSION)-$(DEFAULT_PUSH_TARGET)
docker-image: $(DEFAULT_PUSH_TARGET)
	$(DOCKER) tag $(IMAGE):$(VERSION)-$(DEFAULT_PUSH_TARGET) $(OUT_IMAGE)
