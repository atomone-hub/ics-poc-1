package abci

import (
	"context"
	"fmt"
	"sort"
	"sync"

	abci "github.com/cometbft/cometbft/abci/types"
)

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

	return handler.app.CheckTx(&strippedReq)
}

func (m *Multiplexer) InitChain(ctx context.Context, req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	m.logger.Info("InitChain", "chain_id", req.ChainId)

	type result struct {
		chainID  string
		response *abci.ResponseInitChain
		err      error
	}

	// Include provider chain in the count
	numChains := len(m.chainHandlers) + 1
	results := make(chan result, numChains)
	var wg sync.WaitGroup

	// Call provider chain first
	wg.Go(func() {
		defer wg.Done()
		resp, err := m.providerChain.InitChain(req)
		results <- result{chainID: req.ChainId, response: resp, err: err}
	})

	// Then call consumer chains
	for chainID, handler := range m.chainHandlers {
		wg.Add(1)
		go func(id string, h *ChainHandler) {
			defer wg.Done()
			resp, err := h.app.InitChain(req)
			results <- result{chainID: id, response: resp, err: err}
		}(chainID, handler)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	response := &abci.ResponseInitChain{}
	chainHashes := make(map[string][]byte)

	for res := range results {
		if res.err != nil {
			return nil, res.err
		}

		chainHashes[res.chainID] = res.response.AppHash

		// Only use ConsensusParams from provider chain
		if res.chainID == m.providerChainID {
			response.ConsensusParams = res.response.ConsensusParams
		}
		response.Validators = append(response.Validators, res.response.Validators...)
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

	m.logger.Info("InitChain complete", "num_chains", len(sortedChainIDs))
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
				handler := m.chainHandlers[id]
				resp, err = handler.app.ProcessProposal(&chainReq)
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

	chainTxs := make(map[string][][]byte)
	txPositions := make(map[string][]int)
	skippedTxIndices := make([]int, 0)

	for idx, tx := range req.Txs {
		chainID, payload, err := ParseHeader(tx)
		if err != nil {
			return nil, fmt.Errorf("invalid tx at index %d: %w", idx, err)
		}

		// Check if it's for provider or consumer chain
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
	var validatorUpdates []abci.ValidatorUpdate
	var events []abci.Event

	// Mark skipped transactions with error results
	for _, idx := range skippedTxIndices {
		response.TxResults[idx] = &abci.ExecTxResult{
			Code: 1,
			Log:  "transaction skipped: consumer chain rejected proposal",
		}
	}

	for res := range results {
		if res.err != nil {
			return nil, res.err
		}

		for i, pos := range res.positions {
			if i < len(res.response.TxResults) {
				response.TxResults[pos] = res.response.TxResults[i]
			}
		}

		chainHashes[res.chainID] = res.response.AppHash
		validatorUpdates = append(validatorUpdates, res.response.ValidatorUpdates...)
		events = append(events, res.response.Events...)

		// Only use ConsensusParamUpdates from provider chain
		if res.chainID == m.providerChainID {
			response.ConsensusParamUpdates = res.response.ConsensusParamUpdates
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

	response.ValidatorUpdates = validatorUpdates
	response.Events = events

	// Clear rejected consumer chains after finalizing block
	m.rejectedConsumerChains = make(map[string]bool)

	m.logger.Info("FinalizeBlock complete", "height", req.Height)
	return response, nil
}

func (m *Multiplexer) Commit(ctx context.Context, req *abci.RequestCommit) (*abci.ResponseCommit, error) {
	m.logger.Debug("Commit")

	// Call provider chain first
	response, err := m.providerChain.Commit()
	if err != nil {
		return nil, err
	}

	// Then call consumer chains
	for _, handler := range m.chainHandlers {
		resp, err := handler.app.Commit()
		if err != nil {
			return nil, err
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
