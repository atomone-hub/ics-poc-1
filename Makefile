BUILDDIR ?= $(CURDIR)/build


BUILD_TARGETS := build install
build: BUILD_ARGS=-o $(BUILDDIR)/

$(BUILD_TARGETS): go.sum $(BUILDDIR)/
	go $@ -mod=readonly  $(BUILD_ARGS) ./...

$(BUILDDIR)/:
	mkdir -p $(BUILDDIR)/

.PHONY: build install

provider_home=~/.provider-localnet
providerd=./build/provider --home $(provider_home)

provider-start: build
	rm -rf $(provider_home)
	$(providerd) init localnet --default-denom uatone --chain-id provider-localnet
	$(providerd) config set client chain-id provider-localnet
	$(providerd) config set client keyring-backend test
	$(providerd) keys add val
	$(providerd) genesis add-genesis-account val 1000000000000uatone --chain-id provider-localnet
	$(providerd) keys add user
	$(providerd) genesis add-genesis-account user 1000000000uatone --chain-id provider-localnet
	$(providerd) genesis gentx val 1000000000uatone
	$(providerd) genesis collect-gentxs
	
	# Set validator gas prices
	sed -i.bak 's#^minimum-gas-prices = .*#minimum-gas-prices = "0.01uatone,0.01uphoton"#g' $(provider_home)/config/app.toml
	# enable REST API
	$(providerd) config set app api.enable true
	# Decrease voting period to 5min
	jq '.app_state.gov.params.voting_period = "300s"' $(provider_home)/config/genesis.json > /tmp/gen
	mv /tmp/gen $(provider_home)/config/genesis.json
	$(providerd) start

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
