package e2e

import (
	"encoding/hex"
	"time"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/types/tx"
	"strings"
	"fmt"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

func (s *IntegrationTestSuite) waitTx(endpoint, txHash string, msgResp codec.ProtoMarshaler) (err error) {
	for i := 0; i < 15; i++ {
		time.Sleep(time.Second)
		_, err = s.queryTx(endpoint, txHash, msgResp)
		if isErrNotFound(err) {
			continue
		}
		return
	}
	return
}


// queryTx returns an error if the tx is not found or is failed.
func (s *IntegrationTestSuite) queryTx(endpoint, txHash string, msgResp codec.ProtoMarshaler) (height int, err error) {
	body, err := httpGet(fmt.Sprintf("%s/cosmos/tx/v1beta1/txs/%s", endpoint, txHash))
	if err != nil {
		return 0, err
	}

	var resp tx.GetTxResponse
	if err := s.cdc.UnmarshalJSON(body, &resp); err != nil {
		return 0, fmt.Errorf("failed to read response body: %w", err)
	}
	if resp.TxResponse.Code != 0 {
		return 0, fmt.Errorf("tx %s is failed with code=%d log='%s'", txHash, resp.TxResponse.Code, resp.TxResponse.RawLog)
	}
	if msgResp != nil {
		// msgResp is provided, try to decode the tx response
		data, err := hex.DecodeString(resp.TxResponse.Data)
		if err != nil {
			return 0, err
		}
		var txMsgData sdk.TxMsgData
		if err := s.cdc.Unmarshal(data, &txMsgData); err != nil {
			return 0, err
		}
		if err := s.cdc.Unmarshal(txMsgData.MsgResponses[0].Value, msgResp); err != nil {
			return 0, err
		}
	}
	return int(resp.TxResponse.Height), nil
}

// if denom not found, return 0 denom.
func (s *IntegrationTestSuite) queryBalance(endpoint, addr, denom string) sdk.Coin {
	balances, err := s.queryAllBalances(endpoint, addr)
	s.Require().NoError(err)
	for _, c := range balances {
		if strings.Contains(c.Denom, denom) {
			return c
		}
	}
	return sdk.NewInt64Coin(denom, 0)
}

func (s *IntegrationTestSuite) queryAllBalances(endpoint, addr string) (sdk.Coins, error) {
	body, err := httpGet(fmt.Sprintf("%s/cosmos/bank/v1beta1/balances/%s", endpoint, addr))
	if err != nil {
		return nil, fmt.Errorf("failed to execute HTTP request: %w", err)
	}

	var balancesResp banktypes.QueryAllBalancesResponse
	if err := s.cdc.UnmarshalJSON(body, &balancesResp); err != nil {
		return nil, err
	}

	return balancesResp.Balances, nil
}
