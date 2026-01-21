# End-to-End Testing Infrastructure

This test suite creates two blockchain chains: a provider chain with two validators and a consumer chain with one validator. Each chain runs in its own Docker container, and the containers share the same network.

## Architecture Overview

During the setup of the end-to-end infrastructure:

1. **Consumer chain initialization**: The consumer chain launches first and waits for the provider chain to connect
2. **Provider chain startup**: The provider chain starts and establishes a connection to the consumer chain
3. **Test execution**: Once initialization is complete, the tests defined in `e2e_test.go` are executed

## Prerequisites

Before running the end-to-end tests, you must build the required Docker containers:

```bash
make docker-build
```

## Running the Tests

Execute the test suite using:

```bash
make test-e2e
```

