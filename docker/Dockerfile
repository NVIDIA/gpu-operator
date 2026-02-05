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

ARG CUDA_SAMPLES_VERSION=12.9

FROM golang:1.25.7 AS builder

ARG GOPROXY="https://proxy.golang.org,direct"
ENV GOPROXY=$GOPROXY

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
COPY vendor vendor

# Copy the go source
COPY cmd/ cmd/
COPY api/ api/
COPY controllers/ controllers/
COPY internal/ internal/

# Copy Makefile
COPY Makefile Makefile
COPY *.mk .

# Build
ARG VERSION="unknown"
ARG GIT_COMMIT="unknown"
RUN make cmds

FROM registry.access.redhat.com/ubi9/ubi:latest AS sample-builder

RUN ARCH=$(uname -m) && OS_ARCH=${ARCH/amd64/x86_64} && OS_ARCH=${ARCH/aarch64/sbsa} && OS_ARCH=${ARCH/arm64/sbsa} \
    dnf config-manager --add-repo https://developer.download.nvidia.com/compute/cuda/repos/rhel9/$OS_ARCH/cuda-rhel9.repo

ARG CUDA_SAMPLES_VERSION
RUN CUDA_PACKAGE_VERSION=${CUDA_SAMPLES_VERSION/./-} && \
    dnf install -y --allowerasing \
        cmake \
        cuda-cudart-devel-${CUDA_PACKAGE_VERSION} \
        cuda-compat-${CUDA_PACKAGE_VERSION} \
        cuda-nvcc-${CUDA_PACKAGE_VERSION} \
        g++ \
        curl \
    && \
    dnf clean all

ENV PATH=/usr/local/cuda/bin:${PATH}

WORKDIR /build

ARG SAMPLE_NAME=vectorAdd

RUN curl -L https://codeload.github.com/NVIDIA/cuda-samples/tar.gz/refs/tags/v${CUDA_SAMPLES_VERSION} | \
    tar -xzvf - --strip-components=1 --wildcards */${SAMPLE_NAME}/* --wildcards */Common/* && \
    cd $(find /build/Samples -iname "${SAMPLE_NAME}") && \
    cmake . && \
    make && \
    cp ${SAMPLE_NAME} /build/${SAMPLE_NAME}

# The C/C++ distroless image is used as a base since the CUDA vectorAdd
# sample application depends on C/C++ libraries.
FROM nvcr.io/nvidia/distroless/cc:v4.0.1-dev

ENV NVIDIA_VISIBLE_DEVICES=void

ARG VERSION="unknown"
ARG GIT_COMMIT="unknown"

LABEL io.k8s.display-name="NVIDIA GPU Operator"
LABEL name="NVIDIA GPU Operator"
LABEL vendor="NVIDIA"
LABEL version="${VERSION}"
LABEL release="N/A"
LABEL summary="Automate the management and monitoring of NVIDIA GPUs."
LABEL description="See summary"
LABEL vsc-ref=${GIT_COMMIT}

WORKDIR /
COPY --from=builder /workspace/gpu-operator /usr/bin/
COPY --from=builder /workspace/manage-crds /usr/bin/
COPY --from=builder /workspace/nvidia-validator /usr/bin/
COPY --from=sample-builder /build/vectorAdd /usr/bin/vectorAdd
ARG CUDA_SAMPLES_VERSION
COPY --from=sample-builder /usr/local/cuda-${CUDA_SAMPLES_VERSION}/compat /usr/local/cuda/compat

COPY assets /opt/gpu-operator/
COPY manifests /opt/gpu-operator/manifests
COPY validator/manifests /opt/validator/manifests

COPY hack/must-gather.sh /usr/bin/gather

# Add CRD resource into the image for helm upgrades
COPY deployments/gpu-operator/crds/nvidia.com_clusterpolicies.yaml /opt/gpu-operator/nvidia.com_clusterpolicies.yaml
COPY deployments/gpu-operator/crds/nvidia.com_nvidiadrivers.yaml /opt/gpu-operator/nvidia.com_nvidiadrivers.yaml
COPY deployments/gpu-operator/charts/node-feature-discovery/crds/nfd-api-crds.yaml /opt/gpu-operator/nfd-api-crds.yaml

USER 65532:65532

COPY LICENSE /licenses/

ENTRYPOINT ["/usr/bin/gpu-operator"]
