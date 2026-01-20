BUILDDIR ?= $(CURDIR)/build

BUILD_TARGETS := build install
build: BUILD_ARGS=-o $(BUILDDIR)/

$(BUILD_TARGETS): go.sum $(BUILDDIR)/
	cd testapp; go $@ -mod=readonly  $(BUILD_ARGS) ./...

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
	# Add consumer chain as active in provider genesis
	jq '.app_state.provider.consumer_chains = [{"chain_id": "consumer-localnet", "name": "Consumer Localnet", "version": "1.0.0", "status": 2, "added_at": 0, "sunset_at": 0, "module_account_address": ""}]' $(provider_home)/config/genesis.json > /tmp/gen
	mv /tmp/gen $(provider_home)/config/genesis.json
	printf "[[chains]]\nchain_id = \"consumer-localnet\"\ngrpc_address = \"tcp://localhost:26658\"\nhome = \"$(HOME)/.consumer-localnet\"\n" > $(provider_home)/config/ics.toml
	$(providerd) start --rpc.grpc_laddr tcp://127.0.0.1:36658

consumer_home=~/.consumer-localnet
consumerd=./build/consumer --home $(consumer_home)

consumer-start: build
	rm -rf $(consumer_home)
	$(consumerd) init localnet --default-denom uatone --chain-id consumer-localnet
	$(consumerd) config set client chain-id consumer-localnet
	$(consumerd) config set client keyring-backend test
	$(consumerd) keys add val
	$(consumerd) genesis add-genesis-account val 1000000000000uatone --chain-id consumer-localnet
	$(consumerd) keys add user
	$(consumerd) genesis add-genesis-account user 1000000000uatone --chain-id consumer-localnet
	$(consumerd) genesis gentx val 1000000000uatone # todo this should not be needed
	$(consumerd) genesis collect-gentxs

	# Set validator gas prices
	sed -i.bak 's#^minimum-gas-prices = .*#minimum-gas-prices = "0.01uatone,0.01uphoton"#g' $(consumer_home)/config/app.toml
	# disable REST API
	$(consumerd) config set app api.enable false
	# Decrease voting period to 5min
	jq '.app_state.gov.params.voting_period = "300s"' $(consumer_home)/config/genesis.json > /tmp/gen
	mv /tmp/gen $(consumer_home)/config/genesis.json
	$(consumerd) start --with-comet=false --transport=grpc --rpc.grpc_laddr tcp://127.0.0.1:36659 --grpc.enable=false --log_level "debug"

# Run unit tests
test:
	go test ./... -v

test-short:
	go test ./... -v -short

# lint code with golangci-lint
lint-fix:
	golangci-lint run --fix

# generate mocks
mocks:
	@go install go.uber.org/mock/mockgen@v0.6.0
	sh ./scripts/mockgen.sh
.PHONY: mocks

proto-gen:
	@echo "--> Generating Protobuf files"
	buf generate --path="./proto/ics" --template="./proto/buf.gen.yaml" --config="buf.yaml"
	mv modules/ics/provider/v1/* modules/provider/types; mv modules/ics/provider/module/v1/* modules/provider/types; rm -r modules/ics

.PHONY: mocks test test-short lint-fix proto-gen
