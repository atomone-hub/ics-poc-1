package provider

import (
	"context"

	"github.com/cosmos/cosmos-sdk/telemetry"

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
