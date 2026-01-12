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

.PHONY: test test-short lint-fix
