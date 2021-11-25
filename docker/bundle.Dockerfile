FROM scratch

ARG VERSION=""
ARG DEFAULT_CHANNEL=stable
ARG CHANNELS=stable

LABEL operators.operatorframework.io.bundle.mediatype.v1=registry+v1
LABEL operators.operatorframework.io.bundle.manifests.v1=manifests/
LABEL operators.operatorframework.io.bundle.metadata.v1=metadata/
LABEL operators.operatorframework.io.bundle.package.v1=gpu-operator-certified
LABEL operators.operatorframework.io.bundle.channels.v1=${CHANNELS}
LABEL operators.operatorframework.io.bundle.channel.default.v1=${DEFAULT_CHANNEL}
LABEL operators.operatorframework.io.metrics.builder=operator-sdk-v1.4.0
LABEL operators.operatorframework.io.metrics.mediatype.v1=metrics+v1
LABEL operators.operatorframework.io.metrics.project_layout=go.kubebuilder.io/v3
LABEL operators.operatorframework.io.test.config.v1=tests/scorecard/
LABEL operators.operatorframework.io.test.mediatype.v1=scorecard+v1

LABEL com.redhat.openshift.versions="v4.9"
LABEL com.redhat.delivery.operator.bundle=true
LABEL com.redhat.delivery.backport=false

COPY bundle/${VERSION}/manifests /manifests/
COPY bundle/${VERSION}/metadata /metadata/
COPY bundle/tests/scorecard /tests/scorecard/
