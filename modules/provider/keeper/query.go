package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	"github.com/atomone-hub/ics-poc-1/modules/provider/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var _ types.QueryServer = queryServer{}

// NewQueryServerImpl returns an implementation of the QueryServer interface
// for the provided Keeper.
func NewQueryServerImpl(k Keeper) types.QueryServer {
	return queryServer{k}
}

type queryServer struct {
	k Keeper
}

func (q queryServer) Params(ctx context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	params, err := q.k.Params.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			// Return default params if not found
			params = types.DefaultParams()
		} else {
			return nil, status.Error(codes.Internal, "internal error")
		}
	}

	return &types.QueryParamsResponse{Params: params}, nil
}

func (q queryServer) ConsumerChain(ctx context.Context, req *types.QueryConsumerChainRequest) (*types.QueryConsumerChainResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.ChainId == "" {
		return nil, status.Error(codes.InvalidArgument, "chain ID cannot be empty")
	}

	consumer, err := q.k.ConsumerChains.Get(ctx, req.ChainId)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "consumer chain %s not found", req.ChainId)
		}
		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryConsumerChainResponse{ConsumerChain: consumer}, nil
}

func (q queryServer) ConsumerChains(ctx context.Context, req *types.QueryConsumerChainsRequest) (*types.QueryConsumerChainsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	var consumers []types.ConsumerChain

	// Collect all consumer chains
	err := q.k.ConsumerChains.Walk(ctx, nil, func(key string, value types.ConsumerChain) (stop bool, err error) {
		consumers = append(consumers, value)
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryConsumerChainsResponse{
		ConsumerChains: consumers,
		Pagination:     nil,
	}, nil
}
