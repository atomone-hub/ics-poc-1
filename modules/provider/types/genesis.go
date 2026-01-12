package types

// DefaultGenesis returns the default genesis state
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:         DefaultParams(),
		ConsumerChains: []ConsumerChain{},
	}
}

// Validate performs basic genesis state validation returning an error upon any
// failure.
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}

	// Validate consumer chains
	chainIDs := make(map[string]bool)

	for _, consumer := range gs.ConsumerChains {
		// Check for duplicate chain IDs
		if chainIDs[consumer.ChainId] {
			return ErrConsumerAlreadyExists
		}
		chainIDs[consumer.ChainId] = true

		// Validate consumer fields
		if consumer.ChainId == "" {
			return ErrInvalidChainID
		}
		if consumer.AddedAt < 0 {
			return ErrInvalidBlockHeight
		}
		if consumer.SunsetAt < 0 {
			return ErrInvalidBlockHeight
		}
	}

	return nil
}
