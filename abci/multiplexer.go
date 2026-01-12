package abci

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"cosmossdk.io/log"
	"github.com/atomone-hub/ics-poc-1/config"
	cmtcfg "github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/node"
	"github.com/cometbft/cometbft/p2p"
	pvm "github.com/cometbft/cometbft/privval"
	"github.com/cometbft/cometbft/proxy"
	"github.com/cometbft/cometbft/rpc/client/local"
	db "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/server/api"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	servergrpc "github.com/cosmos/cosmos-sdk/server/grpc"
	servercmtlog "github.com/cosmos/cosmos-sdk/server/log"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/telemetry"
	"github.com/cosmos/cosmos-sdk/version"
	"github.com/hashicorp/go-metrics"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	abci "github.com/cometbft/cometbft/abci/types"
)

const (
	flagTraceStore = "trace-store"
	flagGRPCOnly   = "grpc-only"
)

// ChainHandler manages a single chain application instance
type ChainHandler struct {
	ChainID string
	conn    *grpc.ClientConn
	app     *RemoteABCIClient
}

// Multiplexer implements ABCI Application interface and routes requests to multiple chain applications
type Multiplexer struct {
	logger log.Logger
	mu     sync.Mutex

	svrCtx *server.Context
	svrCfg serverconfig.Config
	// clientContext is used to configure the different services managed by the multiplexer.
	clientContext client.Context
	// appCreator is a function type responsible for creating the provider chain.
	appCreator servertypes.AppCreator
	// providerChain is the native provider chain.
	providerChain servertypes.Application
	// providerChainID is the chain ID of the provider chain.
	providerChainID string
	// chainHandlers maps chain_id to chain handler
	chainHandlers map[string]*ChainHandler
	// rejectedConsumerChains tracks consumer chains that rejected the current proposal
	rejectedConsumerChains map[string]bool
	// lastConsumerAppHashes stores consumer app hashes from the previous block for PostFinalizeBlock
	lastConsumerAppHashes map[string][]byte
	// activeConsumerChains tracks which consumer chains should have blocks produced for them
	// This is updated by PostFinalizeBlock and enforced in subsequent blocks
	activeConsumerChains map[string]bool
	// cmNode is the comet node which has been created (of the provider chain).
	cmNode *node.Node
	// ctx is the context which is passed to the comet, grpc and api server starting functions.
	ctx context.Context
	// g is the waitgroup to which the comet, grpc and api server init functions are added to.
	g *errgroup.Group
	// traceWriter is the trace writer for the multiplexer.
	traceWriter io.WriteCloser

	// chainConfig holds configuration for all chains
	chainConfig config.Config
}

var _ abci.Application = (*Multiplexer)(nil)

// NewMultiplexer creates a new Multiplexer for handling ICS chains.
func NewMultiplexer(
	svrCtx *server.Context,
	svrCfg serverconfig.Config,
	clientCtx client.Context,
	appCreator servertypes.AppCreator,
	chainConfig config.Config,
) (*Multiplexer, error) {
	if len(chainConfig.Chains) == 0 {
		return nil, fmt.Errorf("at least one chain required")
	}

	mp := &Multiplexer{
		svrCtx:                 svrCtx,
		svrCfg:                 svrCfg,
		clientContext:          clientCtx,
		appCreator:             appCreator,
		logger:                 svrCtx.Logger.With("module", "multiplexer"),
		chainHandlers:          make(map[string]*ChainHandler, len(chainConfig.Chains)),
		rejectedConsumerChains: make(map[string]bool),
		lastConsumerAppHashes:  make(map[string][]byte),
		activeConsumerChains:   make(map[string]bool),
		chainConfig:            chainConfig,
	}

	// Initialize all configured consumer chains as active by default
	// This will be updated by PostFinalizeBlock if the provider implements the extension
	for _, chainInfo := range chainConfig.Chains {
		mp.activeConsumerChains[chainInfo.ChainID] = true
	}

	return mp, nil
}

// isGrpcOnly checks if the GRPC-only mode is enabled using the configuration flag.
func (m *Multiplexer) isGrpcOnly() bool {
	return m.svrCtx.Viper.GetBool(flagGRPCOnly)
}

func (m *Multiplexer) Start() error {
	m.g, m.ctx = getCtx(m.svrCtx, true)

	emitServerInfoMetrics()

	// Initialize provider
	if err := m.startNativeProvider(); err != nil {
		return fmt.Errorf("failed to start native app: %w", err)
	}

	if err := m.enableGRPCAndAPIServers(m.providerChain); err != nil {
		return fmt.Errorf("failed to enable grpc and api servers: %w", err)
	}

	// Initialize all chain handlers
	if err := m.initChainHandlers(); err != nil {
		return fmt.Errorf("failed to initialize chain handlers: %w", err)
	}

	if err := m.validateChainConfiguration(); err != nil {
		return fmt.Errorf("chain configuration validation failed: %w", err)
	}

	if m.isGrpcOnly() {
		m.logger.Info("starting node in gRPC only mode; CometBFT is disabled")
		m.svrCfg.GRPC.Enable = true
	} else {
		m.logger.Info("starting comet node")
		if err := m.startCmtNode(); err != nil {
			return err
		}
	}

	// wait for signal capture and gracefully return
	return m.g.Wait()
}

// startNativeProvider starts a native provider app.
func (m *Multiplexer) startNativeProvider() error {
	traceWriter, err := getTraceWriter(m.svrCtx)
	if err != nil {
		return err
	}
	m.traceWriter = traceWriter

	home := m.svrCtx.Config.RootDir
	db, err := openDB(home, server.GetAppDBBackend(m.svrCtx.Viper))
	if err != nil {
		return err
	}

	m.providerChain = m.appCreator(m.logger, db, m.traceWriter, m.svrCtx.Viper)
	m.providerChainID = getProviderChainID(m.svrCtx.Viper)

	return nil
}

// initChainHandlers creates handlers for all configured chains
func (m *Multiplexer) initChainHandlers() error {
	for _, chainInfo := range m.chainConfig.Chains {
		chainConn, err := m.initRemoteGrpcConn(chainInfo)
		if err != nil {
			return fmt.Errorf("failed to initialize gRPC connection for chain %s: %w", chainInfo.ChainID, err)
		}

		handler := &ChainHandler{
			ChainID: chainInfo.ChainID,
			app:     NewRemoteABCIClient(chainConn),
		}

		if _, exists := m.chainHandlers[chainInfo.ChainID]; exists {
			return fmt.Errorf("duplicate chain ID: %s", chainInfo.ChainID)
		}

		m.chainHandlers[chainInfo.ChainID] = handler
		m.logger.Info("Registered chain", "chain_id", chainInfo.ChainID, "home", chainInfo.Home)
	}

	return nil
}

// initRemoteGrpcConn initializes a gRPC connection to the remote application client and configures transport credentials.
func (m *Multiplexer) initRemoteGrpcConn(chainInfo config.ChainInfo) (*grpc.ClientConn, error) {
	// remove tcp:// prefix if present
	abciServerAddr := strings.TrimPrefix(chainInfo.GRPCAddress, "tcp://")

	conn, err := grpc.NewClient(
		abciServerAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallSendMsgSize(math.MaxInt32),
			grpc.MaxCallRecvMsgSize(math.MaxInt32),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare app connection: %w", err)
	}

	m.logger.Info("initialized remote app client", "address", abciServerAddr, "chain_id", chainInfo.ChainID)
	return conn, nil
}

// validateChainConfiguration logs configured chains for operator awareness.
// Full validation happens at runtime during PostFinalizeBlock calls.
func (m *Multiplexer) validateChainConfiguration() error {
	m.logger.Info("Configured consumer chains",
		"count", len(m.chainHandlers),
		"chains", m.getConfiguredChainIDs())

	// Check if provider implements PostFinalizeBlock
	_, ok := m.providerChain.(HasPostFinalizeBlock)
	if !ok {
		m.logger.Info("Provider doesn't implement PostFinalizeBlock - all configured chains will be active by default")
	} else {
		m.logger.Info("Provider implements PostFinalizeBlock - chain activation will be controlled dynamically")
	}

	return nil
}

// getConfiguredChainIDs returns a list of configured chain IDs for logging
func (m *Multiplexer) getConfiguredChainIDs() []string {
	chainIDs := make([]string, 0, len(m.chainHandlers))
	for chainID := range m.chainHandlers {
		chainIDs = append(chainIDs, chainID)
	}
	return chainIDs
}

func (m *Multiplexer) startCmtNode() error {
	cfg := m.svrCtx.Config
	nodeKey, err := p2p.LoadOrGenNodeKey(cfg.NodeKeyFile())
	if err != nil {
		return err
	}

	cmNode, err := node.NewNodeWithContext(
		m.ctx,
		cfg,
		pvm.LoadOrGenFilePV(cfg.PrivValidatorKeyFile(), cfg.PrivValidatorStateFile()),
		nodeKey,
		proxy.NewConnSyncLocalClientCreator(m),
		GetGenDocProvider(cfg),
		cmtcfg.DefaultDBProvider,
		node.DefaultMetricsProvider(cfg.Instrumentation),
		servercmtlog.CometLoggerWrapper{Logger: m.logger},
	)
	if err != nil {
		return err
	}

	if err := cmNode.Start(); err != nil {
		return err
	}

	m.cmNode = cmNode
	return nil
}

// Stop stops the multiplexer and all its components. It intentionally proceeds
// even if an error occurs in order to shut down as many components as possible.
func (m *Multiplexer) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Info("stopping multiplexer")

	var errs error
	if err := m.stopCometNode(); err != nil {
		errs = errors.Join(errs, err)
	}

	if err := m.providerChain.Close(); err != nil {
		errs = errors.Join(errs, err)
	}

	for _, handler := range m.chainHandlers {
		errs = errors.Join(errs, handler.conn.Close())
	}

	if err := m.stopTraceWriter(); err != nil {
		errs = errors.Join(errs, err)
	}

	return errs
}

func (m *Multiplexer) stopCometNode() error {
	if m.cmNode != nil && m.cmNode.IsRunning() {
		m.logger.Info("stopping comet node")
		if err := m.cmNode.Stop(); err != nil {
			return fmt.Errorf("failed to stop comet node: %w", err)
		}
		m.cmNode.Wait()
	}
	return nil
}

func (m *Multiplexer) stopTraceWriter() error {
	if m.traceWriter != nil {
		return m.traceWriter.Close()
	}
	return nil
}

// ABCI Application interface implementation

// checkHaltConditions returns an error if the node should halt based on a halt-height or
// halt-time configured in app.toml.
func (m *Multiplexer) checkHaltConditions(req *abci.RequestFinalizeBlock) error {
	if m.svrCfg.HaltHeight > 0 && uint64(req.Height) >= m.svrCfg.HaltHeight {
		return fmt.Errorf("halting node per configuration at height %v", m.svrCfg.HaltHeight)
	}
	if m.svrCfg.HaltTime > 0 && req.Time.Unix() >= int64(m.svrCfg.HaltTime) {
		return fmt.Errorf("halting node per configuration at time %v", m.svrCfg.HaltTime)
	}
	return nil
}

// Helper functions

func getTraceWriter(svrCtx *server.Context) (traceWriter io.WriteCloser, err error) {
	traceWriterFile := svrCtx.Viper.GetString(flagTraceStore)
	traceWriter, err = openTraceWriter(traceWriterFile)
	if err != nil {
		return nil, err
	}
	return traceWriter, nil
}

func openDB(rootDir string, backendType db.BackendType) (db.DB, error) {
	dataDir := filepath.Join(rootDir, "data")
	return db.NewDB("application", backendType, dataDir)
}

// openTraceWriter opens a trace writer for the given file.
// If the file is empty, it returns no writer and no error.
func openTraceWriter(traceWriterFile string) (w io.WriteCloser, err error) {
	if traceWriterFile == "" {
		return w, nil
	}
	return os.OpenFile(
		traceWriterFile,
		os.O_WRONLY|os.O_APPEND|os.O_CREATE,
		0o666,
	)
}

// enableGRPCAndAPIServers enables the gRPC and API servers for the provided application if configured to do so.
// It registers transaction, Tendermint, and node services, and starts the gRPC and API servers if enabled.
func (m *Multiplexer) enableGRPCAndAPIServers(app servertypes.Application) error {
	if app == nil {
		return fmt.Errorf("unable to enable grpc and api servers, app is nil")
	}
	// if we are running natively and have specified to enable gRPC or API servers
	// we need to register the relevant services.
	if m.svrCfg.API.Enable || m.svrCfg.GRPC.Enable {
		m.logger.Debug("registering services and local comet client")
		m.clientContext = m.clientContext.WithClient(local.New(m.cmNode))
		app.RegisterTxService(m.clientContext)
		app.RegisterTendermintService(m.clientContext)
		app.RegisterNodeService(m.clientContext, m.svrCfg)
	}

	// startGRPCServer the grpc server in the case of a native app. If using an embedded app
	// it will use that instead.
	if m.svrCfg.GRPC.Enable {
		grpcServer, clientContext, err := m.startGRPCServer()
		if err != nil {
			return err
		}
		m.clientContext = clientContext // update client context with grpc

		// startAPIServer starts the api server for a native app. If using an embedded app
		// it will use that instead.
		if m.svrCfg.API.Enable {
			metrics, err := telemetry.New(m.svrCfg.Telemetry)
			if err != nil {
				return err
			}

			m.clientContext = m.clientContext.WithHomeDir(m.svrCtx.Config.RootDir)

			apiSrv := api.New(m.clientContext, m.logger.With(log.ModuleKey, "api-server"), grpcServer)
			m.providerChain.RegisterAPIRoutes(apiSrv, m.svrCfg.API)

			if m.svrCfg.Telemetry.Enabled {
				apiSrv.SetTelemetry(metrics)
			}

			m.logger.Debug("starting api server")
			m.g.Go(func() error {
				return apiSrv.Start(m.ctx, m.svrCfg)
			})
		}
	}
	return nil
}

// startGRPCServer initializes and starts a gRPC server if enabled in the configuration, returning the server and updated context.
func (m *Multiplexer) startGRPCServer() (*grpc.Server, client.Context, error) {
	_, _, err := net.SplitHostPort(m.svrCfg.GRPC.Address)
	if err != nil {
		return nil, m.clientContext, err
	}

	maxSendMsgSize := m.svrCfg.GRPC.MaxSendMsgSize
	if maxSendMsgSize == 0 {
		maxSendMsgSize = serverconfig.DefaultGRPCMaxSendMsgSize
	}

	maxRecvMsgSize := m.svrCfg.GRPC.MaxRecvMsgSize
	if maxRecvMsgSize == 0 {
		maxRecvMsgSize = serverconfig.DefaultGRPCMaxRecvMsgSize
	}

	// if gRPC is enabled, configure gRPC client for gRPC gateway
	grpcClient, err := grpc.NewClient(
		m.svrCfg.GRPC.Address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.ForceCodec(codec.NewProtoCodec(m.clientContext.InterfaceRegistry).GRPCCodec()),
			grpc.MaxCallRecvMsgSize(maxRecvMsgSize),
			grpc.MaxCallSendMsgSize(maxSendMsgSize),
		),
	)
	if err != nil {
		return nil, m.clientContext, err
	}

	m.clientContext = m.clientContext.WithGRPCClient(grpcClient)
	m.logger.Debug("gRPC client assigned to client context", "target", m.svrCfg.GRPC.Address)
	grpcSrv, err := servergrpc.NewGRPCServer(m.clientContext, m.providerChain, m.svrCfg.GRPC)
	if err != nil {
		return nil, m.clientContext, err
	}

	// Start the gRPC server in a goroutine. Note, the provided ctx will ensure
	// that the server is gracefully shut down.
	m.g.Go(func() error {
		return servergrpc.StartGRPCServer(m.ctx, m.logger.With(log.ModuleKey, "grpc-server"), m.svrCfg.GRPC, grpcSrv)
	})

	return grpcSrv, m.clientContext, nil
}

func emitServerInfoMetrics() {
	var ls []metrics.Label

	versionInfo := version.NewInfo()
	ls = append(ls, telemetry.NewLabel("version", versionInfo.Version))
	ls = append(ls, telemetry.NewLabel("commit", versionInfo.GitCommit))

	telemetry.SetGaugeWithLabels(
		[]string{"server", "info"},
		1,
		ls,
	)
}

func getCtx(svrCtx *server.Context, block bool) (*errgroup.Group, context.Context) {
	ctx, cancelFn := context.WithCancel(context.Background())
	g, ctx := errgroup.WithContext(ctx)
	// listen for quit signals so the calling parent process can gracefully exit
	server.ListenForQuitSignals(g, block, cancelFn, svrCtx.Logger)
	return g, ctx
}
