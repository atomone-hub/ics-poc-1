package provider

import (
	"context"
	"fmt"

	"github.com/cosmos/cosmos-sdk/telemetry"

	"github.com/atomone-hub/ics-poc-1/modules/provider/keeper"
	"github.com/atomone-hub/ics-poc-1/modules/provider/types"
)

// TODO: ref https://github.com/atomone-hub/ics-poc-1/issues/6

// BeginBlocker substracts ICS costs per chains.
func BeginBlocker(ctx context.Context, k keeper.Keeper) error {
	defer telemetry.ModuleMeasureSince(types.ModuleName, telemetry.Now(), telemetry.MetricKeyBeginBlocker)

	// Get params
	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to get params: %w", err)
	}

	// Skip if fees per block is zero
	if params.FeesPerBlock.IsZero() {
		return nil
	}

	// Collect fees from all active consumer chains
	totalFeesCollected, err := k.CollectFeesFromConsumers(ctx, params.FeesPerBlock)
	if err != nil {
		return err
	}

	// If no fees were collected, return early
	if totalFeesCollected.IsZero() {
		return nil
	}

	// Distribute collected fees to validators
	if err := k.DistributeFeesToValidators(ctx, totalFeesCollected); err != nil {
		return fmt.Errorf("failed to distribute fees to validators: %w", err)
	}

	return nil
}

func EndBlocker(ctx context.Context, k keeper.Keeper) error {

	// TODO: check for consumer chains statuses and ugprade to forward to consensus

	return nil
}
