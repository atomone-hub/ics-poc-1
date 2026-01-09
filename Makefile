BUILDDIR ?= $(CURDIR)/build
SIMAPP = ./simapp
CURRENT_DIR = $(shell pwd)

BRANCH := $(shell git rev-parse --abbrev-ref HEAD)
COMMIT := $(shell git log -1 --format='%H')
# don't override user values
ifeq (,$(VERSION))
  VERSION := $(shell git describe --exact-match 2>/dev/null)
  # if VERSION is empty, then populate it with branch's name and raw commit hash
  ifeq (,$(VERSION))
    VERSION := $(BRANCH)-$(COMMIT)
  endif
endif

sharedFlags = -X github.com/cosmos/cosmos-sdk/version.Version=$(VERSION) \
		  -X github.com/cosmos/cosmos-sdk/version.Commit=$(COMMIT)

providerFlags := $(sharedFlags) -X github.com/cosmos/cosmos-sdk/version.AppName=ics-producer -X github.com/cosmos/cosmos-sdk/version.Name=ics-producer

BUILD_TARGETS := build install
build: BUILD_ARGS=-o $(BUILDDIR)/

$(BUILD_TARGETS): go.sum $(BUILDDIR)/
	go $@ -mod=readonly -ldflags "$(providerFlags)"  $(BUILD_ARGS) ./...

$(BUILDDIR)/:
	mkdir -p $(BUILDDIR)/

.PHONY: build install

# Run unit tests
test:
	go test ./... -v

test-short:
	go test ./... -v -short

# lint code with golangci-lint
lint-fix:
	golangci-lint run --fix

proto-gen:
	@echo "--> Generating Protobuf files"
	buf generate --path="./proto/ics" --template="./proto/buf.gen.yaml" --config="buf.yaml"
	mv modules/ics/provider/v1/* modules/provider/types; mv modules/ics/provider/module/v1/* modules/provider/types; rm -r modules/ics

.PHONY: test test-short lint-fix proto-gen
