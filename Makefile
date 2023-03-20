PATH := $(GOPATH)/bin:$(PATH)
EXAMPLES=./examples/bench/server ./examples/bench/client ./examples/ping ./examples/thrift ./examples/hyperbahn/echo-server
ALL_PKGS := $(shell go list ./... | grep -v 'thirdparty')
PROD_PKGS := . ./http ./hyperbahn ./json ./peers ./pprof ./raw ./relay ./stats ./thrift $(EXAMPLES)
TEST_ARG ?= -race -v -timeout 10m
COV_PKG ?= ./
BUILD := ./build
THRIFT_GEN_RELEASE := ./thrift-gen-release
THRIFT_GEN_RELEASE_LINUX := $(THRIFT_GEN_RELEASE)/linux-x86_64
THRIFT_GEN_RELEASE_DARWIN := $(THRIFT_GEN_RELEASE)/darwin-x86_64

PLATFORM := $(shell uname -s | tr '[:upper:]' '[:lower:]')
ARCH := $(shell uname -m)

BIN := $(shell pwd)/.bin

# Cross language test args
TEST_HOST=127.0.0.1
TEST_PORT=0

-include crossdock/rules.mk

all: lint test examples

$(BIN)/thrift:
	mkdir -p $(BIN)
	scripts/install-thrift.sh $(BIN)

packages_test:
	go list -json ./... | jq -r '. | select ((.TestGoFiles | length) > 0)  | .ImportPath'

setup:
	mkdir -p $(BUILD)
	mkdir -p $(BUILD)/examples
	mkdir -p $(THRIFT_GEN_RELEASE_LINUX)
	mkdir -p $(THRIFT_GEN_RELEASE_DARWIN)

install:
	go mod vendor

install_lint:
	@echo "Installing golint, since we expect to lint"
	go install golang.org/x/lint/golint@latest

install_ci: $(BIN)/thrift install
ifdef CROSSDOCK
	$(MAKE) install_docker_ci
endif

help:
	@egrep "^# target:" [Mm]akefile | sort -

clean:
	echo Cleaning build artifacts...
	go clean
	rm -rf $(BUILD) $(THRIFT_GEN_RELEASE)
	echo

fmt format:
	echo Formatting Packages...
	go fmt $(ALL_PKGS)
	echo

test_ci:
ifdef CROSSDOCK
	$(MAKE) crossdock_ci
else
	$(MAKE) test
endif

test: clean setup check_no_test_deps $(BIN)/thrift
	$(MAKE) test_vanilla
	$(MAKE) test_relay_frame_leaks

# test_vanilla runs all unit tests without checking for frame leaks
test_vanilla:
	@echo Testing packages:
	PATH=$(BIN):$$PATH DISABLE_FRAME_POOLING_CHECKS=1 go test -parallel=4 $(TEST_ARG) $(ALL_PKGS)
	@echo Running frame pool tests
	PATH=$(BIN):$$PATH go test -run TestFramesReleased -stressTest $(TEST_ARG)

# test_relay_frame_leaks runs unit tests in relay_test.go with frame leak checks enabled
test_relay_frame_leaks:
	@echo Testing relay frame leaks
	PATH=$(BIN):$$PATH go test -parallel=4 $(TEST_ARG) relay_test.go

check_no_test_deps:
	! go list -json $(PROD_PKGS) | jq -r '.Deps | select ((. | length) > 0) | .[]' | grep -e test -e mock | grep -v '^internal/testlog'

benchmark: clean setup $(BIN)/thrift
	echo Running benchmarks:
	PATH=$(BIN)::$$PATH go test $(ALL_PKGS) -bench=. -cpu=1 -benchmem -run NONE

cover_profile: clean setup $(BIN)/thrift
	@echo Testing packages:
	mkdir -p $(BUILD)
	PATH=$(BIN)::$$PATH DISABLE_FRAME_POOLING_CHECKS=1 go test $(COV_PKG) $(TEST_ARG) -coverprofile=$(BUILD)/coverage.out

cover: cover_profile
	go tool cover -html=$(BUILD)/coverage.out

cover_ci:
	@echo "Uploading coverage"
	$(MAKE) cover_profile
	curl -s https://codecov.io/bash > $(BUILD)/codecov.bash
	bash $(BUILD)/codecov.bash -f $(BUILD)/coverage.out


FILTER := grep -v -e '_string.go' -e '/gen-go/' -e '/mocks/' -e 'vendor/' -e 'thirdparty'
lint: install
	@echo "Running golint"
	-golint $(ALL_PKGS) | $(FILTER) | tee lint.log
	@echo "Running go vet"
	-go vet $(ALL_PKGS) 2>&1 | $(FILTER) | fgrep -v -e "possible formatting directiv" -e "exit status" | tee -a lint.log
	@echo "Verifying files are gofmt'd"
	-gofmt -l . | $(FILTER) | tee -a lint.log
	@echo "Checking for unresolved FIXMEs"
	-git grep -i -n fixme | $(FILTER) | grep -v -e Makefile | tee -a lint.log
	@[ ! -s lint.log ]

thrift_example: thrift_gen
	go build -o $(BUILD)/examples/thrift       ./examples/thrift/main.go

test_server:
	./build/examples/test_server --host ${TEST_HOST} --port ${TEST_PORT}

examples: clean setup thrift_example
	echo Building examples...
	mkdir -p $(BUILD)/examples/ping $(BUILD)/examples/bench
	go build -o $(BUILD)/examples/ping/pong    ./examples/ping/main.go
	go build -o $(BUILD)/examples/hyperbahn/echo-server    ./examples/hyperbahn/echo-server/main.go
	go build -o $(BUILD)/examples/bench/server ./examples/bench/server
	go build -o $(BUILD)/examples/bench/client ./examples/bench/client
	go build -o $(BUILD)/examples/bench/runner ./examples/bench/runner.go
	go build -o $(BUILD)/examples/test_server ./examples/test_server

thrift_gen: $(BIN)/thrift
	go build -o $(BUILD)/thrift-gen ./thrift/thrift-gen
	PATH=$(BIN):$$PATH $(BUILD)/thrift-gen --generateThrift --inputFile thrift/test.thrift --outputDir thrift/gen-go/
	PATH=$(BIN):$$PATH $(BUILD)/thrift-gen --generateThrift --inputFile examples/keyvalue/keyvalue.thrift --outputDir examples/keyvalue/gen-go
	PATH=$(BIN):$$PATH $(BUILD)/thrift-gen --generateThrift --inputFile examples/thrift/example.thrift --outputDir examples/thrift/gen-go
	PATH=$(BIN):$$PATH $(BUILD)/thrift-gen --generateThrift --inputFile hyperbahn/hyperbahn.thrift --outputDir hyperbahn/gen-go

release_thrift_gen: clean setup
	GOOS=linux GOARCH=amd64 go build -o $(THRIFT_GEN_RELEASE_LINUX)/thrift-gen ./thrift/thrift-gen
	GOOS=darwin GOARCH=amd64 go build -o $(THRIFT_GEN_RELEASE_DARWIN)/thrift-gen ./thrift/thrift-gen
	tar -czf thrift-gen-release.tar.gz $(THRIFT_GEN_RELEASE)
	mv thrift-gen-release.tar.gz $(THRIFT_GEN_RELEASE)/

.PHONY: all help clean fmt format install install_ci install_lint release_thrift_gen packages_test check_no_test_deps test test_ci lint
.SILENT: all help clean fmt format test lint
