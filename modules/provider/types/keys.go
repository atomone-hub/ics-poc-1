package types

import (
	"fmt"

	"cosmossdk.io/collections"
)

const (
	// ModuleName defines the module name
	ModuleName = "provider"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// GovModuleName duplicates the gov module's name to avoid a dependency with x/gov.
	// It should be synced with the gov module's name if it is ever changed.
	// See: https://github.com/cosmos/cosmos-sdk/blob/v0.52.0-beta.2/x/gov/types/keys.go#L9
	GovModuleName = "gov"

	// ConsumerModuleAccountPrefix is the prefix for consumer module accounts
	ConsumerModuleAccountPrefix = "consumer_"
)

// ParamsKey is the prefix to retrieve all Params
var ParamsKey = collections.NewPrefix("p_provider")

// ConsumerChainsKey is the prefix to retrieve all ConsumerChains
var ConsumerChainsKey = collections.NewPrefix("consumer_chains")

// GetConsumerModuleAccountName returns the module account name for a consumer chain
func GetConsumerModuleAccountName(chainID string) string {
	return fmt.Sprintf("%s%s", ConsumerModuleAccountPrefix, chainID)
}
