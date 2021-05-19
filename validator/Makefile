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
GOLANG_VERSION ?= 1.15
BUILD_DIR ?= ../

ifeq ($(IMAGE),)
REGISTRY ?= nvcr.io/nvidia/cloud-native
IMAGE := $(REGISTRY)/gpu-operator-validator
endif

##### Public rules #####
DEFAULT_PUSH_TARGET := ubi8
TARGETS := ubi8

PUSH_TARGETS := $(patsubst %, push-%, $(TARGETS))
BUILD_TARGETS := $(patsubst %, build-%, $(TARGETS))
TEST_TARGETS := $(patsubst %, test-%, $(TARGETS))

.PHONY: $(TARGETS) $(PUSH_TARGETS) $(BUILD_TARGETS) $(TEST_TARGETS)

all: $(TARGETS)

push-all: $(PUSH_TARGETS)
build-all: $(BUILD_TARGETS)

$(PUSH_TARGETS): push-%:
	$(DOCKER) push "$(IMAGE):$(VERSION)-$(*)"

# For the default push target push the short tag
push-$(DEFAULT_PUSH_TARGET): push-short

push-short:
	$(DOCKER) tag "$(IMAGE):$(VERSION)-$(DEFAULT_PUSH_TARGET)" "$(IMAGE):$(VERSION)"
	$(DOCKER) push "$(IMAGE):$(VERSION)"

# Both ubi8 and build-ubi8 trigger a build of the relevant image
$(TARGETS): %: build-%
$(BUILD_TARGETS): build-%:
		$(DOCKER) build --pull \
			--build-arg VERSION=$(VERSION) \
			--build-arg GOLANG_VERSION=$(GOLANG_VERSION) \
			--build-arg BASE_IMAGE=nvcr.io/nvidia/cuda:11.2.1-base-$(*) \
			--build-arg CUDA_SAMPLE_IMAGE=nvcr.io/nvidia/k8s/cuda-sample:vectoradd-cuda11.2.1-$(*) \
			--tag $(IMAGE):$(VERSION) \
			--file Dockerfile \
			$(BUILD_DIR)