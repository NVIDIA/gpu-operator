# Copyright (c) 2022, NVIDIA CORPORATION.  All rights reserved.
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

PUSH_ON_BUILD ?= false
DOCKER_BUILD_PLATFORM_OPTIONS ?= --platform=linux/amd64
DOCKER_BUILD_OPTIONS = --output=type=image,push=$(PUSH_ON_BUILD) --provenance=$(ATTACH_ATTESTATIONS) --sbom=$(ATTACH_ATTESTATIONS)
$(PUSH_TARGETS): OUT_IMAGE ?= $(IMAGE_NAME):$(IMAGE_TAG)
$(PUSH_TARGETS): push-%:
	$(DOCKER) tag "$(IMAGE_NAME):$(VERSION)-$(DEFAULT_PUSH_TARGET)" "$(OUT_IMAGE)"
	$(DOCKER) push "$(OUT_IMAGE)"
