package e2e

import (
	"encoding/json"
	"fmt"
	"os"

	"cosmossdk.io/math"


	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/server"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
)

func getGenDoc(path string) (*genutiltypes.AppGenesis, error) {
	serverCtx := server.NewDefaultContext()
	config := serverCtx.Config
	config.SetRoot(path)

	genFile := config.GenesisFile()

	if _, err := os.Stat(genFile); err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}

		return &genutiltypes.AppGenesis{}, nil
	}

	var err error
	doc, err := genutiltypes.AppGenesisFromFile(genFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read genesis doc from file: %w", err)
	}

	return doc, nil
}

func modifyGenesis(cdc codec.Codec, path, moniker, amountStr string, addrAll []sdk.AccAddress, denom string) error {
	serverCtx := server.NewDefaultContext()
	config := serverCtx.Config
	config.SetRoot(path)
	config.Moniker = moniker

	//-----------------------------------------
	// Modifying auth genesis

	coins, err := sdk.ParseCoinsNormalized(amountStr)
	if err != nil {
		return fmt.Errorf("failed to parse coins: %w", err)
	}

	var balances []banktypes.Balance
	var genAccounts []*authtypes.BaseAccount
	for _, addr := range addrAll {
		balance := banktypes.Balance{Address: addr.String(), Coins: coins.Sort()}
		balances = append(balances, balance)
		genAccount := authtypes.NewBaseAccount(addr, nil, 0, 0)
		genAccounts = append(genAccounts, genAccount)
	}

	genFile := config.GenesisFile()
	appState, genDoc, err := genutiltypes.GenesisStateFromGenFile(genFile)
	if err != nil {
		return fmt.Errorf("failed to unmarshal genesis state: %w", err)
	}

	authGenState := authtypes.GetGenesisStateFromAppState(cdc, appState)
	accs, err := authtypes.UnpackAccounts(authGenState.Accounts)
	if err != nil {
		return fmt.Errorf("failed to get accounts from any: %w", err)
	}

	for _, addr := range addrAll {
		if accs.Contains(addr) {
			return fmt.Errorf("failed to add account to genesis state; account already exists: %s", addr)
		}
	}

	// Add the new account to the set of genesis accounts and sanitize the
	// accounts afterwards.
	for _, genAcct := range genAccounts {
		accs = append(accs, genAcct)
		accs = authtypes.SanitizeGenesisAccounts(accs)
	}

	genAccs, err := authtypes.PackAccounts(accs)
	if err != nil {
		return fmt.Errorf("failed to convert accounts into any's: %w", err)
	}

	authGenState.Accounts = genAccs

	authGenStateBz, err := cdc.MarshalJSON(&authGenState)
	if err != nil {
		return fmt.Errorf("failed to marshal auth genesis state: %w", err)
	}
	appState[authtypes.ModuleName] = authGenStateBz

	//-----------------------------------------
	// Modifying bank genesis

	bankGenState := banktypes.GetGenesisStateFromAppState(cdc, appState)
	bankGenState.Balances = append(bankGenState.Balances, balances...)
	bankGenState.Balances = banktypes.SanitizeGenesisBalances(bankGenState.Balances)

	bankGenStateBz, err := cdc.MarshalJSON(bankGenState)
	if err != nil {
		return fmt.Errorf("failed to marshal bank genesis state: %w", err)
	}
	appState[banktypes.ModuleName] = bankGenStateBz

	// Modifying staking genesis

	stakingGenState := stakingtypes.GetGenesisStateFromAppState(cdc, appState)
	stakingGenState.Params.BondDenom = denom
	stakingGenStateBz, err := cdc.MarshalJSON(stakingGenState)
	if err != nil {
		return fmt.Errorf("failed to marshal staking genesis state: %s", err)
	}
	appState[stakingtypes.ModuleName] = stakingGenStateBz

	var mintGenState minttypes.GenesisState
	cdc.MustUnmarshalJSON(appState[minttypes.ModuleName], &mintGenState)
	mintGenState.Params.MintDenom = denom
	mintGenStateBz, err := cdc.MarshalJSON(&mintGenState)
	if err != nil {
		return fmt.Errorf("failed to marshal mint genesis state: %s", err)
	}
	appState[minttypes.ModuleName] = mintGenStateBz

	//-----------------------------------------
	// Modifying gov genesis

	// Refactor to separate method
	govParams := govv1.DefaultParams()
	govParams.Threshold = "0.000000000000000001"
	govParams.LawThreshold = "0.000000000000000001"
	govParams.ConstitutionAmendmentThreshold = "0.000000000000000001"
	govParams.QuorumRange.Min = "0.2"
	govParams.QuorumRange.Max = "0.8"
	participationEma := "0.25"
	govParams.ConstitutionAmendmentQuorumRange.Min = "0.2"
	govParams.ConstitutionAmendmentQuorumRange.Max = "0.8"
	govParams.LawQuorumRange.Min = "0.2"
	govParams.LawQuorumRange.Max = "0.8"
	minGovernorSelfDelegation, _ := math.NewIntFromString("10000000")
	govParams.MinGovernorSelfDelegation = minGovernorSelfDelegation.String()
	

	govGenState := govv1.NewGenesisState(1,
		participationEma, participationEma, participationEma,
		govParams,
	)
	govGenStateBz, err := cdc.MarshalJSON(govGenState)
	if err != nil {
		return fmt.Errorf("failed to marshal gov genesis state: %w", err)
	}
	appState[govtypes.ModuleName] = govGenStateBz

	//-----------------------------------------
	// Record final genesis

	appStateJSON, err := json.Marshal(appState)
	if err != nil {
		return fmt.Errorf("failed to marshal application genesis state: %w", err)
	}
	genDoc.AppState = appStateJSON

	return genutil.ExportGenesisFile(genDoc, genFile)
}
