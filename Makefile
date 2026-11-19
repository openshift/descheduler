all: build
.PHONY: all

GO=GO111MODULE=on GOFLAGS=-mod=vendor go

# Version to use when bumping Dockerfile versions
VERSION ?= 5.4.3

# Include the library makefile
include $(addprefix ./vendor/github.com/openshift/build-machinery-go/make/, \
	golang.mk \
)

# Exclude e2e tests from unit testing
GO_TEST_PACKAGES :=./pkg/... ./cmd/...
GO_BUILD_FLAGS :=-tags strictfipsruntime
GO_LD_EXTRAFLAGS :=-X sigs.k8s.io/descheduler/pkg/version.version=v20260520-v0.36.0
# IMAGE is the local image name of descheduler
IMAGE := descheduler:$(VERSION)

# CONTAINER_ENGINE is the container engine used to build images locally
CONTAINER_ENGINE ?= docker

# image builds the descheduler image from the root Dockerfile.
image:
	$(CONTAINER_ENGINE) build -t $(IMAGE) .
.PHONY: image

# dev-image builds a static development image from Dockerfile.dev for kind-based tests.
dev-image:
	CGO_ENABLED=0 $(GO) build -o _output/bin/descheduler sigs.k8s.io/descheduler/cmd/descheduler
	$(CONTAINER_ENGINE) build -f Dockerfile.dev -t $(IMAGE) .
.PHONY: dev-image

# kind-multi-node creates a multi node kind cluster used by the helm chart tests.
kind-multi-node:
	kind create cluster --name kind --config ./hack/kind_config.yaml --wait 2m
.PHONY: kind-multi-node

test-e2e:
	./test/run-e2e-tests.sh
.PHONY: test-e2e

update-version:
	./hack/update-version.sh $(VERSION)
.PHONY: update-version

verify-version:
	./hack/verify-version.sh $(VERSION)
.PHONY: verify-version

# Extend the verify target from vendor/github.com/openshift/build-machinery-go
verify: verify-version

clean:
	$(RM) -r ./apiserver.local.config
	$(RM) -r ./_output
.PHONY: clean
