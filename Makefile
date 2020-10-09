.PHONY: all build devel image verify fmt lint test clean
.DEFAULT_GOAL := all


##### Global definitions #####

export MAKE_DIR ?= $(CURDIR)/mk

include $(MAKE_DIR)/common.mk


##### Global variables #####

DOCKERFILE       ?= $(CURDIR)/docker/Dockerfile.ubi8.prod
DOCKERFILE_DEVEL ?= $(CURDIR)/docker/Dockerfile.devel

BIN_NAME  ?= gpu-operator
IMAGE     ?= nvidia/gpu-operator
VERSION   ?= 1.2.0
TAG       ?= latest
TAG_DEVEL ?= devel


##### File definitions #####

PACKAGE      := github.com/NVIDIA/gpu-operator
MAIN_PACKAGE := $(PACKAGE)/cmd/manager
BINDATA      := $(PACKAGE)/pkg/manifests/bindata.go


##### Flags definitions #####

CGO_ENABLED  := 0
GOOS         := linux


##### Public rules #####

all: build verify
verify: fmt lint test vet assign

build:
	GOOS=$(GOOS) CGO_ENABLED=$(CGO_ENABLED) go build -o $(BIN_NAME) $(MAIN_PACKAGE)

fmt:
	find . -not \( \( -wholename './.*' -o -wholename '*/vendor/*' \) -prune \) -name '*.go' \
		| sort -u | xargs gofmt -s -l -d > fmt.out
	if [ -s fmt.out ]; then cat fmt.out; rm fmt.out; exit 1; else rm fmt.out; fi

lint:
	find . -not \( \( -wholename './.*' -o -wholename '*/vendor/*' \) -prune \) -name '*.go' \
		| sort -u | xargs golint -set_exit_status

vet:
	go vet $(PACKAGE)/...

test:
	go test $(PACKAGE)/cmd/... $(PACKAGE)/pkg/... -coverprofile cover.out

assign:
	find . -not \( \( -wholename './.*' -o -wholename '*/vendor/*' \) -prune \) -name '*.go' \
		| sort -u | xargs ineffassign

clean:
	go clean
	rm -f $(BIN)

prod-image:
	$(DOCKER) build --build-arg VERSION=$(VERSION) -t $(IMAGE):$(TAG) -f $(DOCKERFILE) .

devel-image:
	$(DOCKER) build -t $(IMAGE):$(TAG_DEVEL) -f $(DOCKERFILE_DEVEL) .
