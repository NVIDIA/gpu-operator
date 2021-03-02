# Build the manager binary
FROM golang:1.15 as builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY main.go main.go
COPY api/ api/
COPY controllers/ controllers/

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go build -a -o gpu-operator main.go

FROM nvidia/cuda:11.2.1-base-ubi8

ENV NVIDIA_DISABLE_REQUIRE="true"
ENV NVIDIA_VISIBLE_DEVICES=all
ENV NVIDIA_DRIVER_CAPABILITIES=utility

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

RUN useradd gpu-operator
USER gpu-operator

ENTRYPOINT ["/usr/bin/gpu-operator"]