package keeper

import (
	"bytes"
	"context"

	errorsmod "cosmossdk.io/errors"
	"github.com/atomone-hub/ics-poc-1/modules/provider/types"
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
	panic("unimplemented")
}

func (k msgServer) SunsetConsumer(ctx context.Context, req *types.MsgSunsetConsumer) (*types.MsgSunsetConsumerResponse, error) {
	panic("unimplemented")
}

func (k msgServer) UpgradeConsumer(ctx context.Context, req *types.MsgUpgradeConsumer) (*types.MsgUpgradeConsumerResponse, error) {
	panic("unimplemented")
}
