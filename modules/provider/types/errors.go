package types

// DONTCOVER

import (
	"cosmossdk.io/errors"
)

// x/provider module sentinel errors
var (
	ErrInvalidSigner         = errors.Register(ModuleName, 1100, "expected gov account as only signer for proposal message")
	ErrConsumerAlreadyExists = errors.Register(ModuleName, 1101, "consumer chain already exists")
	ErrConsumerNotFound      = errors.Register(ModuleName, 1102, "consumer chain not found")
	ErrInvalidChainID        = errors.Register(ModuleName, 1104, "invalid chain ID")
	ErrInvalidBlockHeight    = errors.Register(ModuleName, 1105, "invalid block height")
	ErrInvalidConsumerStatus = errors.Register(ModuleName, 1106, "invalid consumer status")
	ErrConsumerNotActive     = errors.Register(ModuleName, 1107, "consumer chain is not active")
	ErrConsumerAlreadySunset = errors.Register(ModuleName, 1108, "consumer chain is already sunset or removed")
)
