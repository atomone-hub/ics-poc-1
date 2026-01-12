package abci

import (
	"context"
)

// HasPostFinalizeBlock is an extension interface that modules can implement
// to receive aggregated consumer chain data after FinalizeBlock.
// This is similar to HasEndBlocker but with a different signature that includes
// consumer chain aggregation data.
//
// Example implementation:
//
//	func (am AppModule) PostFinalizeBlock(ctx context.Context, req abci.PostFinalizeBlockRequest) (abci.PostFinalizeBlockResponse, error) {
//	    consumers, _ := k.GetAllConsumerChains(ctx)
//	    activeChains := []abci.ChainUpdate{}
//
//	    for _, consumer := range consumers {
//	        activeChains = append(activeChains, abci.ChainUpdate{
//	            ChainId: consumer.ChainId,
//	            Active:  consumer.IsActive(),
//	            Status:  consumer.Status.String(),
//	            Name:    consumer.Name,
//	            Version: consumer.Version,
//	        })
//	    }
//
//	    return abci.PostFinalizeBlockResponse{ActiveChains: activeChains}, nil
//	}
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

// ChainUpdate represents an update for a consumer chain.
// This is returned by PostFinalizeBlock to indicate which chains should be
// active in the next block, similar to how EndBlock returns validator updates.
type ChainUpdate struct {
	// ChainId is the unique identifier of the consumer chain.
	// Must match the chain_id configured in the multiplexer's chain handlers.
	ChainId string

	// Active indicates whether this chain should be active in the next block.
	// If true, the multiplexer will process transactions and produce blocks for this chain.
	// If false, the chain will be skipped in all ABCI methods (CheckTx, ProcessProposal, FinalizeBlock, etc.).
	Active bool

	// Status is the current consensus status of the consumer chain.
	// Common values: "CONSUMER_STATUS_ACTIVE", "CONSUMER_STATUS_PENDING",
	// "CONSUMER_STATUS_SUNSET", "CONSUMER_STATUS_REMOVED".
	Status string

	// Name is the human-readable name of the consumer chain.
	// Used for logging and monitoring purposes.
	Name string

	// Version is the version of the consumer chain software.
	// Used for tracking upgrades and compatibility.
	Version string
}

// PostFinalizeBlockResponse contains the provider's response about
// which chains should be active in the next block.
type PostFinalizeBlockResponse struct {
	// ActiveChains is the list of consumer chains that should be active
	// in the next block. This is similar to validator updates in EndBlock.
	ActiveChains []ChainUpdate
}
