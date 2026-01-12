package types

import (
	"fmt"

	"cosmossdk.io/math"
)

// NewParams creates a new Params instance.
func NewParams(feesPerBlock math.Int, downtimeSlashingWindow int64) Params {
	return Params{
		FeesPerBlock:           feesPerBlock,
		DowntimeSlashingWindow: downtimeSlashingWindow,
	}
}

// DefaultParams returns a default set of parameters.
func DefaultParams() Params {
	return NewParams(
		math.NewInt(1000), // Default fees per block
		int64(10000),      // Default downtime slashing window (blocks)
	)
}

// Validate validates the set of params.
func (p Params) Validate() error {
	if p.FeesPerBlock.IsNil() {
		return fmt.Errorf("fees per block cannot be nil")
	}
	if p.FeesPerBlock.IsNegative() {
		return fmt.Errorf("fees per block cannot be negative: %s", p.FeesPerBlock)
	}

	if p.DowntimeSlashingWindow < 0 {
		return fmt.Errorf("downtime slashing window cannot be negative: %d", p.DowntimeSlashingWindow)
	}

	return nil
}
