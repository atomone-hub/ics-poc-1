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

localnet_home=~/.provider-localnet
localnetd=./build/provider --home $(localnet_home)

localnet-start: build
	rm -rf $(localnet_home)
	$(localnetd) init localnet --default-denom uatone --chain-id localnet
	$(localnetd) config set client chain-id localnet
	$(localnetd) config set client keyring-backend test
	$(localnetd) keys add val
	$(localnetd) genesis add-genesis-account val 1000000000000uatone,1000000000uphoton --chain-id localnet
	$(localnetd) keys add user
	$(localnetd) genesis add-genesis-account user 1000000000uatone,1000000000uphoton --chain-id localnet
	$(localnetd) genesis gentx val 1000000000uatone
	$(localnetd) genesis collect-gentxs
	# Add treasury DAO address
	#$(localnetd) genesis add-genesis-account atone1qqqqqqqqqqqqqqqqqqqqqqqqqqqqp0dqtalx52 5388766663072uatone --chain-id localnet
	# Add CP funds
	#$(localnetd) genesis add-genesis-account atone1jv65s3grqf6v6jl3dp4t6c9t9rk99cd8flcml8 5388766663072uatone --chain-id localnet
	#jq '.app_state.distribution.fee_pool.community_pool = [ { "denom": "uatone", "amount": "5388766663072.000000000000000000" }]' $(localnet_home)/config/genesis.json > /tmp/gen
	#mv /tmp/gen $(localnet_home)/config/genesis.json
	# Previous add-genesis-account call added the auth module account as a BaseAccount, we need to remove it
	# Set validator gas prices
	sed -i.bak 's#^minimum-gas-prices = .*#minimum-gas-prices = "0.01uatone,0.01uphoton"#g' $(localnet_home)/config/app.toml
	# enable REST API
	$(localnetd) config set app api.enable true
	# Decrease voting period to 5min
	jq '.app_state.gov.params.voting_period = "300s"' $(localnet_home)/config/genesis.json > /tmp/gen
	mv /tmp/gen $(localnet_home)/config/genesis.json
	$(localnetd) start
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
