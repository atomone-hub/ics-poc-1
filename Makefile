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
