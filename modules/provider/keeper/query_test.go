package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/atomone-hub/ics-poc-1/modules/provider/keeper"
	"github.com/atomone-hub/ics-poc-1/modules/provider/types"
)

func TestParamsQuery(t *testing.T) {
	f := initFixture(t)

	qs := keeper.NewQueryServerImpl(f.keeper)
	params := types.DefaultParams()
	require.NoError(t, f.keeper.Params.Set(f.ctx, params))

	response, err := qs.Params(f.ctx, &types.QueryParamsRequest{})
	require.NoError(t, err)
	require.Equal(t, &types.QueryParamsResponse{Params: params}, response)
}

func TestActiveConsumerChainsQuery(t *testing.T) {
	f := initFixture(t)

	qs := keeper.NewQueryServerImpl(f.keeper)

	// Add some consumer chains with different statuses
	activeChain1 := types.NewConsumerChain(
		"consumer-1",
		"Consumer Chain 1",
		"v1.0.0",
		types.ConsumerStatus_CONSUMER_STATUS_ACTIVE,
		100,
		0,
		"cosmos1activeaddr1",
	)
	activeChain2 := types.NewConsumerChain(
		"consumer-2",
		"Consumer Chain 2",
		"v1.0.0",
		types.ConsumerStatus_CONSUMER_STATUS_ACTIVE,
		200,
		0,
		"cosmos1activeaddr2",
	)
	pendingChain := types.NewConsumerChain(
		"consumer-3",
		"Consumer Chain 3",
		"v1.0.0",
		types.ConsumerStatus_CONSUMER_STATUS_PENDING,
		300,
		0,
		"cosmos1pendingaddr",
	)
	sunsetChain := types.NewConsumerChain(
		"consumer-4",
		"Consumer Chain 4",
		"v1.0.0",
		types.ConsumerStatus_CONSUMER_STATUS_SUNSET,
		400,
		500,
		"cosmos1sunsetaddr",
	)

	// Store the chains
	require.NoError(t, f.keeper.ConsumerChains.Set(f.ctx, activeChain1.ChainId, activeChain1))
	require.NoError(t, f.keeper.ConsumerChains.Set(f.ctx, activeChain2.ChainId, activeChain2))
	require.NoError(t, f.keeper.ConsumerChains.Set(f.ctx, pendingChain.ChainId, pendingChain))
	require.NoError(t, f.keeper.ConsumerChains.Set(f.ctx, sunsetChain.ChainId, sunsetChain))

	response, err := qs.ActiveConsumerChains(f.ctx, &types.QueryActiveConsumerChainsRequest{})
	require.NoError(t, err)
	require.NotNil(t, response)

	require.Len(t, response.ConsumerChains, 2)

	// Verify the returned chains are active
	for _, chain := range response.ConsumerChains {
		require.True(t, chain.IsActive())
		require.Contains(t, []string{"consumer-1", "consumer-2"}, chain.ChainId)
	}
}
