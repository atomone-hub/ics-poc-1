package provider

import (
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"

	"github.com/atomone-hub/ics-poc-1/modules/provider/types"
)

// AutoCLIOptions implements the autocli.HasAutoCLIConfig interface.
func (am AppModule) AutoCLIOptions() *autocliv1.ModuleOptions {
	return &autocliv1.ModuleOptions{
		Query: &autocliv1.ServiceCommandDescriptor{
			Service: types.Query_serviceDesc.ServiceName,
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "Params",
					Use:       "params",
					Short:     "Shows the parameters of the module",
				},
				{
					RpcMethod: "ConsumerChains",
					Use:       "list-consumers",
					Short:     "List all consumer chains, regardless their statuses",
				},
				{
					RpcMethod: "ActiveConsumerChains",
					Use:       "list-active-consumers",
					Short:     "List active consumer chains",
				},
				{
					RpcMethod: "ConsumerChain",
					Use:       "consumer [chain_id]",
					Short:     "Get a consumer chain info",
					PositionalArgs: []*autocliv1.PositionalArgDescriptor{
						{ProtoField: "chain_id"},
					},
				},
				// this line is used by ignite scaffolding # autocli/query
			},
		},
		Tx: &autocliv1.ServiceCommandDescriptor{
			Service:              types.Msg_serviceDesc.ServiceName,
			EnhanceCustomCommand: true, // only required if you want to use the custom command
			RpcCommandOptions: []*autocliv1.RpcCommandOptions{
				{
					RpcMethod: "UpdateParams",
					Skip:      true, // skipped because authority gated
				},
				{
					RpcMethod: "AddConsumer",
					Skip:      true, // skipped because authority gated

				},
				{
					RpcMethod: "SunsetConsumer",
					Skip:      true, // skipped because authority gated

				},
				{
					RpcMethod: "UpgradeConsumer",
					Skip:      true, // skipped because authority gated

				},
				// this line is used by ignite scaffolding # autocli/tx
			},
		},
	}
}
