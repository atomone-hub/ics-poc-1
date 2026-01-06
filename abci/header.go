package abci

import (
	"bytes"
	"fmt"
)

const (
	// HeaderPrefix identifies ICS multiplexed transactions
	HeaderPrefix = "ICS:"
	HeaderLength = 4 // len("ICS:")
)

// CreateTransactionHeader creates a header for a multiplexed transaction
// Format: "ICS:<chain_id>:"
func CreateTransactionHeader(chainID string) []byte {
	return fmt.Appendf(nil, "ICS:%s:", chainID)
}

// ParseHeader extracts the chain ID and payload from a multiplexed transaction
// Format: "ICS:<chain_id>:<payload>"
// Returns: chainID, payload, error
func ParseHeader(tx []byte) (string, []byte, error) {
	if len(tx) < HeaderLength {
		return "", nil, fmt.Errorf("transaction too short")
	}

	if !bytes.HasPrefix(tx, []byte(HeaderPrefix)) {
		return "", nil, fmt.Errorf("invalid header prefix")
	}

	// Find the second colon
	rest := tx[HeaderLength:]
	idx := bytes.IndexByte(rest, ':')
	if idx == -1 {
		return "", nil, fmt.Errorf("invalid header format, missing chain separator")
	}

	chainID := string(rest[:idx])
	payload := rest[idx+1:]

	return chainID, payload, nil
}

// ValidateTransaction performs validation of a multiplexed transaction
func ValidateTransaction(tx []byte, minPayloadSize int) error {
	chainID, payload, err := ParseHeader(tx)
	if err != nil {
		return err
	}

	if chainID == "" {
		return fmt.Errorf("empty chain ID")
	}

	if len(payload) < minPayloadSize {
		return fmt.Errorf("payload too small: %d bytes (minimum %d)", len(payload), minPayloadSize)
	}

	return nil
}
