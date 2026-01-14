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
