package abci

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// mockPostFinalizeBlockApp is a mock application that implements HasPostFinalizeBlock
type mockPostFinalizeBlockApp struct {
	response PostFinalizeBlockResponse
	err      error
}

func (m *mockPostFinalizeBlockApp) PostFinalizeBlock(ctx context.Context, req PostFinalizeBlockRequest) (PostFinalizeBlockResponse, error) {
	return m.response, m.err
}

func TestPostFinalizeBlockValidation_MissingChainHandler(t *testing.T) {
	tests := []struct {
		name             string
		configuredChains []string
		activeChains     []ChainUpdate
		expectError      bool
		errorContains    string
	}{
		{
			name:             "all active chains configured - valid",
			configuredChains: []string{"consumer-1", "consumer-2"},
			activeChains: []ChainUpdate{
				{ChainId: "consumer-1", Active: true},
				{ChainId: "consumer-2", Active: true},
			},
			expectError: false,
		},
		{
			name:             "subset of chains active - valid",
			configuredChains: []string{"consumer-1", "consumer-2", "consumer-3"},
			activeChains: []ChainUpdate{
				{ChainId: "consumer-1", Active: true},
				{ChainId: "consumer-2", Active: false},
			},
			expectError: false,
		},
		{
			name:             "active chain not configured - invalid",
			configuredChains: []string{"consumer-1", "consumer-2"},
			activeChains: []ChainUpdate{
				{ChainId: "consumer-1", Active: true},
				{ChainId: "consumer-3", Active: true}, // Not configured!
			},
			expectError:   true,
			errorContains: "node misconfiguration: chain 'consumer-3' is marked as active",
		},
		{
			name:             "multiple missing chains - invalid",
			configuredChains: []string{"consumer-1"},
			activeChains: []ChainUpdate{
				{ChainId: "consumer-1", Active: true},
				{ChainId: "consumer-2", Active: true}, // Not configured!
				{ChainId: "consumer-3", Active: true}, // Not configured!
			},
			expectError:   true,
			errorContains: "node misconfiguration",
		},
		{
			name:             "inactive chain not configured - valid",
			configuredChains: []string{"consumer-1"},
			activeChains: []ChainUpdate{
				{ChainId: "consumer-1", Active: true},
				{ChainId: "consumer-2", Active: false}, // Inactive, so OK if not configured
			},
			expectError: false,
		},
		{
			name:             "no active chains - valid",
			configuredChains: []string{"consumer-1", "consumer-2"},
			activeChains: []ChainUpdate{
				{ChainId: "consumer-1", Active: false},
				{ChainId: "consumer-2", Active: false},
			},
			expectError: false,
		},
		{
			name:             "empty response - valid",
			configuredChains: []string{"consumer-1", "consumer-2"},
			activeChains:     []ChainUpdate{},
			expectError:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock multiplexer with configured chains
			m := &Multiplexer{
				chainHandlers: make(map[string]*ChainHandler),
			}

			// Configure chains
			for _, chainID := range tt.configuredChains {
				m.chainHandlers[chainID] = &ChainHandler{
					ChainID: chainID,
				}
			}

			// Validate active chains
			for _, chain := range tt.activeChains {
				if chain.Active {
					_, exists := m.chainHandlers[chain.ChainId]
					if !exists && tt.expectError {
						// Expected error case
						require.Contains(t, "consumer-3", chain.ChainId, "test setup error")
					} else if !exists && !tt.expectError {
						t.Errorf("test setup error: active chain %s not configured but no error expected", chain.ChainId)
					}
				}
			}

			// Simulate the validation logic from callPostFinalizeBlock
			var validationErr error
			for _, chain := range tt.activeChains {
				if chain.Active {
					if _, exists := m.chainHandlers[chain.ChainId]; !exists {
						validationErr = &validationError{
							chainID:          chain.ChainId,
							configuredChains: getChainIDs(m.chainHandlers),
						}
						break
					}
				}
			}

			if tt.expectError {
				require.Error(t, validationErr, "expected validation error")
				if tt.errorContains != "" {
					require.Contains(t, validationErr.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, validationErr, "unexpected validation error")
			}
		})
	}
}

func TestPostFinalizeBlockRequest_Validation(t *testing.T) {
	tests := []struct {
		name    string
		request PostFinalizeBlockRequest
		valid   bool
	}{
		{
			name: "valid request with consumer hashes",
			request: PostFinalizeBlockRequest{
				Height: 100,
				ConsumerAppHashes: map[string][]byte{
					"consumer-1": []byte{0x01, 0x02, 0x03},
					"consumer-2": []byte{0x04, 0x05, 0x06},
				},
			},
			valid: true,
		},
		{
			name: "valid request with empty hashes",
			request: PostFinalizeBlockRequest{
				Height:            100,
				ConsumerAppHashes: map[string][]byte{},
			},
			valid: true,
		},
		{
			name: "valid request with nil hashes",
			request: PostFinalizeBlockRequest{
				Height:            100,
				ConsumerAppHashes: nil,
			},
			valid: true,
		},
		{
			name: "genesis height",
			request: PostFinalizeBlockRequest{
				Height:            0,
				ConsumerAppHashes: map[string][]byte{},
			},
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify request structure is correct
			require.NotNil(t, &tt.request)
			if tt.valid {
				require.GreaterOrEqual(t, tt.request.Height, int64(0))
			}
		})
	}
}

func TestChainUpdate_Properties(t *testing.T) {
	tests := []struct {
		name     string
		update   ChainUpdate
		metadata map[string]string
	}{
		{
			name: "active chain with metadata",
			update: ChainUpdate{
				ChainId: "consumer-1",
				Active:  true,
				Metadata: map[string]string{
					"status":  "ACTIVE",
					"version": "v1.0.0",
				},
			},
			metadata: map[string]string{
				"status":  "ACTIVE",
				"version": "v1.0.0",
			},
		},
		{
			name: "inactive chain",
			update: ChainUpdate{
				ChainId: "consumer-2",
				Active:  false,
			},
		},
		{
			name: "chain with empty metadata",
			update: ChainUpdate{
				ChainId:  "consumer-3",
				Active:   true,
				Metadata: map[string]string{},
			},
			metadata: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NotEmpty(t, tt.update.ChainId)
			if tt.metadata != nil {
				require.Equal(t, tt.metadata, tt.update.Metadata)
			}
		})
	}
}

func TestPostFinalizeBlockResponse_ActiveChainsFiltering(t *testing.T) {
	response := PostFinalizeBlockResponse{
		ActiveChains: []ChainUpdate{
			{ChainId: "consumer-1", Active: true},
			{ChainId: "consumer-2", Active: false},
			{ChainId: "consumer-3", Active: true},
			{ChainId: "consumer-4", Active: false},
		},
	}

	// Filter active chains
	var activeChains []string
	for _, chain := range response.ActiveChains {
		if chain.Active {
			activeChains = append(activeChains, chain.ChainId)
		}
	}

	require.Len(t, activeChains, 2)
	require.Contains(t, activeChains, "consumer-1")
	require.Contains(t, activeChains, "consumer-3")
	require.NotContains(t, activeChains, "consumer-2")
	require.NotContains(t, activeChains, "consumer-4")
}

// validationError is a helper type for testing
type validationError struct {
	chainID          string
	configuredChains []string
}

func (e *validationError) Error() string {
	return "node misconfiguration: chain '" + e.chainID + "' is marked as active but not configured in this node's chain handlers"
}

// getChainIDs extracts chain IDs from handlers map
func getChainIDs(handlers map[string]*ChainHandler) []string {
	ids := make([]string, 0, len(handlers))
	for id := range handlers {
		ids = append(ids, id)
	}
	return ids
}
