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

	var response *abci.ResponseInfo
	for _, handler := range m.chainHandlers {
		resp, err := handler.app.Info(req)
		if err != nil {
			return nil, err
		}
		response = resp
	}

	return response, nil
}

func (m *Multiplexer) Query(ctx context.Context, req *abci.RequestQuery) (*abci.ResponseQuery, error) {
	m.logger.Debug("Query", "chain_id", req.ChainId)

	handler, err := m.getHandler(req.ChainId)
	if err != nil {
		return &abci.ResponseQuery{Code: 1, Log: err.Error()}, nil
	}

	return handler.app.Query(ctx, req)
}

func (m *Multiplexer) CheckTx(ctx context.Context, req *abci.RequestCheckTx) (*abci.ResponseCheckTx, error) {
	m.logger.Debug("CheckTx", "tx_length", len(req.Tx))

	chainID, payload, err := ParseHeader(req.Tx)
	if err != nil {
		return &abci.ResponseCheckTx{Code: 1, Log: err.Error()}, nil
	}

	handler, err := m.getHandler(chainID)
	if err != nil {
		return &abci.ResponseCheckTx{Code: 1, Log: err.Error()}, nil
	}

	strippedReq := *req
	strippedReq.Tx = payload

	return handler.app.CheckTx(&strippedReq)
}

func (m *Multiplexer) InitChain(ctx context.Context, req *abci.RequestInitChain) (*abci.ResponseInitChain, error) {
	m.logger.Info("InitChain", "chain_id", req.ChainId)

	type result struct {
		chainID  string
		response *abci.ResponseInitChain
		err      error
	}

	results := make(chan result, len(m.chainHandlers))
	var wg sync.WaitGroup

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

		if response.ConsensusParams == nil {
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

		if _, exists := m.chainHandlers[chainID]; !exists {
			return &abci.ResponseProcessProposal{Status: abci.ResponseProcessProposal_REJECT}, nil
		}

		chainTxs[chainID] = append(chainTxs[chainID], payload)
	}

	type result struct {
		response *abci.ResponseProcessProposal
		err      error
	}

	results := make(chan result, len(chainTxs))
	var wg sync.WaitGroup

	for chainID, txs := range chainTxs {
		wg.Add(1)
		go func(id string, transactions [][]byte) {
			defer wg.Done()

			handler := m.chainHandlers[id]
			chainReq := *req
			chainReq.Txs = transactions

			resp, err := handler.app.ProcessProposal(&chainReq)
			results <- result{response: resp, err: err}
		}(chainID, txs)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	status := abci.ResponseProcessProposal_ACCEPT
	for res := range results {
		if res.err != nil {
			return nil, res.err
		}
		if res.response.Status != abci.ResponseProcessProposal_ACCEPT {
			status = res.response.Status
		}
	}

	return &abci.ResponseProcessProposal{Status: status}, nil
}

func (m *Multiplexer) FinalizeBlock(ctx context.Context, req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	m.logger.Info("FinalizeBlock", "height", req.Height, "num_txs", len(req.Txs))

	// Check halt conditions
	if err := m.checkHaltConditions(req); err != nil {
		return nil, fmt.Errorf("failed to finalize block because the node should halt: %w", err)
	}

	chainTxs := make(map[string][][]byte)
	txPositions := make(map[string][]int)

	for idx, tx := range req.Txs {
		chainID, payload, err := ParseHeader(tx)
		if err != nil {
			return nil, fmt.Errorf("invalid tx at index %d: %w", idx, err)
		}

		handler, err := m.getHandler(chainID)
		if err != nil {
			return nil, fmt.Errorf("unknown chain for tx at index %d: %w", idx, err)
		}

		chainTxs[handler.ChainID] = append(chainTxs[handler.ChainID], payload)
		txPositions[handler.ChainID] = append(txPositions[handler.ChainID], idx)
	}

	type result struct {
		chainID   string
		response  *abci.ResponseFinalizeBlock
		positions []int
		err       error
	}

	results := make(chan result, len(chainTxs))
	var wg sync.WaitGroup

	for chainID, txs := range chainTxs {
		wg.Add(1)
		go func(id string, transactions [][]byte, positions []int) {
			defer wg.Done()

			handler := m.chainHandlers[id]
			chainReq := *req
			chainReq.Txs = transactions

			resp, err := handler.app.FinalizeBlock(&chainReq)
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

		if response.ConsensusParamUpdates == nil {
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

	m.logger.Info("FinalizeBlock complete", "height", req.Height)
	return response, nil
}

func (m *Multiplexer) Commit(ctx context.Context, req *abci.RequestCommit) (*abci.ResponseCommit, error) {
	m.logger.Debug("Commit")

	var response *abci.ResponseCommit
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
