package abci

import (
	"context"
)

// HasPostFinalizeBlock is an extension interface that modules can implement
// to receive aggregated consumer chain data after FinalizeBlock.
// This is similar to HasEndBlocker but with a different signature that includes
// consumer chain aggregation data.
type HasPostFinalizeBlock interface {
	// PostFinalizeBlock is called after FinalizeBlock and receives:
	// - ctx: the block context
	// - req: aggregated data from all consumer chains in the previous block
	// Returns:
	// - resp: list of active chains and any updates for the next block
	// - error: if processing fails
	PostFinalizeBlock(ctx context.Context, req PostFinalizeBlockRequest) (PostFinalizeBlockResponse, error)
}

// PostFinalizeBlockRequest contains aggregated data from consumer chains
// in the previous block.
type PostFinalizeBlockRequest struct {
	// Height is the current block height
	Height int64

	// ConsumerAppHashes maps chain_id to the app hash from the previous block.
	// This aggregates all consumer chain app hashes that were processed.
	ConsumerAppHashes map[string][]byte
}

// ChainUpdate represents an update for a consumer chain
type ChainUpdate struct {
	// ChainId is the identifier of the consumer chain
	ChainId string

	// Active indicates whether this chain should be active in the next block
	Active bool

	// Metadata can contain additional chain-specific information
	Metadata map[string]string
}

// PostFinalizeBlockResponse contains the provider's response about
// which chains should be active in the next block.
type PostFinalizeBlockResponse struct {
	// ActiveChains is the list of consumer chains that should be active
	// in the next block. This is similar to validator updates in EndBlock.
	ActiveChains []ChainUpdate
}
