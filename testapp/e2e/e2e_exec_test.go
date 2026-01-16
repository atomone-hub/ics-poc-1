package e2e

import (
	"bytes"
	"context"
	"time"

	"fmt"
	"strings"
	"github.com/ory/dockertest/v3/docker"
	"github.com/cosmos/cosmos-sdk/codec"
	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

const (
	flagFrom            = "from"
	flagHome            = "home"
	flagFees            = "fees"
	flagGas             = "gas"
	flagOutput          = "output"
	flagChainID         = "chain-id"
	flagSpendLimit      = "spend-limit"
	flagGasAdjustment   = "gas-adjustment"
	flagFeeGranter      = "fee-granter"
	flagBroadcastMode   = "broadcast-mode"
	flagKeyringBackend  = "keyring-backend"
	flagAllowedMessages = "allowed-messages"
	flagMultisig        = "multisig"
	flagOutputDocument  = "output-document"
)

type flagOption func(map[string]interface{})

// withKeyValue add a new flag to command

func withKeyValue(key string, value interface{}) flagOption {
	return func(o map[string]interface{}) {
		o[key] = value
	}
}

func applyOptions(chainID string, options []flagOption) map[string]interface{} {
	opts := map[string]interface{}{
		flagKeyringBackend: "test",
		flagOutput:         "json",
		flagGas:            "auto",
		flagFrom:           "alice",
		flagBroadcastMode:  "sync",
		flagGasAdjustment:  "1.5",
		flagChainID:        chainID,
		flagHome:           providerHomePath,
		flagFees:           standardFees.String(),
	}
	for _, apply := range options {
		apply(opts)
	}
	return opts
}

func (s *IntegrationTestSuite) execEncode(
	c *chain,
	txPath string,
	opt ...flagOption,
) string {
	opts := applyOptions(c.id, opt)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	s.T().Logf("%s - Executing atomoned encoding with %v", c.id, txPath)
	atomoneCommand := []string{
		providerBinary,
		txCommand,
		"encode",
		txPath,
	}
	for flag, value := range opts {
		atomoneCommand = append(atomoneCommand, fmt.Sprintf("--%s=%v", flag, value))
	}

	var encoded string
	s.executeTxCommand(ctx, c, atomoneCommand, 0, func(stdOut []byte, stdErr []byte) error {
		if stdErr != nil {
			return fmt.Errorf("stdErr: %s", string(stdErr))
		}
		encoded = strings.TrimSuffix(string(stdOut), "\n")
		return nil
	})
	s.T().Logf("successfully encode with %v", txPath)
	return encoded
}

func (s *IntegrationTestSuite) execDecode(
	c *chain,
	txPath string,
	opt ...flagOption,
) string {
	opts := applyOptions(c.id, opt)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	s.T().Logf("%s - Executing atomoned decoding with %v", c.id, txPath)
	atomoneCommand := []string{
		providerBinary,
		txCommand,
		"decode",
		txPath,
	}
	for flag, value := range opts {
		atomoneCommand = append(atomoneCommand, fmt.Sprintf("--%s=%v", flag, value))
	}

	var decoded string
	s.executeTxCommand(ctx, c, atomoneCommand, 0, func(stdOut []byte, stdErr []byte) error {
		if stdErr != nil {
			return fmt.Errorf("stderr=%s", stdErr)
		}
		decoded = strings.TrimSuffix(string(stdOut), "\n")
		return nil
	})
	s.T().Logf("successfully decode %v", txPath)
	return decoded
}

func (s *IntegrationTestSuite) executeTxCommand(ctx context.Context, c *chain, atomoneCommand []string, valIdx int, validation func([]byte, []byte) error) int {
	if validation == nil {
		validation = s.defaultExecValidation(s.provider, 0, nil)
	}
	var (
		outBuf bytes.Buffer
		errBuf bytes.Buffer
	)
	exec, err := s.dkrPool.Client.CreateExec(docker.CreateExecOptions{
		Context:      ctx,
		AttachStdout: true,
		AttachStderr: true,
		Container:    s.valResources[c.id][valIdx].Container.ID,
		User:         "nonroot",
		Cmd:          atomoneCommand,
	})
	s.Require().NoError(err)

	err = s.dkrPool.Client.StartExec(exec.ID, docker.StartExecOptions{
		Context:      ctx,
		Detach:       false,
		OutputStream: &outBuf,
		ErrorStream:  &errBuf,
	})
	s.Require().NoError(err)

	stdOut := outBuf.Bytes()
	stdErr := errBuf.Bytes()
	s.Require().NoError(validation(stdOut, stdErr),
		"Exec validation failed stdout: %s, stderr: %s",
		string(stdOut), string(stdErr))

	var txResp sdk.TxResponse
	if err := s.cdc.UnmarshalJSON(stdOut, &txResp); err != nil {
		return 0
	}
	endpoint := fmt.Sprintf("http://%s", s.valResources[c.id][valIdx].GetHostPort("1317/tcp"))
	height, err := s.queryTx(endpoint, txResp.TxHash, nil)
	if err != nil {
		s.Require().FailNowf("Failed query of Tx height", "err: %s, stdout: %s, stderr: %s",
			err, string(stdOut), string(stdErr))
	}
	return height
}

func (s *IntegrationTestSuite) defaultExecValidation(chain *chain, valIdx int, msgResp codec.ProtoMarshaler) func([]byte, []byte) error {
	return func(stdOut []byte, stdErr []byte) error {
		var txResp sdk.TxResponse
		if err := s.cdc.UnmarshalJSON(stdOut, &txResp); err != nil {
			return err
		}
		if strings.Contains(txResp.String(), "code: 0") || txResp.Code == 0 {
			endpoint := fmt.Sprintf("http://%s", s.valResources[chain.id][valIdx].GetHostPort("1317/tcp"))
			return s.waitTx(endpoint, txResp.TxHash, msgResp)
		}
		return fmt.Errorf("tx error : %s", txResp.String())
	}
}

func (s *IntegrationTestSuite) expectErrExecValidation(chain *chain, valIdx int, expectErr bool) func([]byte, []byte) error {
	return func(stdOut []byte, stdErr []byte) error {
		err := s.defaultExecValidation(chain, valIdx, nil)(stdOut, stdErr)
		if expectErr && err != nil {
			return nil
		}
		return err
	}
}

func (s *IntegrationTestSuite) rpcClient(c *chain, valIdx int) *rpchttp.HTTP {
	rc, err := rpchttp.New("tcp://"+s.valResources[c.id][valIdx].GetHostPort("26657/tcp"), "/websocket")
	s.Require().NoError(err)
	return rc
}


func (s *IntegrationTestSuite) execBankSend(
	c *chain,
	valIdx int,
	from,
	to,
	amt string,
	expectErr bool,
	opt ...flagOption,
) {
	// TODO remove the hardcode opt after refactor, all methods should accept custom flags
	opt = append(opt, withKeyValue(flagFrom, from))
	opts := applyOptions(c.id, opt)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	s.T().Logf("sending %s tokens from %s to %s on chain %s", amt, from, to, c.id)

	atomoneCommand := []string{
		providerBinary,
		txCommand,
		banktypes.ModuleName,
		"send",
		from,
		to,
		amt,
		"-y",
	}
	for flag, value := range opts {
		atomoneCommand = append(atomoneCommand, fmt.Sprintf("--%s=%v", flag, value))
	}

	s.executeTxCommand(ctx, c, atomoneCommand, valIdx, s.expectErrExecValidation(c, valIdx, expectErr))
}

func (s *IntegrationTestSuite) execBankMultiSend(
	c *chain,
	valIdx int,
	from string,
	to []string,
	amt string,
	expectErr bool,
	opt ...flagOption,
) int {
	// TODO remove the hardcode opt after refactor, all methods should accept custom flags
	opt = append(opt, withKeyValue(flagFrom, from))
	opts := applyOptions(c.id, opt)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	if len(to) < 4 {
		s.T().Logf("sending %s tokens from %s to %s on chain %s", amt, from, to, c.id)
	} else {
		s.T().Logf("sending %s tokens from %s to %d accounts on chain %s", amt, from, len(to), c.id)
	}

	atomoneCommand := []string{
		providerBinary,
		txCommand,
		banktypes.ModuleName,
		"multi-send",
		from,
	}

	atomoneCommand = append(atomoneCommand, to...)
	atomoneCommand = append(atomoneCommand, amt, "-y")

	for flag, value := range opts {
		atomoneCommand = append(atomoneCommand, fmt.Sprintf("--%s=%v", flag, value))
	}

	return s.executeTxCommand(ctx, c, atomoneCommand, valIdx, s.expectErrExecValidation(c, valIdx, expectErr))
}
