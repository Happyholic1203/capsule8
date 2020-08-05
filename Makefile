# Go import path of this repo

PKG=github.com/Happyholic1203/capsule8
REPO=$(shell basename $(shell readlink -f .))

#
# SemVer 2.0 version string: (X.Y.Z-pre-release-identifier+build.metadata)
#
TAG=$(shell git describe --tags --abbrev=0 2>/dev/null)
SHA=$(shell git describe --match=NeVeRmAtCh --always --abbrev=7 --dirty)

ifeq ($(TAG),)
	VERSION=$(SHA)
else
	VERSION=$(TAG)+$(SHA)
endif

# Automated build unique identifier (if any)
BUILD=$(shell echo ${BUILD_ID})

# Allow DOCKER to be set on invocation of make to prefix "sudo", for example
DOCKER?=docker

# Allow GO_BUILD to be set on invocation of make to customize behavior
GO_BUILD?=go build
GO_BUILD_FLAGS+=-ldflags "-X $(PKG)/pkg/version.Version=$(VERSION) -X $(PKG)/pkg/version.Build=$(BUILD)"

GO_FMT?=gofmt
GO_FMT_FLAGS+=-l

GO_LINT?=golint -set_exit_status
GO_LINTFLAGS+=

# allow GO_TEST to be set on invocation of make to customize behavior
GO_TEST?=go test
GO_TEST_FLAGS+=

GO_VET?=go vet
GO_VET_FLAGS+=-all -shadow -shadowstrict -structtags=false

# Extra test execution flags (e.g. glog flags like -v=2)
TEST_FLAGS+=

# Extra 'docker run' flags
DOCKER_RUN_FLAGS+=

# Need to use clang instead of gcc for -msan, specify its path here
CLANG?=clang

# Needed to regenerate code from protos
GO_OUT=../
PROTO_INC=-I../:vendor/github.com/grpc-ecosystem/grpc-gateway/third_party/googleapis

CMDS=$(notdir $(wildcard ./cmd/*))
EXAMPLES=$(notdir $(wildcard ./examples/*))
BINS=$(patsubst %,bin/%,$(CMDS)) \
	$(patsubst %,bin/%,$(EXAMPLES)) \
	test/functional/functional.test

# All source directories that need to be checked, compiled, tested, etc.
SRC=./cmd/... ./pkg/... ./examples/... 
CHECK_SRC=$(SRC) ./test/...
FMT_SRC=$(patsubst %/...,%,$(CHECK_SRC))
TEST_SRC=./pkg/...
PKG_SOURCES=$(shell find pkg 2>&1 | grep -E '.*\.go$$')

#
# Docker flags to use for builder
#
DOCKER_RUN_BUILDER=$(DOCKER) run                                            \
	--network host                                                      \
	-ti                                                                 \
	--rm                                                                \
	-u $(shell id -u):$(shell getent group docker | cut -d: -f3)        \
	-v "$$(pwd):/go/src/$(PKG)"                                         \
	-v /var/run/docker.sock:/var/run/docker.sock:ro                     \
	-w /go/src/$(PKG)                                                   \
	$(BUILDER_IMAGE)

#
# Docker flags to use to run the capsule8 container
#
DOCKER_RUN=$(DOCKER) run                                                    \
	--privileged                                                        \
	$(DOCKER_RUN_FLAGS)                                                 \
	--name capsule8-sensor                                              \
	--rm                                                                \
	-v /proc:/var/run/capsule8/proc/:ro                                 \
	-v /sys/kernel/debug:/sys/kernel/debug                              \
	-v /sys/fs/cgroup:/sys/fs/cgroup                                    \
	-v /var/lib/docker:/var/lib/docker:ro                               \
	-v /var/run/capsule8:/var/run/capsule8                              \
	-v /var/run/docker:/var/run/docker:ro                               \
	$(CONTAINER_IMAGE)

#
# Docker flags to use to run the functional test container
#
DOCKER_RUN_FUNCTIONAL_TEST=$(DOCKER) run                                    \
	-v /var/run/capsule8:/var/run/capsule8                              \
	-v /var/run/docker.sock:/var/run/docker.sock                        \
	$(FUNCTIONAL_TEST_IMAGE)

.PHONY: all api builder run_builder container load save                     \
	run_sensor run_sensor_detach shell static dist check                \
	test test_verbose test_all test_msan test_race test_functional      \
	test_coverage functional_test run_functional_test clean deps fmt

#
# Default target: build all executables
#
all: $(BINS)

deps:
	dep ensure

fmt:
	$(GO_FMT) $(GO_FMT_FLAGS) -w $(FMT_SRC)

#
# Build all executables as static executables
#
static: GO_BUILD:=CGO_ENABLED=0 $(GO_BUILD)
static: GO_BUILD_FLAGS=-a
static: clean all

api: ../capsule8/api/v0/*.proto
        # Compile grpc and gateway stubs
	protoc --doc_out=../capsule8/docs/ --doc_opt=markdown,API.md:google/* \
		--go_out=plugins=grpc:$(GO_OUT) $(PROTO_INC) $?

#
# Build all container images
#
containers: builder container functional_test

builder: Dockerfile.builder
	$(DOCKER) build -f Dockerfile.builder .
	$(eval BUILDER_IMAGE=$(shell $(DOCKER) build -q -f Dockerfile.builder .))

# Run a shell within the builder container
run_builder: Dockerfile.builder builder_image
	$(DOCKER_RUN_BUILDER)

container: GO_BUILD:=CGO_ENABLED=0 $(GO_BUILD)
container: bin/sensor Dockerfile
	$(DOCKER) build --build-arg vcsref=$(SHA) --build-arg version=$(VERSION) .
	$(eval CONTAINER_IMAGE=$(shell $(DOCKER) build -q .))

load: capsule8-$(VERSION).tar
	$(DOCKER) load -i $<

save: capsule8-$(VERSION).tar

capsule8-$(VERSION).tar: container
	$(DOCKER) save -o $@ $(CONTAINER_IMAGE)

# Run sensor container in foreground
run_sensor: DOCKER_RUN_FLAGS+=-ti
run_sensor: container
	$(DOCKER_RUN)

# Run sensor container in background
run_sensor_detach: DOCKER_RUN_FLAGS+=-d
run_sensor_detach: container
	$(DOCKER_RUN)

# Build docker image for the functional test suite
functional_test: GO_TEST:=CGO_ENABLED=0 $(GO_TEST)
functional_test: test/functional/functional.test
	$(DOCKER) build ./test/functional
	$(eval FUNCTIONAL_TEST_IMAGE=$(shell $(DOCKER) build -q ./test/functional))

# Run docker image for the functional test suite
run_functional_test: functional_test
	$(DOCKER_RUN_FUNCTIONAL_TEST) $(TEST_FLAGS)

#
# Run an interactive shell within the docker container with the
# required ports and mounts. This is useful for debugging and testing
# the environment within the continer.
#
shell: DOCKER_RUN_FLAGS+=-ti
shell: container
	$(DOCKER_RUN) /bin/sh

#
# Make a binary distribution tarball
#
dist: static
	tar -czf capsule8-$(VERSION).tar.gz bin/ ./examples/ ./vendor/

#
# Pattern rules to allow 'make foo' to build ./cmd/foo or ./test/cmd/foo (whichever exists)
#
bin/% : cmd/% cmd/%/*.go $(PKG_SOURCES)
	$(GO_BUILD) $(GO_BUILD_FLAGS) -o $@ ./$<

bin/% : examples/% examples/%/*.go $(PKG_SOURCES)
	$(GO_BUILD) $(GO_BUILD_FLAGS) -o $@ ./$<

#
# Check that all sources build successfully, gofmt, go vet, golint, etc)
#
check:
	@echo "--- Checking that all sources build"
	$(GO_BUILD) $(SRC)
	@echo "--- Checking source code formatting"
	OUT=`$(GO_FMT) $(GO_FMT_FLAGS) $(FMT_SRC)` && (test -z "$$OUT" || (echo $$OUT && exit 1))
	@echo "--- Checking that all sources vet clean"
	$(GO_VET) $(GO_VET_FLAGS) $(CHECK_SRC)
	@echo "--- Checking sources for lint"
	$(GO_LINT) $(GO_LINT_FLAGS) $(CHECK_SRC)

#
# Run unit tests
#
test:
	$(GO_TEST) $(GO_TEST_FLAGS) -tags testing $(TEST_SRC) $(TEST_FLAGS)

test_coverage: GO_TEST_FLAGS+=-cover -coverprofile=coverage.out
test_coverage: test

test_verbose: GO_TEST_FLAGS+=-v
test_verbose: test

#
# Run all tests
#
test_all: test test_msan test_race

#
# Run all unit tests in pkg/ under memory sanitizer
#
test_msan: GO_TEST_FLAGS+=-msan
test_msan: GO_TEST:=CC=${CLANG} $(GO_TEST)
test_msan: test

#
# Run all unit tests in pkg/ under race detector
#
test_race: GO_TEST_FLAGS+=-race
test_race: test

#
# Run functional test suite (requires sensor to be already running)
#
test_functional:
	go test ./test/functional $(GO_TEST_FLAGS)

test/functional/functional.test: test/functional/*.go
	$(GO_TEST) $(GO_TEST_FLAGS) -c -o $@ ./test/functional

clean:
	rm -rf $(BINS)
	go clean $(CHECK_SRC)
