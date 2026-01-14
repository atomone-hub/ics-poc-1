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
	"time"

	"cosmossdk.io/log"
	"github.com/atomone-hub/ics-poc-1/config"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtcfg "github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/crypto/encoding"
	"github.com/cometbft/cometbft/node"
	"github.com/cometbft/cometbft/p2p"
	pvm "github.com/cometbft/cometbft/privval"
	"github.com/cometbft/cometbft/proxy"
	"github.com/cometbft/cometbft/rpc/client/local"
	cmttypes "github.com/cometbft/cometbft/types"
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

	providertypes "github.com/atomone-hub/ics-poc-1/modules/provider/types"
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
	// providerCreator is a function type responsible for creating the provider chain.
	providerCreator servertypes.AppCreator
	// providerChain is the native provider chain.
	providerChain servertypes.Application
	// providerChainID is the chain ID of the provider chain.
	providerChainID string
	// chainHandlers maps chain_id to chain handler
	chainHandlers map[string]*ChainHandler
	// rejectedConsumerChains tracks consumer chains that rejected the current proposal
	rejectedConsumerChains map[string]bool
	// initializedChains tracks which chains have had InitChain called
	initializedChains map[string]bool
	// activeChains tracks the list of active chains from the provider module
	activeChains map[string]bool
	// genesisValidators stores the initial validators from InitChain for use with dynamic consumer chains
	genesisValidators []abci.ValidatorUpdate
	// genesisConsensusParams stores the initial consensus params from InitChain for use with dynamic consumer chains
	genesisConsensusParams *cmttypes.ConsensusParams
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
	providerCreator servertypes.AppCreator,
	chainConfig config.Config,
) (*Multiplexer, error) {
	logger := svrCtx.Logger.With("module", "multiplexer")
	if len(chainConfig.Chains) == 0 {
		logger.Warn("no chains configured in multiplexer", "config", chainConfig)
	}

	mp := &Multiplexer{
		svrCtx:                 svrCtx,
		svrCfg:                 svrCfg,
		clientContext:          clientCtx,
		providerCreator:        providerCreator,
		logger:                 logger,
		chainHandlers:          make(map[string]*ChainHandler, len(chainConfig.Chains)),
		rejectedConsumerChains: make(map[string]bool),
		initializedChains:      make(map[string]bool),
		activeChains:           make(map[string]bool),
		chainConfig:            chainConfig,
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

	// Initialize all chain handlers
	if err := m.initChainHandlers(); err != nil {
		return fmt.Errorf("failed to initialize chain handlers: %w", err)
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

	if err := m.enableGRPCAndAPIServers(m.providerChain); err != nil {
		return fmt.Errorf("failed to enable grpc and api servers: %w", err)
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

	m.providerChain = m.providerCreator(m.logger, db, m.traceWriter, m.svrCtx.Viper)
	m.providerChainID = getProviderChainID(m.svrCtx.Viper)

	if err := m.loadGenesisValues(); err != nil {
		return fmt.Errorf("failed to load genesis values: %w", err)
	}

	return nil
}

// loadGenesisValues loads validators and consensus params from the genesis document.
func (m *Multiplexer) loadGenesisValues() error {
	genDocProvider := GetGenDocProvider(m.svrCtx.Config)
	genDoc, err := genDocProvider()
	if err != nil {
		return fmt.Errorf("failed to load genesis doc: %w", err)
	}

	// Convert genesis validators to ABCI validator updates
	m.genesisValidators = make([]abci.ValidatorUpdate, 0, len(genDoc.Validators))
	for _, val := range genDoc.Validators {
		pubKey, err := encoding.PubKeyToProto(val.PubKey)
		if err != nil {
			return fmt.Errorf("failed to convert validator pubkey: %w", err)
		}
		m.genesisValidators = append(m.genesisValidators, abci.ValidatorUpdate{
			PubKey: pubKey,
			Power:  val.Power,
		})
	}

	m.genesisConsensusParams = genDoc.ConsensusParams
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
		m.logger.Info("Registered chain", "chain_id", chainInfo.ChainID)
	}

	return nil
}

// initRemoteGrpcConn initializes a gRPC connection to the remote application client
func (m *Multiplexer) initRemoteGrpcConn(chainInfo config.ChainInfo) (*grpc.ClientConn, error) {
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
		if handler.conn != nil {
			errs = errors.Join(errs, handler.conn.Close())
		}
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

// getActiveChainIDsFromProvider gets active chain IDs from the provider module
func (m *Multiplexer) getActiveChainIDsFromProvider(ctx context.Context) ([]string, error) {
	queryReq := &providertypes.QueryActiveConsumerChainsRequest{}
	queryReqBytes, err := queryReq.Marshal()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal query request: %w", err)
	}

	abciReq := &abci.RequestQuery{
		Path:   "/ics.provider.v1.Query/ActiveConsumerChains",
		Data:   queryReqBytes,
		Height: 0,
		Prove:  false,
	}

	abciResp, err := m.providerChain.Query(ctx, abciReq)
	if err != nil {
		return nil, fmt.Errorf("failed to query active chains: %w", err)
	}

	if abciResp.Code != 0 {
		return nil, fmt.Errorf("query returned error code %d: %s", abciResp.Code, abciResp.Log)
	}

	var queryResp providertypes.QueryActiveConsumerChainsResponse
	if err := queryResp.Unmarshal(abciResp.Value); err != nil {
		return nil, fmt.Errorf("failed to unmarshal query response: %w", err)
	}

	var activeChainIDs []string
	for _, chain := range queryResp.ConsumerChains {
		activeChainIDs = append(activeChainIDs, chain.ChainId)
	}

	return activeChainIDs, nil
}

// validateActiveChains checks that all active chains have corresponding handlers
func (m *Multiplexer) validateActiveChains() {
	for chainID := range m.activeChains {
		if _, exists := m.chainHandlers[chainID]; !exists {
			m.logger.Error(
				"Active consumer chain missing from validator configuration",
				"chain_id", chainID,
				"error", "chain not found in chainHandlers",
			)
		}
	}
}

// initChainIfNeeded initializes a chain if it hasn't been initialized yet.
// Checks chain state via Info to detect already-initialized chains after restart.
func (m *Multiplexer) initChainIfNeeded(chainID string, height int64, blockTime time.Time) error {
	if m.initializedChains[chainID] {
		return nil
	}

	handler, exists := m.chainHandlers[chainID]
	if !exists {
		return fmt.Errorf("chain handler not found for chain_id: %s", chainID)
	}

	// Check if chain has state (already initialized)
	infoResp, err := handler.app.Info(&abci.RequestInfo{})
	if err == nil && (infoResp.LastBlockHeight > 0 || len(infoResp.LastBlockAppHash) > 0) {
		m.logger.Info("Chain already initialized", "chain_id", chainID, "height", infoResp.LastBlockHeight)
		m.initializedChains[chainID] = true
		return nil
	}

	m.logger.Info("Initializing new consumer chain", "chain_id", chainID, "height", height)

	// Use stored genesis values, falling back to defaults if not available
	consensusParams := m.genesisConsensusParams
	if consensusParams == nil {
		consensusParams = cmttypes.DefaultConsensusParams()
	}

	protoConsensusParams := consensusParams.ToProto()
	initReq := &abci.RequestInitChain{
		Time:            blockTime,
		ChainId:         chainID,
		ConsensusParams: &protoConsensusParams,
		Validators:      m.genesisValidators,
		AppStateBytes:   []byte{},
		InitialHeight:   height,
	}

	_, err = handler.app.InitChain(initReq)
	if err != nil {
		return fmt.Errorf("failed to initialize chain %s: %w", chainID, err)
	}

	m.initializedChains[chainID] = true
	m.logger.Info("Successfully initialized consumer chain", "chain_id", chainID)

	return nil
}

// updateActiveChains updates the list of active chains from the provider module
func (m *Multiplexer) updateActiveChains(ctx context.Context) error {
	activeChainIDs, err := m.getActiveChainIDsFromProvider(ctx)
	if err != nil {
		return fmt.Errorf("failed to get active chains: %w", err)
	}

	newActiveChains := make(map[string]bool)
	for _, chainID := range activeChainIDs {
		newActiveChains[chainID] = true
	}

	for chainID := range newActiveChains {
		if !m.activeChains[chainID] {
			m.logger.Info("New active consumer chain detected", "chain_id", chainID)
		}
	}

	for chainID := range m.activeChains {
		if !newActiveChains[chainID] {
			m.logger.Info("Consumer chain no longer active", "chain_id", chainID)
		}
	}

	m.activeChains = newActiveChains
	return nil
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
	server.ListenForQuitSignals(g, block, cancelFn, svrCtx.Logger)
	return g, ctx
}
