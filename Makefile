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

.PHONY: test test-short lint-fix
