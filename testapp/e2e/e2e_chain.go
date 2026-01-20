package e2e

import (
	"fmt"
	"os"

	providerApp "github.com/atomone-hub/ics-poc-1/testapp/provider"
	consumerApp "github.com/atomone-hub/ics-poc-1/testapp/consumer"

	"cosmossdk.io/log"
	tmrand "github.com/cometbft/cometbft/libs/rand"

	dbm "github.com/cosmos/cosmos-db"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
)

const (
	keyringPassphrase = "testpassphrase"
	keyringAppName    = "testnet"
)

type chain struct {
	dataDir          string
	id               string
	validators       []*validator
	accounts         []*account //nolint:unused
	multiSigAccounts []*multiSigAccount
	// initial accounts in genesis
	genesisAccounts        []*account
	genesisVestingAccounts map[string]sdk.AccAddress

	// codecs and chain config
	cdc      codec.Codec
	txConfig client.TxConfig

}

func newProviderChain() (*chain, error) {
	tmpDir, err := os.MkdirTemp("", "ics-e2e-testnet-")
	if err != nil {
		return nil, err
	}

	tempApp := providerApp.New(log.NewNopLogger(), dbm.NewMemDB(), nil, true, simtestutil.NewAppOptionsWithFlagHome(tmpDir))

	return &chain{
		id:       "provider-chain-" + tmrand.Str(6),
		dataDir:  tmpDir,
		cdc:      tempApp.AppCodec(),
		txConfig: tempApp.GetTxConfig(),
	}, nil
}

func newConsumerChain() (*chain, error) {
	tmpDir, err := os.MkdirTemp("", "ics-e2e-testnet-")
	if err != nil {
		return nil, err
	}
	tempApp := consumerApp.New(log.NewNopLogger(), dbm.NewMemDB(), nil, true, simtestutil.NewAppOptionsWithFlagHome(tmpDir))

	return &chain{
		id:       "consumer-chain-" + tmrand.Str(6),
		dataDir:  tmpDir,
		cdc:      tempApp.AppCodec(),
		txConfig: tempApp.GetTxConfig(),
	}, nil
}

func (c *chain) configDir() string {
	return fmt.Sprintf("%s/%s", c.dataDir, c.id)
}

func (c *chain) createAndInitValidators(count int) error {
	for i := 0; i < count; i++ {
		node := c.createValidator(i)

		// generate genesis files
		if err := node.init(); err != nil {
			return err
		}

		c.validators = append(c.validators, node)

		// create keys
		if err := node.createKey("val"); err != nil {
			return err
		}
		if err := node.createNodeKey(); err != nil {
			return err
		}
		if err := node.createConsensusKey(); err != nil {
			return err
		}
	}

	return nil
}

func (c *chain) createValidator(index int) *validator {
	return &validator{
		chain:   c,
		index:   index,
		moniker: fmt.Sprintf("%s-%d", c.id, index),
	}
}
