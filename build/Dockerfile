FROM alpine:3.8

RUN apk upgrade --update --no-cache

USER nobody

ADD build/_output/bin/special-resource-operator /usr/local/bin/special-resource-operator
