package keeper

import (
	"bytes"
	"context"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	"github.com/atomone-hub/ics-poc-1/modules/provider/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type msgServer struct {
	Keeper
}

// NewMsgServerImpl returns an implementation of the MsgServer interface
// for the provided Keeper.
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{Keeper: keeper}
}

var _ types.MsgServer = msgServer{}

func (k msgServer) UpdateParams(ctx context.Context, req *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	authority, err := k.addressCodec.StringToBytes(req.Authority)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	if !bytes.Equal(k.GetAuthority(), authority) {
		expectedAuthorityStr, _ := k.addressCodec.BytesToString(k.GetAuthority())
		return nil, errorsmod.Wrapf(types.ErrInvalidSigner, "invalid authority; expected %s, got %s", expectedAuthorityStr, req.Authority)
	}

	if err := req.Params.Validate(); err != nil {
		return nil, err
	}

	if err := k.Params.Set(ctx, req.Params); err != nil {
		return nil, err
	}

	return &types.MsgUpdateParamsResponse{}, nil
}

func (k msgServer) AddConsumer(ctx context.Context, req *types.MsgAddConsumer) (*types.MsgAddConsumerResponse, error) {
	authority, err := k.addressCodec.StringToBytes(req.Authority)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	if !bytes.Equal(k.GetAuthority(), authority) {
		expectedAuthorityStr, _ := k.addressCodec.BytesToString(k.GetAuthority())
		return nil, errorsmod.Wrapf(types.ErrInvalidSigner, "invalid authority; expected %s, got %s", expectedAuthorityStr, req.Authority)
	}

	// Validate input
	if req.ChainId == "" {
		return nil, types.ErrInvalidChainID
	}
	if req.BlockHeight < 0 {
		return nil, types.ErrInvalidBlockHeight
	}

	// Check if consumer already exists
	exists, err := k.ConsumerChains.Has(ctx, req.ChainId)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to check consumer existence")
	}
	if exists {
		return nil, types.ErrConsumerAlreadyExists
	}

	// Get current block height
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentHeight := sdkCtx.BlockHeight()

	if req.BlockHeight <= currentHeight {
		return nil, errorsmod.Wrapf(types.ErrInvalidBlockHeight, "block height must be greater than current height %d", currentHeight)
	}

	// Create module account for this consumer
	moduleAccountAddr, err := k.CreateConsumerModuleAccount(ctx, req.ChainId)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to create module account")
	}

	// Create consumer chain
	consumer := types.ConsumerChain{
		ChainId:              req.ChainId,
		Name:                 req.Name,
		Version:              req.Version,
		Status:               types.ConsumerStatus_CONSUMER_STATUS_PENDING,
		AddedAt:              req.BlockHeight,
		SunsetAt:             0,
		ModuleAccountAddress: moduleAccountAddr,
	}

	if err := k.ConsumerChains.Set(ctx, consumer.ChainId, consumer); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store consumer chain")
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"consumer_added",
			sdk.NewAttribute("chain_id", req.ChainId),
			sdk.NewAttribute("block_height", fmt.Sprintf("%d", req.BlockHeight)),
		),
	)

	return &types.MsgAddConsumerResponse{
		ModuleAccountAddress: moduleAccountAddr,
	}, nil
}

func (k msgServer) SunsetConsumer(ctx context.Context, req *types.MsgSunsetConsumer) (*types.MsgSunsetConsumerResponse, error) {
	authority, err := k.addressCodec.StringToBytes(req.Authority)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	if !bytes.Equal(k.GetAuthority(), authority) {
		expectedAuthorityStr, _ := k.addressCodec.BytesToString(k.GetAuthority())
		return nil, errorsmod.Wrapf(types.ErrInvalidSigner, "invalid authority; expected %s, got %s", expectedAuthorityStr, req.Authority)
	}

	// Validate input
	if req.ChainId == "" {
		return nil, types.ErrInvalidChainID
	}
	if req.BlockHeight < 0 {
		return nil, types.ErrInvalidBlockHeight
	}

	// Get consumer chain
	consumer, err := k.ConsumerChains.Get(ctx, req.ChainId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrConsumerNotFound, "consumer %s not found", req.ChainId)
	}

	// Check if consumer is already sunset or removed
	if consumer.Status == types.ConsumerStatus_CONSUMER_STATUS_SUNSET ||
		consumer.Status == types.ConsumerStatus_CONSUMER_STATUS_REMOVED {
		return nil, types.ErrConsumerAlreadySunset
	}

	// Get current block height
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentHeight := sdkCtx.BlockHeight()

	// Update consumer status
	if req.BlockHeight <= currentHeight {
		consumer.Status = types.ConsumerStatus_CONSUMER_STATUS_REMOVED
	} else {
		consumer.Status = types.ConsumerStatus_CONSUMER_STATUS_SUNSET
	}
	consumer.SunsetAt = req.BlockHeight

	if err := k.ConsumerChains.Set(ctx, consumer.ChainId, consumer); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update consumer chain")
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"consumer_sunset",
			sdk.NewAttribute("chain_id", req.ChainId),
			sdk.NewAttribute("block_height", fmt.Sprintf("%d", req.BlockHeight)),
		),
	)

	return &types.MsgSunsetConsumerResponse{}, nil
}

func (k msgServer) UpgradeConsumer(ctx context.Context, req *types.MsgUpgradeConsumer) (*types.MsgUpgradeConsumerResponse, error) {
	authority, err := k.addressCodec.StringToBytes(req.Authority)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	if !bytes.Equal(k.GetAuthority(), authority) {
		expectedAuthorityStr, _ := k.addressCodec.BytesToString(k.GetAuthority())
		return nil, errorsmod.Wrapf(types.ErrInvalidSigner, "invalid authority; expected %s, got %s", expectedAuthorityStr, req.Authority)
	}

	// Validate input
	if req.ChainId == "" {
		return nil, types.ErrInvalidChainID
	}
	if req.Version == "" {
		return nil, errorsmod.Wrap(types.ErrInvalidConsumerStatus, "version cannot be empty")
	}
	if req.BlockHeight < 0 {
		return nil, types.ErrInvalidBlockHeight
	}

	consumer, err := k.ConsumerChains.Get(ctx, req.ChainId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrConsumerNotFound, "consumer %s not found", req.ChainId)
	}

	// Check if consumer can be upgraded (can't upgrade sunset/removed chains)
	if !consumer.CanUpgrade() {
		return nil, types.ErrConsumerNotActive
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentHeight := sdkCtx.BlockHeight()

	// Update version immediately if the upgrade height is in the past or present
	if req.BlockHeight <= currentHeight {
		return nil, errorsmod.Wrapf(types.ErrInvalidBlockHeight, "block height must be greater than current height %d", currentHeight)
	}

	// TODO: Apply in BeginBlock/EndBlock.

	if err := k.ConsumerChains.Set(ctx, consumer.ChainId, consumer); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update consumer chain")
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"consumer_upgraded",
			sdk.NewAttribute("chain_id", req.ChainId),
			sdk.NewAttribute("version", req.Version),
			sdk.NewAttribute("block_height", fmt.Sprintf("%d", req.BlockHeight)),
		),
	)

	return &types.MsgUpgradeConsumerResponse{}, nil
}
