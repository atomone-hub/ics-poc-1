package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/core/address"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/atomone-hub/ics-poc-1/modules/provider/keeper"
	module "github.com/atomone-hub/ics-poc-1/modules/provider/module"
	icstestutil "github.com/atomone-hub/ics-poc-1/modules/provider/testutil"
	"github.com/atomone-hub/ics-poc-1/modules/provider/types"
)

type fixture struct {
	ctx            context.Context
	keeper         keeper.Keeper
	addressCodec   address.Codec
	mockController *gomock.Controller
	authKeeper     *icstestutil.MockAuthKeeper
	bankKeeper     *icstestutil.MockBankKeeper
	stakingKeeper  *icstestutil.MockStakingKeeper
}

func initFixture(t *testing.T) *fixture {
	t.Helper()

	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	addressCodec := addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix())
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)

	storeService := runtime.NewKVStoreService(storeKey)
	ctx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx

	authority := authtypes.NewModuleAddress(types.GovModuleName)

	ctrl := gomock.NewController(t)
	bankKeeper := icstestutil.NewMockBankKeeper(ctrl)
	stakingKeeper := icstestutil.NewMockStakingKeeper(ctrl)
	accountKeeper := icstestutil.NewMockAuthKeeper(ctrl)

	k := keeper.NewKeeper(
		storeService,
		encCfg.Codec,
		addressCodec,
		authority,
		accountKeeper, // auth keeper
		bankKeeper,    // bank keeper
		stakingKeeper, // staking keeper
	)

	// Initialize params
	if err := k.Params.Set(ctx, types.DefaultParams()); err != nil {
		t.Fatalf("failed to set params: %v", err)
	}

	return &fixture{
		mockController: ctrl,
		ctx:            ctx,
		keeper:         k,
		addressCodec:   addressCodec,
		authKeeper:     accountKeeper,
		bankKeeper:     bankKeeper,
		stakingKeeper:  stakingKeeper,
	}
}

func TestCollectFeesFromConsumers(t *testing.T) {
	// Create consumer chain
	DefaultConsumer := types.NewConsumerChain(
		"consumer-1",
		"Consumer 1",
		"v1.0.0",
		types.ConsumerStatus_CONSUMER_STATUS_ACTIVE,
		100,
		0,
		"",
	)
	defaultModuleAddr := sdk.AccAddress("consumer-1-module-account")

	testCases := []struct {
		name              string
		feesPerBlock      math.Int
		setupConsumers    func(t *testing.T, f *fixture)
		expectedTotalFees math.Int
		expectError       bool
	}{
		{
			name:         "collect fees from single active consumer with sufficient balance",
			feesPerBlock: math.NewInt(1000),
			setupConsumers: func(t *testing.T, f *fixture) {
				consumer := DefaultConsumer
				moduleAddr := defaultModuleAddr
				// Mock auth keeper for module account creation
				f.authKeeper.EXPECT().GetModuleAddress("consumer_consumer-1").Return(moduleAddr).AnyTimes()
				f.authKeeper.EXPECT().NewAccountWithAddress(gomock.Any(), moduleAddr).Return(authtypes.NewBaseAccountWithAddress(moduleAddr)).AnyTimes()
				f.authKeeper.EXPECT().SetAccount(gomock.Any(), gomock.Any()).AnyTimes()

				// Create consumer module account
				consumerAddr, err := f.keeper.CreateConsumerModuleAccount(f.ctx, consumer.ChainId)
				require.NoError(t, err)
				consumer.ModuleAccountAddress = consumerAddr

				// Store consumer chain
				require.NoError(t, f.keeper.ConsumerChains.Set(f.ctx, consumer.ChainId, consumer))

				params, err := f.keeper.Params.Get(f.ctx)
				require.NoError(t, err)

				// Mock bank keeper
				addr, _ := f.addressCodec.StringToBytes(consumerAddr)
				f.bankKeeper.EXPECT().GetBalance(gomock.Any(), addr, params.FeeDenom).Return(sdk.NewCoin(params.FeeDenom, math.NewInt(10000)))
				f.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
					gomock.Any(),
					"consumer_consumer-1",
					"fee_collector",
					sdk.NewCoins(sdk.NewCoin(params.FeeDenom, math.NewInt(1000))),
				).Return(nil)
			},
			expectedTotalFees: math.NewInt(1000),
			expectError:       false,
		},
		{
			name:         "skip consumer with insufficient balance",
			feesPerBlock: math.NewInt(1000),
			setupConsumers: func(t *testing.T, f *fixture) {
				consumer := DefaultConsumer
				moduleAddr := defaultModuleAddr

				f.authKeeper.EXPECT().GetModuleAddress("consumer_consumer-1").Return(moduleAddr).AnyTimes()
				f.authKeeper.EXPECT().NewAccountWithAddress(gomock.Any(), moduleAddr).Return(authtypes.NewBaseAccountWithAddress(moduleAddr)).AnyTimes()
				f.authKeeper.EXPECT().SetAccount(gomock.Any(), gomock.Any()).AnyTimes()

				consumerAddr, err := f.keeper.CreateConsumerModuleAccount(f.ctx, consumer.ChainId)
				require.NoError(t, err)
				consumer.ModuleAccountAddress = consumerAddr
				require.NoError(t, f.keeper.ConsumerChains.Set(f.ctx, consumer.ChainId, consumer))

				params, err := f.keeper.Params.Get(f.ctx)
				require.NoError(t, err)

				addr, _ := f.addressCodec.StringToBytes(consumerAddr)
				// Insufficient balance - less than feesPerBlock
				f.bankKeeper.EXPECT().GetBalance(gomock.Any(), addr, params.FeeDenom).Return(sdk.NewCoin(params.FeeDenom, math.NewInt(100)))
			},
			expectedTotalFees: math.ZeroInt(),
			expectError:       false,
		},
		{
			name:         "skip inactive consumers",
			feesPerBlock: math.NewInt(1000),
			setupConsumers: func(t *testing.T, f *fixture) {
				consumer := DefaultConsumer
				moduleAddr := defaultModuleAddr
				// Create inactive consumer (PENDING status)
				consumer.Status = types.ConsumerStatus_CONSUMER_STATUS_PENDING
				f.authKeeper.EXPECT().GetModuleAddress("consumer_consumer-1").Return(moduleAddr).AnyTimes()
				f.authKeeper.EXPECT().NewAccountWithAddress(gomock.Any(), moduleAddr).Return(authtypes.NewBaseAccountWithAddress(moduleAddr)).AnyTimes()
				f.authKeeper.EXPECT().SetAccount(gomock.Any(), gomock.Any()).AnyTimes()

				consumerAddr, err := f.keeper.CreateConsumerModuleAccount(f.ctx, consumer.ChainId)
				require.NoError(t, err)
				consumer.ModuleAccountAddress = consumerAddr
				require.NoError(t, f.keeper.ConsumerChains.Set(f.ctx, consumer.ChainId, consumer))
			},
			expectedTotalFees: math.ZeroInt(),
			expectError:       false,
		},
		{
			name:         "collect from multiple active consumers",
			feesPerBlock: math.NewInt(500),
			setupConsumers: func(t *testing.T, f *fixture) {
				consumer1 := DefaultConsumer
				moduleAddr1 := defaultModuleAddr
				f.authKeeper.EXPECT().GetModuleAddress("consumer_consumer-1").Return(moduleAddr1).AnyTimes()
				f.authKeeper.EXPECT().NewAccountWithAddress(gomock.Any(), moduleAddr1).Return(authtypes.NewBaseAccountWithAddress(moduleAddr1)).AnyTimes()
				f.authKeeper.EXPECT().SetAccount(gomock.Any(), gomock.Any()).AnyTimes()

				consumerAddr1, err := f.keeper.CreateConsumerModuleAccount(f.ctx, consumer1.ChainId)
				require.NoError(t, err)
				consumer1.ModuleAccountAddress = consumerAddr1
				require.NoError(t, f.keeper.ConsumerChains.Set(f.ctx, consumer1.ChainId, consumer1))

				params, err := f.keeper.Params.Get(f.ctx)
				require.NoError(t, err)

				addr1, _ := f.addressCodec.StringToBytes(consumerAddr1)
				f.bankKeeper.EXPECT().GetBalance(gomock.Any(), addr1, params.FeeDenom).Return(sdk.NewCoin(params.FeeDenom, math.NewInt(5000)))
				f.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
					gomock.Any(),
					"consumer_consumer-1",
					"fee_collector",
					sdk.NewCoins(sdk.NewCoin(params.FeeDenom, math.NewInt(500))),
				).Return(nil)

				// Create second consumer
				consumer2 := types.NewConsumerChain(
					"consumer-2",
					"Consumer 2",
					"v1.0.0",
					types.ConsumerStatus_CONSUMER_STATUS_ACTIVE,
					150,
					0,
					"",
				)

				moduleAddr2 := sdk.AccAddress("consumer-2-module-account")
				f.authKeeper.EXPECT().GetModuleAddress("consumer_consumer-2").Return(moduleAddr2).AnyTimes()
				f.authKeeper.EXPECT().NewAccountWithAddress(gomock.Any(), moduleAddr2).Return(authtypes.NewBaseAccountWithAddress(moduleAddr2)).AnyTimes()

				consumerAddr2, err := f.keeper.CreateConsumerModuleAccount(f.ctx, consumer2.ChainId)
				require.NoError(t, err)
				consumer2.ModuleAccountAddress = consumerAddr2
				require.NoError(t, f.keeper.ConsumerChains.Set(f.ctx, consumer2.ChainId, consumer2))

				addr2, _ := f.addressCodec.StringToBytes(consumerAddr2)
				f.bankKeeper.EXPECT().GetBalance(gomock.Any(), addr2, params.FeeDenom).Return(sdk.NewCoin(params.FeeDenom, math.NewInt(3000)))
				f.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
					gomock.Any(),
					"consumer_consumer-2",
					"fee_collector",
					sdk.NewCoins(sdk.NewCoin(params.FeeDenom, math.NewInt(500))),
				).Return(nil)
			},
			expectedTotalFees: math.NewInt(1000),
			expectError:       false,
		},
		{
			name:         "no consumers registered",
			feesPerBlock: math.NewInt(1000),
			setupConsumers: func(t *testing.T, f *fixture) {
				// No consumers to setup
			},
			expectedTotalFees: math.ZeroInt(),
			expectError:       false,
		},
		{
			name:         "bank transfer fails",
			feesPerBlock: math.NewInt(1000),
			setupConsumers: func(t *testing.T, f *fixture) {
				consumer := DefaultConsumer
				moduleAddr := defaultModuleAddr
				f.authKeeper.EXPECT().GetModuleAddress("consumer_consumer-1").Return(moduleAddr).AnyTimes()
				f.authKeeper.EXPECT().NewAccountWithAddress(gomock.Any(), moduleAddr).Return(authtypes.NewBaseAccountWithAddress(moduleAddr)).AnyTimes()
				f.authKeeper.EXPECT().SetAccount(gomock.Any(), gomock.Any()).AnyTimes()

				consumerAddr, err := f.keeper.CreateConsumerModuleAccount(f.ctx, consumer.ChainId)
				require.NoError(t, err)
				consumer.ModuleAccountAddress = consumerAddr
				require.NoError(t, f.keeper.ConsumerChains.Set(f.ctx, consumer.ChainId, consumer))

				params, err := f.keeper.Params.Get(f.ctx)
				require.NoError(t, err)

				addr, _ := f.addressCodec.StringToBytes(consumerAddr)
				f.bankKeeper.EXPECT().GetBalance(gomock.Any(), addr, params.FeeDenom).Return(sdk.NewCoin(params.FeeDenom, math.NewInt(10000)))
				f.bankKeeper.EXPECT().SendCoinsFromModuleToModule(
					gomock.Any(),
					"consumer_consumer-1",
					"fee_collector",
					gomock.Any(),
				).Return(sdkerrors.ErrInsufficientFunds)
			},
			expectedTotalFees: math.ZeroInt(),
			expectError:       true,
		},
		{
			name:         "invalid module account address",
			feesPerBlock: math.NewInt(1000),
			setupConsumers: func(t *testing.T, f *fixture) {
				consumer := DefaultConsumer
				consumer.ModuleAccountAddress = "invalid-address"
				require.NoError(t, f.keeper.ConsumerChains.Set(f.ctx, consumer.ChainId, consumer))
			},
			expectedTotalFees: math.ZeroInt(),
			expectError:       true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			f := initFixture(t)

			// Setup test-specific mocks
			tc.setupConsumers(t, f)

			// Execute collection
			totalFeesCollected, err := f.keeper.CollectFeesFromConsumers(f.ctx, tc.feesPerBlock)

			if tc.expectError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.expectedTotalFees.String(), totalFeesCollected.String(), "total fees collected mismatch")
		})
	}
}

func TestDistributeFeesToValidators(t *testing.T) {
	f := initFixture(t)

	testCases := []struct {
		name          string
		totalFees     math.Int
		setupMocks    func(t *testing.T, f *fixture)
		expectError   bool
		errorContains string
	}{
		{
			name:      "distribute to single validator",
			totalFees: math.NewInt(1000),
			setupMocks: func(t *testing.T, f *fixture) {
				// Create validator using gomock
				valAddr := sdk.AccAddress("validator1-address-12345")
				valAddrBech32, _ := sdk.Bech32ifyAddressBytes(sdk.GetConfig().GetBech32AccountAddrPrefix(), valAddr)

				mockVal := icstestutil.NewMockValidatorI(gomock.NewController(t))
				mockVal.EXPECT().GetOperator().Return(valAddrBech32).AnyTimes()
				mockVal.EXPECT().GetConsensusPower(gomock.Any()).Return(int64(100)).AnyTimes()

				params, err := f.keeper.Params.Get(f.ctx)
				require.NoError(t, err)

				f.stakingKeeper.EXPECT().GetBondedValidatorsByPower(gomock.Any()).Return([]stakingtypes.ValidatorI{mockVal}, nil)
				f.bankKeeper.EXPECT().SendCoinsFromModuleToAccount(
					gomock.Any(),
					"fee_collector",
					valAddr,
					sdk.NewCoins(sdk.NewCoin(params.FeeDenom, math.NewInt(1000))),
				).Return(nil)
			},
			expectError: false,
		},
		{
			name:      "distribute to multiple validators proportionally",
			totalFees: math.NewInt(1000),
			setupMocks: func(t *testing.T, f *fixture) {
				// Create validators
				valAddr1 := sdk.AccAddress("validator1-address-12345")
				valAddrBech32_1, _ := sdk.Bech32ifyAddressBytes(sdk.GetConfig().GetBech32AccountAddrPrefix(), valAddr1)
				mockVal1 := icstestutil.NewMockValidatorI(f.mockController)
				mockVal1.EXPECT().GetOperator().Return(valAddrBech32_1).AnyTimes()
				mockVal1.EXPECT().GetConsensusPower(gomock.Any()).Return(int64(60)).AnyTimes()

				valAddr2 := sdk.AccAddress("validator2-address-12345")
				valAddrBech32_2, _ := sdk.Bech32ifyAddressBytes(sdk.GetConfig().GetBech32AccountAddrPrefix(), valAddr2)
				mockVal2 := icstestutil.NewMockValidatorI(f.mockController)
				mockVal2.EXPECT().GetOperator().Return(valAddrBech32_2).AnyTimes()
				mockVal2.EXPECT().GetConsensusPower(gomock.Any()).Return(int64(30)).AnyTimes()

				valAddr3 := sdk.AccAddress("validator3-address-12345")
				valAddrBech32_3, _ := sdk.Bech32ifyAddressBytes(sdk.GetConfig().GetBech32AccountAddrPrefix(), valAddr3)
				mockVal3 := icstestutil.NewMockValidatorI(f.mockController)
				mockVal3.EXPECT().GetOperator().Return(valAddrBech32_3).AnyTimes()
				mockVal3.EXPECT().GetConsensusPower(gomock.Any()).Return(int64(10)).AnyTimes()

				f.stakingKeeper.EXPECT().GetBondedValidatorsByPower(gomock.Any()).Return(
					[]stakingtypes.ValidatorI{mockVal1, mockVal2, mockVal3}, nil)

				params, err := f.keeper.Params.Get(f.ctx)
				require.NoError(t, err)

				f.bankKeeper.EXPECT().SendCoinsFromModuleToAccount(gomock.Any(), "fee_collector", valAddr1, sdk.NewCoins(sdk.NewCoin(params.FeeDenom, math.NewInt(600)))).Return(nil)
				f.bankKeeper.EXPECT().SendCoinsFromModuleToAccount(gomock.Any(), "fee_collector", valAddr2, sdk.NewCoins(sdk.NewCoin(params.FeeDenom, math.NewInt(300)))).Return(nil)
				f.bankKeeper.EXPECT().SendCoinsFromModuleToAccount(gomock.Any(), "fee_collector", valAddr3, sdk.NewCoins(sdk.NewCoin(params.FeeDenom, math.NewInt(100)))).Return(nil)
			},
			expectError: false,
		},
		{
			name:      "no validators",
			totalFees: math.NewInt(1000),
			setupMocks: func(t *testing.T, f *fixture) {
				f.stakingKeeper.EXPECT().GetBondedValidatorsByPower(gomock.Any()).Return([]stakingtypes.ValidatorI{}, nil)
			},
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup test-specific mocks
			tc.setupMocks(t, f)

			// Execute distribution
			err := f.keeper.DistributeFeesToValidators(f.ctx, tc.totalFees)

			if !tc.expectError {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			if tc.errorContains != "" {
				require.Contains(t, err.Error(), tc.errorContains)
			}
		})
	}
}
