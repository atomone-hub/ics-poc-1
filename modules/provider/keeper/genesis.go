package keeper

import (
	"context"

	"github.com/atomone-hub/ics-poc-1/modules/provider/types"
)

// InitGenesis initializes the module's state from a provided genesis state.
func (k Keeper) InitGenesis(ctx context.Context, genState types.GenesisState) error {
	if err := k.Params.Set(ctx, genState.Params); err != nil {
		return err
	}

	// Initialize consumer chains
	for _, consumer := range genState.ConsumerChains {
		if err := k.ConsumerChains.Set(ctx, consumer.ChainId, consumer); err != nil {
			return err
		}
	}

	return nil
}

// ExportGenesis returns the module's exported genesis.
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	var err error

	genesis := types.DefaultGenesis()
	genesis.Params, err = k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	// Export consumer chains
	genesis.ConsumerChains = []types.ConsumerChain{}
	err = k.ConsumerChains.Walk(ctx, nil, func(key string, value types.ConsumerChain) (stop bool, err error) {
		genesis.ConsumerChains = append(genesis.ConsumerChains, value)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return genesis, nil
}
