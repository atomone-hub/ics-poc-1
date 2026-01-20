package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	corestore "cosmossdk.io/core/store"
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/atomone-hub/ics-poc-1/modules/provider/types"
)

type Keeper struct {
	storeService corestore.KVStoreService
	cdc          codec.Codec
	addressCodec address.Codec
	// Address capable of executing a MsgUpdateParams message.
	// Typically, this should be the x/gov module account.
	authority []byte

	authKeeper    types.AuthKeeper
	bankKeeper    types.BankKeeper
	stakingKeeper types.StakingKeeper

	Schema         collections.Schema
	Params         collections.Item[types.Params]
	ConsumerChains collections.Map[string, types.ConsumerChain]
}

func NewKeeper(
	storeService corestore.KVStoreService,
	cdc codec.Codec,
	addressCodec address.Codec,
	authority []byte,
	authKeeper types.AuthKeeper,
	bankKeeper types.BankKeeper,
	stakingKeeper types.StakingKeeper,
) Keeper {
	if _, err := addressCodec.BytesToString(authority); err != nil {
		panic(fmt.Sprintf("invalid authority address %s: %s", authority, err))
	}

	sb := collections.NewSchemaBuilder(storeService)

	k := Keeper{
		storeService:  storeService,
		cdc:           cdc,
		addressCodec:  addressCodec,
		authority:     authority,
		authKeeper:    authKeeper,
		bankKeeper:    bankKeeper,
		stakingKeeper: stakingKeeper,

		Params:         collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
		ConsumerChains: collections.NewMap(sb, types.ConsumerChainsKey, "consumer_chains", collections.StringKey, codec.CollValue[types.ConsumerChain](cdc)),
	}

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema

	return k
}

// GetAuthority returns the module's authority.
func (k Keeper) GetAuthority() []byte {
	return k.authority
}

// IterateConsumerChains iterates over all consumer chains.
func (k Keeper) IterateConsumerChains(ctx context.Context, cb func(consumer types.ConsumerChain) (stop bool, err error)) error {
	return k.ConsumerChains.Walk(ctx, nil, func(key string, value types.ConsumerChain) (stop bool, err error) {
		return cb(value)
	})
}

// GetAllConsumerChains retrieves all consumer chains.
func (k Keeper) GetAllConsumerChains(ctx context.Context) ([]types.ConsumerChain, error) {
	consumers := []types.ConsumerChain{}
	err := k.IterateConsumerChains(ctx, func(consumer types.ConsumerChain) (bool, error) {
		consumers = append(consumers, consumer)
		return false, nil
	})
	return consumers, err
}

// CreateConsumerModuleAccount creates a module account for a consumer chain.
func (k Keeper) CreateConsumerModuleAccount(ctx context.Context, chainID string) (string, error) {
	accountName := types.GetConsumerModuleAccountName(chainID)

	// Check if account already exists
	addr := k.authKeeper.GetModuleAddress(accountName)
	if addr != nil {
		// Account already exists, return its address
		addrStr, err := k.addressCodec.BytesToString(addr)
		if err != nil {
			return "", err
		}
		return addrStr, nil
	}

	// Create the module account
	k.authKeeper.SetAccount(ctx, k.authKeeper.NewAccountWithAddress(ctx, addr))

	addrStr, err := k.addressCodec.BytesToString(addr)
	if err != nil {
		return "", err
	}

	return addrStr, nil
}

// CollectFeesFromConsumers iterates through all consumer chains and collects fees from active ones.
func (k Keeper) CollectFeesFromConsumers(ctx context.Context, feesPerBlock sdk.Coin) (sdk.Coin, error) {
	totalFeesCollected := sdk.NewCoin(feesPerBlock.Denom, math.ZeroInt())
	err := k.IterateConsumerChains(ctx, func(consumer types.ConsumerChain) (bool, error) {
		// Only process active and overdue chains (overdue chains get another chance to pay)
		if !consumer.IsActive() && !consumer.IsOverdue() {
			return false, nil
		}

		// Get consumer module account address
		consumerAddr, err := k.addressCodec.StringToBytes(consumer.ModuleAccountAddress)
		if err != nil {
			return true, fmt.Errorf("failed to parse module account address for chain %s: %w", consumer.ChainId, err)
		}

		// Check if account has sufficient balance
		balance := k.bankKeeper.GetBalance(ctx, consumerAddr, feesPerBlock.Denom)
		if balance.IsLT(feesPerBlock) {
			// Set chain overdue status if it doesn't have enough funds
			consumer.Status = types.ConsumerStatus_CONSUMER_STATUS_OVERDUE
		} else {
			// Deduct fees from consumer module account
			feeCoins := sdk.NewCoins(feesPerBlock)
			consumerModuleName := types.GetConsumerModuleAccountName(consumer.ChainId)

			// Send fees to a temporary collection account (fee_collector)
			if err := k.bankKeeper.SendCoinsFromModuleToModule(ctx, consumerModuleName, "fee_collector", feeCoins); err != nil {
				return true, fmt.Errorf("failed to collect fees from chain %s: %w", consumer.ChainId, err)
			}

			totalFeesCollected = totalFeesCollected.Add(feesPerBlock)
			// Chain successfully paid, set status back to active
			consumer.Status = types.ConsumerStatus_CONSUMER_STATUS_ACTIVE
		}

		return false, k.ConsumerChains.Set(ctx, consumer.ChainId, consumer)
	})
	return totalFeesCollected, err
}

// DistributeFeesToValidators distributes collected fees to the validator set.
func (k Keeper) DistributeFeesToValidators(ctx context.Context, totalFees sdk.Coin) error {
	// Get the bonded validators
	validators, err := k.stakingKeeper.GetBondedValidatorsByPower(ctx)
	if err != nil {
		return fmt.Errorf("failed to get bonded validators: %w", err)
	}

	if len(validators) == 0 {
		// No validators to distribute to, return early
		return nil
	}

	// Calculate total voting power
	totalPower := math.ZeroInt()
	for _, val := range validators {
		totalPower = totalPower.Add(math.NewInt(val.GetConsensusPower(sdk.DefaultPowerReduction)))
	}

	if totalPower.IsZero() {
		return fmt.Errorf("total voting power is zero")
	}

	// Distribute fees proportionally to each validator based on voting power
	for _, val := range validators {
		// Get validator operator address
		valAddr, err := k.addressCodec.StringToBytes(val.GetOperator())
		if err != nil {
			return fmt.Errorf("failed to parse validator address: %w", err)
		}

		// Calculate validator's share: (validatorPower / totalPower) * totalFees
		valPower := math.NewInt(val.GetConsensusPower(sdk.DefaultPowerReduction))
		valShare := totalFees.Amount.Mul(valPower).Quo(totalPower)
		if valShare.IsZero() {
			continue
		}

		// Send coins from fee_collector to validator account
		coins := sdk.NewCoins(sdk.NewCoin(totalFees.Denom, valShare))
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, "fee_collector", valAddr, coins); err != nil {
			return fmt.Errorf("failed to send fees to validator %s: %w", val.GetOperator(), err)
		}
	}

	return nil
}
