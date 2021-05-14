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

ARG BASE_IMAGE=undefined
ARG GOLANG_VERSION=undefined
ARG CUDA_SAMPLE_IMAGE=undefined

FROM golang:${GOLANG_VERSION} AS build

WORKDIR /build
COPY . .

RUN go build -o /artifacts/nvidia-validator ./validator
RUN cp /build/validator/plugin-workload-validation.yaml /artifacts/
RUN cp /build/validator/cuda-workload-validation.yaml /artifacts/

FROM ${CUDA_SAMPLE_IMAGE} AS sample-builder
RUN mkdir /artifacts
RUN cp /tmp/vectorAdd /artifacts/vectorAdd

FROM ${BASE_IMAGE}

COPY ./LICENSE ./licenses/LICENSE
COPY --from=build /artifacts/nvidia-validator  /usr/bin/nvidia-validator
COPY --from=sample-builder /artifacts/vectorAdd /usr/bin/vectorAdd
RUN mkdir -p /var/nvidia/manifests
COPY --from=build /artifacts/plugin-workload-validation.yaml /var/nvidia/manifests
COPY --from=build /artifacts/cuda-workload-validation.yaml /var/nvidia/manifests

ENV NVIDIA_DISABLE_REQUIRE="true"
ENV NVIDIA_VISIBLE_DEVICES=all
ENV NVIDIA_DRIVER_CAPABILITIES=utility,compute

ARG VERSION="N/A"
LABEL version=${VERSION}
LABEL release="N/A"
LABEL vendor="NVIDIA"
LABEL io.k8s.display-name="NVIDIA Validators for the GPU Operator Components"
LABEL name="NVIDIA Validator for the GPU Operator"
LABEL summary="NVIDIA Validator for the GPU Operator"
LABEL description="See summary"

ENTRYPOINT ["/bin/bash"]
