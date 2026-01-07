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

## Consumer chain requirements

1. The following modules should not be used on a consumer chain:
   - x/upgrade: Upgrade handling is done via the provider chain governance to simplify the life of validators. Additionally, the x/upgrade module rely on the consensus params app version, that is shared with the provider chain. The module cannot effectively work.
   - x/evidence: Evidence handling is done via the provider chain, as a mishevior on the consumer chain cannot happen without a misbehavior on the provider chain. (ref: https://github.com/atomone-hub/ics-poc-1/issues/2)
   - x/slashing: Slashing is handled via the provider chain for the same reason as x/evidence.
2. Small modification of the following modules should be used on a consumer chain:
   - x/consensus: A wrapper for consumer chains to only be able to query consensus params.
   - x/staking: A wrapper for consumer chains to not handle validator set changes and slashing.
