package provider

import (
	"context"

	"github.com/cosmos/cosmos-sdk/telemetry"

	"github.com/atomone-hub/ics-poc-1/abci"
	"github.com/atomone-hub/ics-poc-1/modules/provider/keeper"
	"github.com/atomone-hub/ics-poc-1/modules/provider/types"
)

// TODO: ref https://github.com/atomone-hub/ics-poc-1/issues/6

// BeginBlocker substracts ICS costs per chains.
func BeginBlocker(ctx context.Context, k keeper.Keeper) error {
	defer telemetry.ModuleMeasureSince(types.ModuleName, telemetry.Now(), telemetry.MetricKeyBeginBlocker)

	// TODO calculate incentives and substract from consumers accounts

	return nil
}

func EndBlocker(ctx context.Context, k keeper.Keeper) error {

	// TODO: check for consumer chains statuses and ugprade to forward to consensus

	return nil
}

// PostFinalizeBlock is called after FinalizeBlock to aggregate consumer chain data
// and determine which chains should be active in the next block.
// This is similar to EndBlocker but receives aggregated app hashes from all consumer chains.
func PostFinalizeBlock(ctx context.Context, k keeper.Keeper, req abci.PostFinalizeBlockRequest) (abci.PostFinalizeBlockResponse, error) {
	defer telemetry.ModuleMeasureSince(types.ModuleName, telemetry.Now(), "post_finalize_blocker")

	// Log received consumer app hashes from previous block
	if len(req.ConsumerAppHashes) > 0 {
		// TODO: Store or process consumer app hashes for verification/incentives
		// For now, just log them
		for chainID, appHash := range req.ConsumerAppHashes {
			_ = chainID
			_ = appHash
			// k.Logger(ctx).Debug("Received consumer app hash",
			//	"chain_id", chainID,
			//	"app_hash", fmt.Sprintf("%X", appHash),
			//	"height", req.Height)
		}
	}

	// Get all consumer chains and determine which should be active
	consumers, err := k.GetAllConsumerChains(ctx)
	if err != nil {
		return abci.PostFinalizeBlockResponse{}, err
	}

	// Build list of active chains for the next block
	activeChains := make([]abci.ChainUpdate, 0, len(consumers))
	for _, consumer := range consumers {
		// Only include active consumer chains
		// Skip chains that are pending, sunset, or in other non-active states
		isActive := consumer.IsActive()

		// TODO: Add additional logic to determine if chain should be active:
		// - Check if chain account has sufficient balance for incentives
		// - Check for any upgrade or maintenance modes

		// Check if chain has been sunset (SunsetAt height reached)
		if consumer.SunsetAt > 0 && req.Height >= consumer.SunsetAt {
			isActive = false
		}

		chainUpdate := abci.ChainUpdate{
			ChainId: consumer.ChainId,
			Active:  isActive,
			Metadata: map[string]string{
				"status":  consumer.Status.String(),
				"name":    consumer.Name,
				"version": consumer.Version,
			},
		}

		activeChains = append(activeChains, chainUpdate)
	}

	return abci.PostFinalizeBlockResponse{
		ActiveChains: activeChains,
	}, nil
}
