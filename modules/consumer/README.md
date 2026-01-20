# modules/consumer

Consumer chains are **not** required to use Atom One SDK and its Tendermint fork.
They may continue to use their own ABCI compatible SDK versions, while being secured by Atom one.

This folder contains a set of module consumer chains are required to use or should use for a better experience.

## Required

### To use

- modules/consumer/staking: A wrapper of Cosmos SDK x/staking for consumer chains to not handle validator set changes and slashing.

### To not use

- x/upgrade: Upgrade handling is done via the provider chain governance to simplify the life of validators. Additionally, the x/upgrade module rely on the consensus params app version, that is shared with the provider chain. The module cannot effectively work.
- x/evidence: Evidence handling is done via the provider chain, as a mishevior on the consumer chain cannot happen without a misbehavior on the provider chain. (ref: https://github.com/atomone-hub/ics-poc-1/issues/2)
- x/slashing: Slashing is handled via the provider chain for the same reason as x/evidence.
- x/consensus: The chain inherits the consensus from the provider, the x/consensus module is not required.

## Recommended

- TBD.
  - modules/consumer/incentives: Automatically fund provider chain module account.
