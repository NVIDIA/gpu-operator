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

ARG CUDA_IMAGE=nvidia/cuda
ARG CUDA_VERSION=undefined

ARG BASE_DIST=ubi8

ARG GOLANG_VERSION=1.17
ARG BUILDER_IMAGE=golang:${GOLANG_VERSION}

FROM ${BUILDER_IMAGE} as builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
COPY vendor vendor

# Copy the go source
COPY main.go main.go
COPY api/ api/
COPY controllers/ controllers/

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on \
    go build -a -o gpu-operator main.go

FROM ${CUDA_IMAGE}:${CUDA_VERSION}-base-${BASE_DIST}

ENV NVIDIA_VISIBLE_DEVICES=void

ARG VERSION

LABEL io.k8s.display-name="NVIDIA GPU Operator"
LABEL name="NVIDIA GPU Operator"
LABEL vendor="NVIDIA"
LABEL version="${VERSION}"
LABEL release="N/A"
LABEL summary="Automate the management and monitoring of NVIDIA GPUs."
LABEL description="See summary"

WORKDIR /
COPY --from=builder /workspace/gpu-operator /usr/bin/

RUN mkdir -p /opt/gpu-operator
COPY assets /opt/gpu-operator/
COPY ./LICENSE /licenses/LICENSE
COPY hack/must-gather.sh /usr/bin/gather

ENV VERSION=${VERSION}

# Install must-gather dependency: `kubectl`
RUN curl -LO https://storage.googleapis.com/kubernetes-release/release/$(curl -s https://storage.googleapis.com/kubernetes-release/release/stable.txt)/bin/linux/amd64/kubectl
RUN chmod +x ./kubectl
RUN mv ./kubectl /usr/local/bin

RUN useradd gpu-operator
USER gpu-operator

ENTRYPOINT ["/usr/bin/gpu-operator"]
