package abci

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Info consumer chains share the same consensus params as the provider chain.
// Additionally, because the consensus is the one inherited from the provider chains, consumer chains,
// regardless when they are started will share the same block height as the provider chain.
func (m *Multiplexer) Info(ctx context.Context, req *abci.RequestInfo) (*abci.ResponseInfo, error) {
	m.logger.Debug("Info")
	return m.providerChain.Info(req)
}

func (m *Multiplexer) Query(ctx context.Context, req *abci.RequestQuery) (*abci.ResponseQuery, error) {
	m.logger.Debug("Query", "chain_id", req.ChainId)

	if req.ChainId == m.providerChainID {
		return m.providerChain.Query(ctx, req)
	}

	handler, exists := m.chainHandlers[req.ChainId]
	if !exists {
		return &abci.ResponseQuery{Code: 1, Log: fmt.Sprintf("unknown chain: %s", req.ChainId)}, nil
	}

	// Check if consumer chain is active
	m.mu.Lock()
	isActive := m.activeConsumerChains[req.ChainId]
	m.mu.Unlock()

	if !isActive {
		return &abci.ResponseQuery{Code: 1, Log: fmt.Sprintf("inactive chain: %s", req.ChainId)}, nil
	}

	return handler.app.Query(ctx, req)
}

func (m *Multiplexer) CheckTx(ctx context.Context, req *abci.RequestCheckTx) (*abci.ResponseCheckTx, error) {
	m.logger.Debug("CheckTx", "tx_length", len(req.Tx))

	chainID, payload, err := ParseHeader(req.Tx)
	if err != nil {
		return &abci.ResponseCheckTx{Code: 1, Log: err.Error()}, nil
	}

	strippedReq := *req
	strippedReq.Tx = payload

	if chainID == m.providerChainID {
		return m.providerChain.CheckTx(&strippedReq)
	}

	handler, exists := m.chainHandlers[chainID]
	if !exists {
		return &abci.ResponseCheckTx{Code: 1, Log: fmt.Sprintf("unknown chain: %s", chainID)}, nil
	}

	// Check if consumer chain is active
	m.mu.Lock()
	isActive := m.activeConsumerChains[chainID]
	m.mu.Unlock()

	if !isActive {
		return &abci.ResponseCheckTx{Code: 1, Log: fmt.Sprintf("inactive chain: %s", chainID)}, nil
	}

	return handler.app.CheckTx(&strippedReq)
}

func (m *Multiplexer) InitChain(ctx context.Context, req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	m.logger.Debug("InitChain", "chain_id", req.ChainId)

	type result struct {
		chainID  string
		response *abci.ResponseInitChain
		err      error
	}

	// Count active chains + provider chain
	m.mu.Lock()
	activeCount := 1 // provider chain
	for chainID := range m.chainHandlers {
		if m.activeConsumerChains[chainID] {
			activeCount++
		}
	}
	m.mu.Unlock()

	results := make(chan result, activeCount)
	var wg sync.WaitGroup

	// Call provider chain first
	wg.Add(1)
	go func() {
		defer wg.Done()
		resp, err := m.providerChain.InitChain(req)
		results <- result{chainID: req.ChainId, response: resp, err: err}
	}()

	// Then call active consumer chains only
	m.mu.Lock()
	for chainID, handler := range m.chainHandlers {
		if !m.activeConsumerChains[chainID] {
			m.logger.Debug("Skipping InitChain for inactive consumer chain", "chain_id", chainID)
			continue
		}
		wg.Add(1)
		go func(id string, h *ChainHandler) {
			defer wg.Done()
			resp, err := h.app.InitChain(req)
			results <- result{chainID: id, response: resp, err: err}
		}(chainID, handler)
	}
	m.mu.Unlock()

	go func() {
		wg.Wait()
		close(results)
	}()

	response := &abci.ResponseInitChain{}
	chainHashes := make(map[string][]byte)

	for res := range results {
		if res.err != nil {
			if res.chainID == m.providerChainID {
				return nil, res.err
			} else {
				m.logger.Error("FinalizeBlock failed for consumer chain, skipping block for this chain",
					"chain_id", res.chainID, "error", res.err)
				continue
			}
		}

		chainHashes[res.chainID] = res.response.AppHash

		// Only use ConsensusParams and Validators from provider chain
		if res.chainID == m.providerChainID {
			response.ConsensusParams = res.response.ConsensusParams
			response.Validators = append(response.Validators, res.response.Validators...)
		}
	}

	// Combine app hashes in sorted order by chain ID
	var sortedChainIDs []string
	for chainID := range chainHashes {
		sortedChainIDs = append(sortedChainIDs, chainID)
	}
	sort.Strings(sortedChainIDs)

	for _, chainID := range sortedChainIDs {
		response.AppHash = append(response.AppHash, chainHashes[chainID]...)
	}

	return response, nil
}

func (m *Multiplexer) PrepareProposal(ctx context.Context, req *abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error) {
	m.logger.Debug("PrepareProposal", "num_txs", len(req.Txs))
	return &abci.ResponsePrepareProposal{Txs: req.Txs}, nil
}

func (m *Multiplexer) ProcessProposal(ctx context.Context, req *abci.RequestProcessProposal) (*abci.ResponseProcessProposal, error) {
	m.logger.Debug("ProcessProposal", "num_txs", len(req.Txs))

	chainTxs := make(map[string][][]byte)

	for _, tx := range req.Txs {
		chainID, payload, err := ParseHeader(tx)
		if err != nil {
			return &abci.ResponseProcessProposal{Status: abci.ResponseProcessProposal_REJECT}, nil
		}

		// Check if it's provider chain or consumer chain
		if chainID != m.providerChainID {
			if _, exists := m.chainHandlers[chainID]; !exists {
				return &abci.ResponseProcessProposal{Status: abci.ResponseProcessProposal_REJECT}, nil
			}

			// Check if consumer chain is active
			m.mu.Lock()
			isActive := m.activeConsumerChains[chainID]
			m.mu.Unlock()

			if !isActive {
				m.logger.Info("Rejecting transaction for inactive consumer chain in proposal",
					"chain_id", chainID)
				return &abci.ResponseProcessProposal{Status: abci.ResponseProcessProposal_REJECT}, nil
			}
		}

		chainTxs[chainID] = append(chainTxs[chainID], payload)
	}

	type result struct {
		chainID  string
		response *abci.ResponseProcessProposal
		err      error
	}

	results := make(chan result, len(chainTxs))
	var wg sync.WaitGroup

	for chainID, txs := range chainTxs {
		wg.Add(1)
		go func(id string, transactions [][]byte) {
			defer wg.Done()

			chainReq := *req
			chainReq.Txs = transactions

			var resp *abci.ResponseProcessProposal
			var err error

			// Call provider chain or consumer chain
			if id == m.providerChainID {
				resp, err = m.providerChain.ProcessProposal(&chainReq)
			} else {
				// Double-check the chain is still active
				m.mu.Lock()
				isActive := m.activeConsumerChains[id]
				m.mu.Unlock()

				if !isActive {
					// Chain became inactive, skip it
					resp = &abci.ResponseProcessProposal{Status: abci.ResponseProcessProposal_ACCEPT}
					err = nil
				} else {
					handler := m.chainHandlers[id]
					resp, err = handler.app.ProcessProposal(&chainReq)
				}
			}

			results <- result{chainID: id, response: resp, err: err}
		}(chainID, txs)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	// Clear rejected chains from previous proposal
	m.rejectedConsumerChains = make(map[string]bool)

	// Only reject proposal if provider chain rejects
	// Consumer chain rejections will skip that chain's block
	for res := range results {
		if res.err != nil {
			return nil, res.err
		}

		// If provider chain rejects, reject the entire proposal
		if res.chainID == m.providerChainID && res.response.Status != abci.ResponseProcessProposal_ACCEPT {
			m.logger.Info("Provider chain rejected proposal, rejecting entire proposal")
			return &abci.ResponseProcessProposal{Status: res.response.Status}, nil
		}

		// If consumer chain rejects, mark it to skip block in FinalizeBlock
		if res.chainID != m.providerChainID && res.response.Status != abci.ResponseProcessProposal_ACCEPT {
			m.logger.Info("Consumer chain rejected proposal, will skip block for this chain",
				"chain_id", res.chainID, "status", res.response.Status)
			m.rejectedConsumerChains[res.chainID] = true
		}
	}

	return &abci.ResponseProcessProposal{Status: abci.ResponseProcessProposal_ACCEPT}, nil
}

func (m *Multiplexer) FinalizeBlock(ctx context.Context, req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	m.logger.Info("FinalizeBlock", "height", req.Height, "num_txs", len(req.Txs))

	// Check halt conditions
	if err := m.checkHaltConditions(req); err != nil {
		return nil, fmt.Errorf("failed to finalize block because the node should halt: %w", err)
	}

	// Store previous block's consumer app hashes for PostFinalizeBlock
	m.mu.Lock()
	previousConsumerHashes := make(map[string][]byte)
	for chainID, hash := range m.lastConsumerAppHashes {
		previousConsumerHashes[chainID] = hash
	}
	m.mu.Unlock()

	chainTxs := make(map[string][][]byte)
	txPositions := make(map[string][]int)
	skippedTxIndices := make([]int, 0)

	for idx, tx := range req.Txs {
		chainID, payload, err := ParseHeader(tx)
		if err != nil {
			return nil, fmt.Errorf("invalid tx at index %d: %w", idx, err)
		}

		// Check if it's provider chain or consumer chain
		if chainID == m.providerChainID {
			chainTxs[m.providerChainID] = append(chainTxs[m.providerChainID], payload)
			txPositions[m.providerChainID] = append(txPositions[m.providerChainID], idx)
		} else {
			// Skip transactions for rejected consumer chains
			if m.rejectedConsumerChains[chainID] {
				m.logger.Debug("Skipping transaction for rejected consumer chain",
					"chain_id", chainID, "tx_index", idx)
				skippedTxIndices = append(skippedTxIndices, idx)
				continue
			}

			// Skip transactions for inactive consumer chains
			m.mu.Lock()
			isActive := m.activeConsumerChains[chainID]
			m.mu.Unlock()

			if !isActive {
				m.logger.Debug("Skipping transaction for inactive consumer chain",
					"chain_id", chainID, "tx_index", idx)
				skippedTxIndices = append(skippedTxIndices, idx)
				continue
			}

			handler, exists := m.chainHandlers[chainID]
			if !exists {
				return nil, fmt.Errorf("unknown chain for tx at index %d: %s", idx, chainID)
			}

			chainTxs[handler.ChainID] = append(chainTxs[handler.ChainID], payload)
			txPositions[handler.ChainID] = append(txPositions[handler.ChainID], idx)
		}
	}

	type result struct {
		chainID   string
		response  *abci.ResponseFinalizeBlock
		positions []int
		err       error
	}

	// Count provider + consumer chains that have transactions
	numChains := len(chainTxs)
	results := make(chan result, numChains)
	var wg sync.WaitGroup

	for chainID, txs := range chainTxs {
		wg.Add(1)
		go func(id string, transactions [][]byte, positions []int) {
			defer wg.Done()

			chainReq := *req
			chainReq.Txs = transactions

			var resp *abci.ResponseFinalizeBlock
			var err error

			// Call provider chain or consumer chain
			if id == m.providerChainID {
				resp, err = m.providerChain.FinalizeBlock(&chainReq)
			} else {
				// Double-check the chain is still active
				m.mu.Lock()
				isActive := m.activeConsumerChains[id]
				m.mu.Unlock()

				if !isActive {
					m.logger.Info("Skipping FinalizeBlock for inactive consumer chain", "chain_id", id)
					return
				}

				handler := m.chainHandlers[id]
				resp, err = handler.app.FinalizeBlock(&chainReq)
			}

			results <- result{
				chainID:   id,
				response:  resp,
				positions: positions,
				err:       err,
			}
		}(chainID, txs, txPositions[chainID])
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	response := &abci.ResponseFinalizeBlock{
		TxResults: make([]*abci.ExecTxResult, len(req.Txs)),
	}

	chainHashes := make(map[string][]byte)
	var events []abci.Event

	// Mark skipped transactions with error results
	for _, idx := range skippedTxIndices {
		response.TxResults[idx] = &abci.ExecTxResult{
			Code: 1,
			Log:  "transaction skipped: consumer chain rejected proposal or inactive",
		}
	}

	for res := range results {
		if res.err != nil {
			if res.chainID == m.providerChainID {
				return nil, res.err
			} else {
				m.logger.Error("FinalizeBlock failed for consumer chain, skipping block for this chain",
					"chain_id", res.chainID, "error", res.err)
				continue
			}
		}

		for i, pos := range res.positions {
			if i < len(res.response.TxResults) {
				response.TxResults[pos] = res.response.TxResults[i]
			}
		}

		chainHashes[res.chainID] = res.response.AppHash
		events = append(events, res.response.Events...)

		// Only use ConsensusParamUpdates and ValidatorUpdates from provider chain
		if res.chainID == m.providerChainID {
			response.ConsensusParamUpdates = res.response.ConsensusParamUpdates
			response.ValidatorUpdates = append(response.ValidatorUpdates, res.response.ValidatorUpdates...)
		}
	}

	// Combine app hashes in sorted order
	var sortedChainIDs []string
	for chainID := range chainHashes {
		sortedChainIDs = append(sortedChainIDs, chainID)
	}
	sort.Strings(sortedChainIDs)

	for _, chainID := range sortedChainIDs {
		response.AppHash = append(response.AppHash, chainHashes[chainID]...)
	}

	response.Events = events

	// Clear rejected consumer chains after finalizing block
	m.rejectedConsumerChains = make(map[string]bool)

	// Store current consumer app hashes for next block's PostFinalizeBlock
	m.mu.Lock()
	m.lastConsumerAppHashes = make(map[string][]byte)
	for chainID, hash := range chainHashes {
		if chainID != m.providerChainID {
			m.lastConsumerAppHashes[chainID] = hash
		}
	}
	m.mu.Unlock()

	// Call PostFinalizeBlock extension if the provider app implements it
	if err := m.callPostFinalizeBlock(ctx, req.Height, previousConsumerHashes); err != nil {
		// Check if this is a misconfiguration error - these are fatal
		if strings.Contains(err.Error(), "node misconfiguration") {
			return nil, fmt.Errorf("fatal configuration error in PostFinalizeBlock: %w", err)
		}
		m.logger.Error("PostFinalizeBlock failed", "height", req.Height, "error", err)
	}

	m.logger.Info("FinalizeBlock complete", "height", req.Height)
	return response, nil
}

// callPostFinalizeBlock calls the PostFinalizeBlock extension on the provider app if it implements the interface
func (m *Multiplexer) callPostFinalizeBlock(ctx context.Context, height int64, consumerAppHashes map[string][]byte) error {
	postFinalizeBlocker, ok := m.providerChain.(HasPostFinalizeBlock)
	if !ok {
		return errors.New("provider does not implement PostFinalizeBlock")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	req := PostFinalizeBlockRequest{
		Height:            height,
		ConsumerAppHashes: consumerAppHashes,
	}

	resp, err := postFinalizeBlocker.PostFinalizeBlock(sdkCtx, req)
	if err != nil {
		return fmt.Errorf("provider PostFinalizeBlock failed: %w", err)
	}

	// Validate that all active chains are configured in chainHandlers
	// This prevents nodes from participating in consensus with incorrect configuration
	for _, chain := range resp.ActiveChains {
		if chain.Active {
			if _, exists := m.chainHandlers[chain.ChainId]; !exists {
				return fmt.Errorf("node misconfiguration: chain '%s' is marked as active by provider but not configured in this node's chain handlers - "+
					"please add this chain to your configuration file and restart the node. "+
					"Configured chains: %v", chain.ChainId, m.getConfiguredChainIDsLocked())
			}
		}
	}

	m.mu.Lock()
	// Clear previous active chains
	m.activeConsumerChains = make(map[string]bool)

	// Set active chains from response
	for _, chain := range resp.ActiveChains {
		m.activeConsumerChains[chain.ChainId] = chain.Active
		m.logger.Debug("Chain status update",
			"chain_id", chain.ChainId,
			"active", chain.Active,
			"height", height)
	}
	m.mu.Unlock()

	// Log active chains summary
	activeCount := 0
	for _, active := range m.activeConsumerChains {
		if active {
			activeCount++
		}
	}
	m.logger.Info("PostFinalizeBlock: active chains updated",
		"height", height,
		"total_chains", len(resp.ActiveChains),
		"active_chains", activeCount)

	// Warn about configured chains that are not in the active chains response
	// This could indicate a configuration mismatch or that the provider doesn't know about the chain yet
	responseChains := make(map[string]bool)
	for _, chain := range resp.ActiveChains {
		responseChains[chain.ChainId] = true
	}
	for chainID := range m.chainHandlers {
		if !responseChains[chainID] {
			m.logger.Warn("Chain configured locally but not returned by provider",
				"chain_id", chainID,
				"height", height,
				"suggestion", "verify chain is registered on provider or remove from local configuration")
		}
	}

	return nil
}

// getConfiguredChainIDsLocked returns a list of configured chain IDs.
// Caller must hold m.mu lock.
func (m *Multiplexer) getConfiguredChainIDsLocked() []string {
	chainIDs := make([]string, 0, len(m.chainHandlers))
	for chainID := range m.chainHandlers {
		chainIDs = append(chainIDs, chainID)
	}
	return chainIDs
}

func (m *Multiplexer) Commit(ctx context.Context, req *abci.RequestCommit) (*abci.ResponseCommit, error) {
	m.logger.Debug("Commit")

	// Call provider chain first
	response, err := m.providerChain.Commit()
	if err != nil {
		return nil, err
	}

	// Then call active consumer chains only
	m.mu.Lock()
	activeChains := make(map[string]*ChainHandler)
	for chainID, handler := range m.chainHandlers {
		if m.activeConsumerChains[chainID] {
			activeChains[chainID] = handler
		}
	}
	m.mu.Unlock()

	for chainID, handler := range activeChains {
		resp, err := handler.app.Commit()
		if err != nil {
			m.logger.Error("Commit failed for consumer chain", "chain_id", chainID, "error", err) // TODO: we should check how to recover from this.
			continue
		}
		response = resp
	}

	return response, nil
}

func (m *Multiplexer) ExtendVote(ctx context.Context, req *abci.RequestExtendVote) (*abci.ResponseExtendVote, error) {
	return &abci.ResponseExtendVote{}, nil
}

func (m *Multiplexer) VerifyVoteExtension(ctx context.Context, req *abci.RequestVerifyVoteExtension) (*abci.ResponseVerifyVoteExtension, error) {
	return &abci.ResponseVerifyVoteExtension{Status: abci.ResponseVerifyVoteExtension_ACCEPT}, nil
}

func (m *Multiplexer) ListSnapshots(ctx context.Context, req *abci.RequestListSnapshots) (*abci.ResponseListSnapshots, error) {
	return &abci.ResponseListSnapshots{}, nil
}

func (m *Multiplexer) OfferSnapshot(ctx context.Context, req *abci.RequestOfferSnapshot) (*abci.ResponseOfferSnapshot, error) {
	return &abci.ResponseOfferSnapshot{}, nil
}

func (m *Multiplexer) LoadSnapshotChunk(ctx context.Context, req *abci.RequestLoadSnapshotChunk) (*abci.ResponseLoadSnapshotChunk, error) {
	return &abci.ResponseLoadSnapshotChunk{}, nil
}

func (m *Multiplexer) ApplySnapshotChunk(ctx context.Context, req *abci.RequestApplySnapshotChunk) (*abci.ResponseApplySnapshotChunk, error) {
	return &abci.ResponseApplySnapshotChunk{Result: abci.ResponseApplySnapshotChunk_ACCEPT}, nil
}
