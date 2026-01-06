package cmd

import (
	"fmt"
	"strings"

	"github.com/atomone-hub/ics-poc-1/abci"
	"github.com/atomone-hub/ics-poc-1/config"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/server"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/cosmos/cosmos-sdk/server/types"
)

func start(config config.Config, svrCtx *server.Context, clientCtx client.Context, appCreator types.AppCreator) error {
	svrCfg, err := getAndValidateConfig(svrCtx)
	if err != nil {
		return err
	}

	svrCtx.Logger.Info("initializing multiplexer")

	multiplexer, err := abci.NewMultiplexer(svrCtx, svrCfg, clientCtx, appCreator, config)
	if err != nil {
		return err
	}

	defer func() {
		if err := multiplexer.Stop(); err != nil {
			svrCtx.Logger.Error("failed to stop multiplexer", "err", err)
		}
	}()

	// Start will either start the latest app natively, or an embedded app if one is specified.
	if err := multiplexer.Start(); err != nil {
		return fmt.Errorf("failed to start multiplexer: %w", err)
	}

	return nil
}

func getAndValidateConfig(svrCtx *server.Context) (serverconfig.Config, error) {
	config, err := serverconfig.GetConfig(svrCtx.Viper)
	if err != nil {
		return config, err
	}

	if err := config.ValidateBasic(); err != nil {
		return config, err
	}

	if strings.TrimSpace(svrCtx.Config.RPC.GRPCListenAddress) == "" {
		return config, fmt.Errorf("must set the RPC GRPC listen address in config.toml (grpc_laddr) or by flag (--rpc.grpc_laddr)")
	}

	return config, nil
}
