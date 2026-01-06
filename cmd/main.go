package cmd

import (
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/server/types"

	"github.com/atomone-hub/ics-poc-1/config"
)

// StartCommandHandler is the type that must implement the multiplexer to match Cosmos SDK start logic.
type StartCommandHandler = func(svrCtx *server.Context, clientCtx client.Context, appCreator types.AppCreator, withCmt bool, opts server.StartCmdOptions) error

// NewProvider creates a command start handler to use in the Cosmos SDK server start options.
func NewProvider(customersConfig config.Config) StartCommandHandler {
	return func(
		svrCtx *server.Context,
		clientCtx client.Context,
		appCreator types.AppCreator,
		withCmt bool,
		_ server.StartCmdOptions,
	) error {
		if !withCmt {
			svrCtx.Logger.Info("App cannot be started without CometBFT when using the multiplexer.")
			return nil
		}

		return start(customersConfig, svrCtx, clientCtx, appCreator)
	}
}
