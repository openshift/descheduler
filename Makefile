all: build
.PHONY: all

GO=GO111MODULE=on GOFLAGS=-mod=vendor go

# Version to use when bumping Dockerfile versions
VERSION ?= 5.0.4

# Include the library makefile
include $(addprefix ./vendor/github.com/openshift/build-machinery-go/make/, \
	golang.mk \
	targets/openshift/images.mk \
)

# Exclude e2e tests from unit testing
GO_TEST_PACKAGES :=./pkg/... ./cmd/...
GO_BUILD_FLAGS :=-tags strictfipsruntime
GO_LD_EXTRAFLAGS :=-X sigs.k8s.io/descheduler/pkg/version.version=v20240119-v0.28.0
IMAGE_REGISTRY :=registry.svc.ci.openshift.org

# This will call a macro called "build-image" which will generate image specific targets based on the parameters:
# $0 - macro name
# $1 - target name
# $2 - image ref
# $3 - Dockerfile path
# $4 - context directory for image build
$(call build-image,descheduler,$(CI_IMAGE_REGISTRY)/ocp/4.12:descheduler,./images/ci/Dockerfile,.)

test-e2e: GO_TEST_PACKAGES :=./test/e2e
test-e2e: GO_TEST_FLAGS :=-v
test-e2e: test-unit
.PHONY: test-e2e

update-version:
	./hack/update-version.sh $(VERSION)
.PHONY: update-version

verify-version:
	./hack/verify-version.sh $(VERSION)
.PHONY: verify-version

verify: verify-version
.PHONY: verify

clean:
	$(RM) -r ./apiserver.local.config
	$(RM) -r ./_output
.PHONY: clean
