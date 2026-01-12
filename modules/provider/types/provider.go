package types

import (
	"fmt"
)

// NewConsumerChain creates a new ConsumerChain instance.
func NewConsumerChain(
	chainID string,
	name string,
	version string,
	status ConsumerStatus,
	addedAt int64,
	sunsetAt int64,
	moduleAccountAddress string,
) ConsumerChain {
	return ConsumerChain{
		ChainId:              chainID,
		Name:                 name,
		Version:              version,
		Status:               status,
		AddedAt:              addedAt,
		SunsetAt:             sunsetAt,
		ModuleAccountAddress: moduleAccountAddress,
	}
}

// Validate performs basic validation of a ConsumerChain.
func (c ConsumerChain) Validate() error {
	if c.ChainId == "" {
		return ErrInvalidChainID
	}

	if c.AddedAt < 0 {
		return fmt.Errorf("added_at cannot be negative: %d", c.AddedAt)
	}

	if c.SunsetAt < 0 {
		return fmt.Errorf("sunset_at cannot be negative: %d", c.SunsetAt)
	}

	if c.Status == ConsumerStatus_CONSUMER_STATUS_UNSPECIFIED {
		return ErrInvalidConsumerStatus
	}

	if c.ModuleAccountAddress == "" {
		return fmt.Errorf("module account address cannot be empty")
	}

	return nil
}

// IsActive returns true if the consumer chain is active.
func (c ConsumerChain) IsActive() bool {
	return c.Status == ConsumerStatus_CONSUMER_STATUS_ACTIVE
}

// IsPending returns true if the consumer chain is pending.
func (c ConsumerChain) IsPending() bool {
	return c.Status == ConsumerStatus_CONSUMER_STATUS_PENDING
}

// IsSunset returns true if the consumer chain is sunset.
func (c ConsumerChain) IsSunset() bool {
	return c.Status == ConsumerStatus_CONSUMER_STATUS_SUNSET
}

// IsRemoved returns true if the consumer chain is removed.
func (c ConsumerChain) IsRemoved() bool {
	return c.Status == ConsumerStatus_CONSUMER_STATUS_REMOVED
}

// CanUpgrade returns true if the consumer chain can be upgraded.
func (c ConsumerChain) CanUpgrade() bool {
	return c.IsActive() || c.IsPending()
}

// CanSunset returns true if the consumer chain can be sunset.
func (c ConsumerChain) CanSunset() bool {
	return !c.IsSunset() && !c.IsRemoved()
}
