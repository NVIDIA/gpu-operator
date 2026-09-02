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

# R580 is the minimum driver branch supported by gpu-operator and
# its backcompat stretches all the way back to the Maxwell architecure.
# CUDA SAMPLES v12.9 is used as that is the latest version with Maxwell support.
ARG CUDA_SAMPLES_VERSION=12.9

FROM golang:1.26.6@sha256:0d1d3a794be25f809dd2cb3160d8c73276c4056a9f8242a138e908ddeee7b6b6 AS builder

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

FROM registry.access.redhat.com/ubi9/ubi:latest@sha256:5426a8f45e80a07168a30ea24d84f266094b3756624a5508cc53927e6ee39e09 AS sample-builder

RUN ARCH=$(uname -m) && OS_ARCH=${ARCH/amd64/x86_64} && OS_ARCH=${ARCH/aarch64/sbsa} && OS_ARCH=${ARCH/arm64/sbsa} \
    dnf config-manager --add-repo https://developer.download.nvidia.com/compute/cuda/repos/rhel9/$OS_ARCH/cuda-rhel9.repo

ARG CUDA_SAMPLES_VERSION
RUN CUDA_PACKAGE_VERSION=${CUDA_SAMPLES_VERSION/./-} && \
    dnf install -y --allowerasing --setopt=install_weak_deps=False \
        cmake \
        cuda-cudart-devel-${CUDA_PACKAGE_VERSION} \
        cuda-compat-${CUDA_PACKAGE_VERSION} \
        cuda-nvcc-${CUDA_PACKAGE_VERSION} \
        g++ \
        curl \
    && \
    dnf clean all && rm -rf /var/cache/dnf

ENV PATH=/usr/local/cuda/bin:${PATH}

WORKDIR /build

ARG SAMPLE_NAME=vectorAdd

RUN curl -L https://codeload.github.com/NVIDIA/cuda-samples/tar.gz/refs/tags/v${CUDA_SAMPLES_VERSION} | \
    tar -xzvf - --strip-components=1 --wildcards */${SAMPLE_NAME}/* --wildcards */Common/* && \
    cd $(find /build/Samples -iname "${SAMPLE_NAME}") && \
    cmake . && \
    make && \
    cp ${SAMPLE_NAME} /build/${SAMPLE_NAME}

# Build a static busybox layout: one binary plus applet symlinks (sh, rm,
# ln, sleep, cat, ...) so PATH-resolved commands in init-container wrappers
# and lifecycle hooks keep working on the non-*-dev* distroless base.
FROM debian:trixie-slim@sha256:3a39a0592364683e6bab97937b72cad5a8fa6dcbbee90edb3bb48c7f8e94f258 AS shell
RUN apt-get update \
 && apt-get install -y --no-install-recommends busybox-static \
 && rm -rf /var/lib/apt/lists/* \
 && mkdir /busybox \
 && cp /bin/busybox /busybox/busybox \
 && /busybox/busybox --install -s /busybox

# The C/C++ distroless image is used as a base since the CUDA vectorAdd
# sample application depends on C/C++ libraries.
FROM nvcr.io/nvidia/distroless/cc:v4.1.1@sha256:8679a8fdeb897906b22cd7cfecb17f9711271dbc6ce1da21354eef76afb11d1b

ENV NVIDIA_VISIBLE_DEVICES=void

ARG VERSION="unknown"
ARG GIT_COMMIT="unknown"
ARG GIT_COMMIT_SHA_FULL="unknown"
ARG BUILD_TIMESTAMP

LABEL io.k8s.display-name="NVIDIA GPU Operator"
LABEL name="NVIDIA GPU Operator"
LABEL vendor="NVIDIA"
LABEL version="${VERSION}"
LABEL release="N/A"
LABEL summary="Automate the management and monitoring of NVIDIA GPUs."
LABEL description="See summary"
LABEL vsc-ref=${GIT_COMMIT}
LABEL org.opencontainers.image.base.name="nvcr.io/nvidia/distroless/cc"
LABEL org.opencontainers.image.created="${BUILD_TIMESTAMP}"
LABEL org.opencontainers.image.documentation="https://docs.nvidia.com/datacenter/cloud-native/gpu-operator"
LABEL org.opencontainers.image.revision="${GIT_COMMIT_SHA_FULL}"
LABEL org.opencontainers.image.source="https://github.com/NVIDIA/gpu-operator.git"
LABEL org.opencontainers.image.title="NVIDIA GPU Operator"
LABEL org.opencontainers.image.url="https://catalog.ngc.nvidia.com/orgs/nvidia/containers/gpu-operator"
LABEL org.opencontainers.image.vendor="NVIDIA"
LABEL org.opencontainers.image.version="${VERSION}"

WORKDIR /

COPY --from=shell /busybox /busybox
USER 0:0
RUN ["/busybox/ln", "-s", "/busybox/sh", "/bin/sh"]
ENV PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/busybox

COPY --from=builder /workspace/gpu-operator /usr/bin/
COPY --from=builder /workspace/manage-crds /usr/bin/
COPY --from=builder /workspace/cleanup-gpuclusters /usr/bin/
COPY --from=builder /workspace/nvidia-validator /usr/bin/
COPY --from=sample-builder /build/vectorAdd /usr/bin/vectorAdd
ARG CUDA_SAMPLES_VERSION
COPY --from=sample-builder /usr/local/cuda-${CUDA_SAMPLES_VERSION}/compat /usr/local/cuda/compat

COPY assets /opt/gpu-operator/
COPY manifests /opt/gpu-operator/manifests
COPY validator/manifests /opt/validator/manifests

# Add CRD resource into the image for helm upgrades
COPY deployments/gpu-operator/crds/nvidia.com_clusterpolicies.yaml /opt/gpu-operator/nvidia.com_clusterpolicies.yaml
COPY deployments/gpu-operator/crds/nvidia.com_nvidiadrivers.yaml /opt/gpu-operator/nvidia.com_nvidiadrivers.yaml
COPY deployments/gpu-operator/crds/nvidia.com_gpuclusters.yaml /opt/gpu-operator/nvidia.com_gpuclusters.yaml
COPY deployments/gpu-operator/charts/node-feature-discovery/crds/nfd-api-crds.yaml /opt/gpu-operator/nfd-api-crds.yaml

USER 1000:1000

COPY LICENSE /licenses/

ENTRYPOINT ["/usr/bin/gpu-operator"]
