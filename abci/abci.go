package abci

import (
	"context"
	"fmt"
	"sort"
	"sync"

	abci "github.com/cometbft/cometbft/abci/types"
)

// Info consumer chains share the same consensus params as the provider chain.
// Additionally, because the consensus is the one inherited from the provider chains, consumer chains,
// regardless when they are started will share the same block height as the provider chain.
func (m *Multiplexer) Info(ctx context.Context, req *abci.RequestInfo) (*abci.ResponseInfo, error) {
	m.logger.Debug("Info", "chain_id", m.providerChainID)
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

	if !m.initializedConsumerChains[chainID] {
		return &abci.ResponseCheckTx{Code: 1, Log: fmt.Sprintf("chain not initialized: %s", chainID)}, nil
	}

	return handler.app.CheckTx(&strippedReq)
}

func (m *Multiplexer) InitChain(ctx context.Context, req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	m.logger.Debug("InitChain", "chain_id", req.ChainId)

	providerResp, err := m.providerChain.InitChain(req)
	if err != nil {
		return nil, err
	}

	response := &abci.ResponseInitChain{
		ConsensusParams: providerResp.ConsensusParams,
		Validators:      providerResp.Validators,
	}
	m.providerGenesisConsensusParams = providerResp.ConsensusParams
	m.providerGenesisValidators = providerResp.Validators

	chainHashes := make(map[string][]byte)
	chainHashes[m.providerChainID] = providerResp.AppHash

	type result struct {
		chainID  string
		response *abci.ResponseInitChain
		err      error
	}

	results := make(chan result, len(m.chainHandlers))
	var wg sync.WaitGroup

	for chainID, handler := range m.chainHandlers {
		wg.Go(func() {
			consumerReq := *req
			consumerReq.ChainId = chainID
			consumerReq.Validators = m.providerGenesisValidators
			consumerReq.ConsensusParams = m.providerGenesisConsensusParams

			// Load consumer chain's own genesis state from its home directory
			appState, err := handler.config.LoadGenesisAppState()
			if err != nil {
				m.logger.Error("Failed to load genesis for consumer chain", "chain_id", chainID, "error", err)
				results <- result{chainID: chainID, response: nil, err: err}
				return
			}
			consumerReq.AppStateBytes = appState

			resp, err := handler.app.InitChain(&consumerReq)
			results <- result{chainID: chainID, response: resp, err: err}
		})
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	for res := range results {
		if res.err != nil {
			m.logger.Error("InitChain failed for consumer chain, skipping", "chain_id", res.chainID, "error", res.err)
			continue
		}

		chainHashes[res.chainID] = res.response.AppHash
		m.initializedConsumerChains[res.chainID] = true
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

	// Parse and categorize transactions by chain
	chainTxs := make(map[string][][]byte)
	for _, tx := range req.Txs {
		chainID, payload, err := ParseHeader(tx)
		if err != nil {
			return &abci.ResponseProcessProposal{Status: abci.ResponseProcessProposal_REJECT}, nil
		}

		if chainID != m.providerChainID {
			if _, exists := m.chainHandlers[chainID]; !exists {
				m.logger.Debug("Transaction for unknown chain in proposal, skipping", "chain_id", chainID)
				continue
			}
			if !m.initializedConsumerChains[chainID] {
				m.logger.Debug("Transaction for uninitialized chain in proposal, skipping", "chain_id", chainID)
				continue
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
		wg.Go(func() {
			chainReq := *req
			chainReq.Txs = txs

			var resp *abci.ResponseProcessProposal
			var err error

			if chainID == m.providerChainID {
				resp, err = m.providerChain.ProcessProposal(&chainReq)
			} else {
				resp, err = m.chainHandlers[chainID].app.ProcessProposal(&chainReq)
			}

			results <- result{chainID: chainID, response: resp, err: err}
		})
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	// Clear rejected chains from previous proposal
	m.rejectedConsumerChains = make(map[string]bool)

	for res := range results {
		if res.err != nil {
			return nil, res.err
		}

		// Provider chain rejection rejects entire proposal
		if res.chainID == m.providerChainID && res.response.Status != abci.ResponseProcessProposal_ACCEPT {
			m.logger.Warn("Provider chain rejected proposal, rejecting entire proposal")
			return &abci.ResponseProcessProposal{Status: res.response.Status}, nil
		}

		// Consumer chain rejection marks it to skip in FinalizeBlock
		if res.chainID != m.providerChainID && res.response.Status != abci.ResponseProcessProposal_ACCEPT {
			m.logger.Warn("Consumer chain rejected proposal, will skip block", "chain_id", res.chainID, "status", res.response.Status)
			m.rejectedConsumerChains[res.chainID] = true
		}
	}

	return &abci.ResponseProcessProposal{Status: abci.ResponseProcessProposal_ACCEPT}, nil
}

func (m *Multiplexer) FinalizeBlock(ctx context.Context, req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	m.logger.Info("FinalizeBlock", "height", req.Height, "num_txs", len(req.Txs))

	if err := m.checkHaltConditions(req); err != nil {
		return nil, fmt.Errorf("failed to finalize block because the node should halt: %w", err)
	}

	// parse and categorize transactions by chain
	chainTxs := make(map[string][][]byte)
	txPositions := make(map[string][]int)
	var skippedIndices []int

	for idx, tx := range req.Txs {
		chainID, payload, err := ParseHeader(tx)
		if err != nil {
			return nil, fmt.Errorf("invalid tx at index %d: %w", idx, err)
		}

		if m.rejectedConsumerChains[chainID] {
			m.logger.Debug("Skipping transaction for rejected consumer chain", "chain_id", chainID, "tx_index", idx)
			skippedIndices = append(skippedIndices, idx)
			continue
		}

		if chainID != m.providerChainID {
			if _, exists := m.chainHandlers[chainID]; !exists {
				m.logger.Debug("Unknown chain for tx at index", "chain_id", chainID, "tx_index", idx)
				skippedIndices = append(skippedIndices, idx)
				continue
			}
		}

		chainTxs[chainID] = append(chainTxs[chainID], payload)
		txPositions[chainID] = append(txPositions[chainID], idx)
	}

	// process provider chain first
	providerReq := *req
	providerReq.Txs = chainTxs[m.providerChainID]
	providerResp, err := m.providerChain.FinalizeBlock(&providerReq)
	if err != nil {
		return nil, fmt.Errorf("provider chain FinalizeBlock failed: %w", err)
	}

	if req.Height > 1 {
		if err := m.updateActiveChains(ctx); err != nil {
			m.logger.Error("Failed to update active chains", "error", err)
		}
	}

	// process active consumer chains in parallel
	type result struct {
		chainID   string
		response  *abci.ResponseFinalizeBlock
		positions []int
		err       error
	}

	results := make(chan result, len(m.activeChains))
	var wg sync.WaitGroup

	for chainID := range m.activeChains {
		if m.rejectedConsumerChains[chainID] {
			continue
		}

		handler, exists := m.chainHandlers[chainID]
		if !exists {
			m.logger.Error("Active consumer chain missing handler", "chain_id", chainID)
			continue
		}

		if _, err := m.initChainIfNeeded(chainID, req.Height, req.Time); err != nil {
			m.logger.Error("Failed to initialize chain", "chain_id", chainID, "error", err)
			continue
		}

		if !m.initializedConsumerChains[chainID] {
			m.logger.Warn("Skipping uninitialized chain", "chain_id", chainID)
			continue
		}

		txs := chainTxs[chainID]
		positions := txPositions[chainID]
		wg.Go(func() {
			chainReq := *req
			chainReq.Txs = txs
			resp, err := handler.app.FinalizeBlock(&chainReq)
			results <- result{chainID: chainID, response: resp, positions: positions, err: err}
		})
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	response := &abci.ResponseFinalizeBlock{
		TxResults:             make([]*abci.ExecTxResult, len(req.Txs)),
		ConsensusParamUpdates: providerResp.ConsensusParamUpdates,
		ValidatorUpdates:      providerResp.ValidatorUpdates,
	}

	chainHashes := map[string][]byte{m.providerChainID: providerResp.AppHash}
	events := providerResp.Events

	// Map provider tx results
	for i, pos := range txPositions[m.providerChainID] {
		if i < len(providerResp.TxResults) {
			response.TxResults[pos] = providerResp.TxResults[i]
		}
	}

	// Mark skipped transactions
	for _, idx := range skippedIndices {
		response.TxResults[idx] = &abci.ExecTxResult{
			Code: 1,
			Log:  "transaction skipped",
		}
	}

	// collect consumer chain results
	for res := range results {
		if res.err != nil {
			m.logger.Error("FinalizeBlock failed for consumer chain", "chain_id", res.chainID, "error", res.err)
			continue
		}

		for i, pos := range res.positions {
			if i < len(res.response.TxResults) {
				response.TxResults[pos] = res.response.TxResults[i]
			}
		}

		chainHashes[res.chainID] = res.response.AppHash
		events = append(events, res.response.Events...)
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

	return response, nil
}

func (m *Multiplexer) Commit(ctx context.Context, req *abci.RequestCommit) (*abci.ResponseCommit, error) {
	m.logger.Debug("Commit")

	response, err := m.providerChain.Commit()
	if err != nil {
		return nil, err
	}

	// Only commit active and initialized chains
	for chainID := range m.activeChains {
		if !m.initializedConsumerChains[chainID] {
			m.logger.Debug("Skipping Commit for uninitialized chain", "chain_id", chainID)
			continue
		}

		if m.rejectedConsumerChains[chainID] {
			continue
		}

		handler, exists := m.chainHandlers[chainID]
		if !exists {
			m.logger.Error("Skipping Commit for missing chain handler", "chain_id", chainID)
			continue
		}

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
