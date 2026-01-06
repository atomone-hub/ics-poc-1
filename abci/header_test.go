package abci

import (
	"testing"
)

func TestCreateTransactionHeader(t *testing.T) {
	tests := []struct {
		name     string
		chainID  string
		expected string
	}{
		{
			name:     "chain-1",
			chainID:  "chain-1",
			expected: "ICS:chain-1:",
		},
		{
			name:     "chain-2",
			chainID:  "chain-2",
			expected: "ICS:chain-2:",
		},
		{
			name:     "cosmoshub-4",
			chainID:  "cosmoshub-4",
			expected: "ICS:cosmoshub-4:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header := CreateTransactionHeader(tt.chainID)
			if string(header) != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, string(header))
			}
		})
	}
}

func TestParseHeader(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		expectedChainID string
		expectedPayload string
		wantErr         bool
	}{
		{
			name:            "simple case",
			input:           "ICS:chain1:hello",
			expectedChainID: "chain1",
			expectedPayload: "hello",
			wantErr:         false,
		},
		{
			name:            "payload with colons",
			input:           "ICS:chain2:some:data:with:colons",
			expectedChainID: "chain2",
			expectedPayload: "some:data:with:colons",
			wantErr:         false,
		},
		{
			name:            "long chain ID",
			input:           "ICS:very-long-chain-identifier-123:payload",
			expectedChainID: "very-long-chain-identifier-123",
			expectedPayload: "payload",
			wantErr:         false,
		},
		{
			name:    "missing ICS prefix",
			input:   "XXX:chain:payload",
			wantErr: true,
		},
		{
			name:    "missing second colon",
			input:   "ICS:chain",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chainID, payload, err := ParseHeader([]byte(tt.input))

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if chainID != tt.expectedChainID {
				t.Errorf("expected chain ID %s, got %s", tt.expectedChainID, chainID)
			}

			if string(payload) != tt.expectedPayload {
				t.Errorf("expected payload %s, got %s", tt.expectedPayload, string(payload))
			}
		})
	}
}
