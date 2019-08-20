.PHONY: all build devel image verify fmt lint test clean
.DEFAULT_GOAL := all


##### Global definitions #####

export MAKE_DIR ?= $(CURDIR)/mk

include $(MAKE_DIR)/common.mk


##### Global variables #####

DOCKERFILE   ?= $(CURDIR)/docker/ubuntu/Dockerfile.ubuntu18.04
DEBUG_DOCKERFILE ?= $(CURDIR)/docker/debug/Dockerfile.debug
DOCKERDEVEL  ?= $(CURDIR)/docker/builder.Dockerfile
BIN_NAME     ?= gpu-operator
IMAGE        ?= nvidia/gpu-operator:latest
BUILDER      ?= nvidia/gpu-operator:builder


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

devel:
	$(DOCKER) build -t $(BUILDER) -f $(DOCKERDEVEL) .
	@echo $(DOCKER) run -it $v $(CURDIR):/go/src/$(PACKAGE) $(BUILDER):devel bash

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
	go vet ./...

test:
	go test $(PACKAGES)/cmd/... $(PACKAGE)/pkg/... -coverprofile cover.out

assign:
	find . -not \( \( -wholename './.*' -o -wholename '*/vendor/*' \) -prune \) -name '*.go' \
		| sort -u | xargs ineffassign

clean:
	go clean
	rm -f $(BIN)

image:
	$(DOCKER) build -t $(IMAGE) -f $(DOCKERFILE) .

debug_image:
	$(DOCKER) build -t $(IMAGE) -f $(DEBUG_DOCKERFILE) .
