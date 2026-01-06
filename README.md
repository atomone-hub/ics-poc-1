# ICS PoC no.1

A minimal implementation for multiplexing multiple blockchain applications
through a single CometBFT consensus layer.

## Transaction format

Format: ICS:<chain_id>:<payload>

Example: ICS:chain-1:{"type":"send","amount":"100"}

The multiplexer strips the "ICS:<chain_id>:" prefix and forwards the
payload to the appropriate chain application.

## Config

```toml
    log_level = "info"

    [[apps]]
    chain_id = "chain-1"
    address = "unix:///tmp/chain1.sock"
    connection_type = "socket"
    home = "/tmp/chain1"

    [[apps]]
    chain_id = "chain-2"
    address = "unix:///tmp/chain2.sock"
    connection_type = "socket"
    home = "/tmp/chain2"
```

## Queries

Queries must specify the target chain via chain_id parameter:

```sh
curl "http://localhost:26657/abci_query?path=/store/key&chain_id=chain-1"
```

## Architecture

```txt
    CometBFT (consensus)
         |
         | ABCI
         |
    Multiplexer (routes by chain_id)
         |
         +-- chain-1 (ABCI app)
         |
         +-- chain-2 (ABCI app)
         |
         +-- chain-N (ABCI app)
```

The multiplexer implements the ABCI Application interface and forwards
requests to the appropriate chain based on the transaction header or
query chain_id parameter.
