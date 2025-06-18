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

ARG CUDA_SAMPLE_IMAGE=undefined
ARG GOLANG_VERSION=x.x.x

FROM nvcr.io/nvidia/cuda:12.9.0-base-ubi9 as builder

RUN dnf install -y wget make git gcc

ARG GOLANG_VERSION=0.0.0
RUN set -eux; \
    \
    arch="$(uname -m)"; \
    case "${arch##*-}" in \
        x86_64 | amd64) ARCH='amd64' ;; \
        ppc64el | ppc64le) ARCH='ppc64le' ;; \
        aarch64 | arm64) ARCH='arm64' ;; \
        *) echo "unsupported architecture" ; exit 1 ;; \
    esac; \
    wget -nv -O - https://storage.googleapis.com/golang/go${GOLANG_VERSION}.linux-${ARCH}.tar.gz \
    | tar -C /usr/local -xz

ENV GOPATH /go
ENV PATH $GOPATH/bin:/usr/local/go/bin:$PATH

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

FROM ${CUDA_SAMPLE_IMAGE} AS sample-builder

FROM nvcr.io/nvidia/cuda:12.9.0-base-ubi9

# Remove CUDA libs(compat etc) in favor of libs installed by the NVIDIA driver
RUN dnf remove -y cuda-*

RUN dnf install -y \
        kmod \
        pciutils && \
    rm -rf /var/cache/yum/*

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
COPY --from=builder /workspace/nvidia-validator /usr/bin/
COPY --from=sample-builder /cuda-samples/vectorAdd /usr/bin/vectorAdd

# gpu-operator manifests
RUN mkdir -p /opt/gpu-operator/manifests
COPY assets /opt/gpu-operator/
COPY manifests /opt/gpu-operator/manifests

# validator manifests
RUN mkdir -p /opt/validator/manifests
COPY validator/manifests/plugin-workload-validation.yaml /opt/validator/manifests
COPY validator/manifests/cuda-workload-validation.yaml /opt/validator/manifests

RUN mkdir /licenses && mv /NGC-DL-CONTAINER-LICENSE /licenses/NGC-DL-CONTAINER-LICENSE
COPY hack/must-gather.sh /usr/bin/gather

# Install must-gather dependency: `kubectl`
ARG TARGETARCH
RUN OS_ARCH=${TARGETARCH/x86_64/amd64} && OS_ARCH=${OS_ARCH/aarch64/arm64} && curl -LO https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/${OS_ARCH}/kubectl
RUN chmod +x ./kubectl
RUN mv ./kubectl /usr/local/bin

# Add CRD resource into the image for helm upgrades
COPY deployments/gpu-operator/crds/nvidia.com_clusterpolicies.yaml /opt/gpu-operator/nvidia.com_clusterpolicies.yaml
COPY deployments/gpu-operator/crds/nvidia.com_nvidiadrivers.yaml /opt/gpu-operator/nvidia.com_nvidiadrivers.yaml
COPY deployments/gpu-operator/charts/node-feature-discovery/crds/nfd-api-crds.yaml /opt/gpu-operator/nfd-api-crds.yaml

# Install / upgrade packages here that are required to resolve CVEs
ARG CVE_UPDATES
RUN if [ -n "${CVE_UPDATES}" ]; then \
        dnf update -y ${CVE_UPDATES} && \
        rm -rf /var/cache/yum/*; \
    fi

USER 65532:65532

ENTRYPOINT ["/usr/bin/gpu-operator"]
