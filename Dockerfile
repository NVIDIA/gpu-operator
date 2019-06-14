FROM registry.svc.ci.openshift.org/openshift/release:golang-1.10 AS builder
WORKDIR /go/src/github.com/NVIDIA/gpu-operator
COPY . .
RUN make build

FROM registry.svc.ci.openshift.org/openshift/origin-v4.0:base
COPY --from=builder /go/src/github.com/NVIDIA/gpu-operator/gpu-operator /usr/bin/

RUN mkdir -p /opt/sro
COPY assets/ /opt/sro

RUN useradd gpu-operator
USER gpu-operator
ENTRYPOINT ["/usr/bin/gpu-operator"]
LABEL io.k8s.display-name="NVIDIA GPU Operator"
