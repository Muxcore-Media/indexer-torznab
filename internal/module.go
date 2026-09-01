package internal

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	indexerv1 "github.com/Muxcore-Media/contracts-indexer/muxcore/indexer/v1"
	"github.com/Muxcore-Media/core/pkg/contracts"
	"github.com/Muxcore-Media/core/sdk/go/client"
)

const (
	defaultIndexerName = "Torznab"
	moduleVersion      = "0.1.5"
	healthProbeTimeout = 5 * time.Second
)

type capabilitiesAPI interface {
	FetchCapabilities(ctx context.Context) (*indexerv1.GetCapabilitiesResponse, error)
	ProbeHealth(ctx context.Context) error
}

type indexerListAPI interface {
	ListIndexers(ctx context.Context) ([]prowlarrIndexer, error)
}

type Module struct { //nolint:govet // fieldalignment: lifecycle fields grouped for readability
	indexerv1.UnimplementedIndexerServiceServer

	mu           sync.RWMutex
	api          searchAPI
	capsAPI      capabilitiesAPI
	listAPI      indexerListAPI
	baseURL      string
	apiKey       string
	name         string
	id           string
	grpcAddr     string
	grpcSrv      *grpc.Server
	lis          net.Listener
	http         *http.Client
	httpInjected bool
	wgConfPath   string
	vpnIface     string
	prowlarr     bool
	httpTimeout  time.Duration
	mc           *client.Client
}

type Config struct { //nolint:govet // fieldalignment: config fields grouped for readability
	ID          string
	GRPCAddr    string
	BaseURL     string
	APIKey      string
	Name        string
	Timeout     time.Duration
	HTTP        *http.Client
	API         searchAPI
	CapsAPI     capabilitiesAPI
	ListAPI     indexerListAPI
	WGConfPath  string
	SkipVPNGate bool
}

func NewModule(cfg Config) *Module {
	if cfg.ID == "" {
		cfg.ID = "indexer-torznab"
	}
	if cfg.GRPCAddr == "" {
		cfg.GRPCAddr = ":9486"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 90 * time.Second
	}
	if v := strings.TrimSpace(os.Getenv("TORZNAB_TIMEOUT")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.Timeout = d
		}
	}
	if v := os.Getenv("TORZNAB_GRPC_ADDR"); v != "" {
		cfg.GRPCAddr = v
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = strings.TrimSpace(os.Getenv("TORZNAB_URL"))
	}
	if cfg.APIKey == "" {
		cfg.APIKey = strings.TrimSpace(os.Getenv("TORZNAB_API_KEY"))
	}
	prowlarr := false
	if cfg.BaseURL == "" {
		if p := strings.TrimSpace(os.Getenv("PROWLARR_URL")); p != "" {
			cfg.BaseURL = strings.TrimRight(p, "/")
			prowlarr = true
		}
	}
	if cfg.APIKey == "" {
		cfg.APIKey = strings.TrimSpace(os.Getenv("PROWLARR_API_KEY"))
	}
	if cfg.Name == "" {
		cfg.Name = strings.TrimSpace(os.Getenv("TORZNAB_NAME"))
	}
	if cfg.Name == "" {
		cfg.Name = defaultIndexerName
	}
	if !prowlarr {
		cfg.BaseURL = normalizeAPIBase(cfg.BaseURL)
	}
	if cfg.WGConfPath == "" {
		cfg.WGConfPath = strings.TrimSpace(os.Getenv("WG_CONF"))
	}

	httpInjected := cfg.HTTP != nil || cfg.SkipVPNGate
	hc := cfg.HTTP
	vpnIface := ""
	if hc == nil && liveRemoteRequiresVPN(cfg.BaseURL, false) && !cfg.SkipVPNGate {
		if bound, iface, err := newVPNBoundHTTPClient(cfg.WGConfPath, cfg.Timeout); err == nil {
			hc = bound
			vpnIface = iface
		}
	}
	if hc == nil {
		hc = &http.Client{Timeout: cfg.Timeout}
	}

	m := &Module{
		id:           cfg.ID,
		grpcAddr:     cfg.GRPCAddr,
		baseURL:      cfg.BaseURL,
		apiKey:       cfg.APIKey,
		name:         cfg.Name,
		http:         hc,
		httpInjected: httpInjected,
		wgConfPath:   cfg.WGConfPath,
		vpnIface:     vpnIface,
		prowlarr:     prowlarr,
		httpTimeout:  cfg.Timeout,
	}
	if cfg.API != nil {
		m.api = cfg.API
	}
	if cfg.CapsAPI != nil {
		m.capsAPI = cfg.CapsAPI
	}
	if cfg.ListAPI != nil {
		m.listAPI = cfg.ListAPI
	}
	if m.api == nil || m.capsAPI == nil || (m.prowlarr && m.listAPI == nil) {
		m.bindUpstreamClients(hc)
	}
	return m
}

func (m *Module) bindUpstreamClients(hc *http.Client) {
	if m.prowlarr {
		pc := newProwlarrClient(m.baseURL, m.apiKey, hc)
		if m.api == nil {
			m.api = pc
		}
		if m.capsAPI == nil {
			m.capsAPI = pc
		}
		if m.listAPI == nil {
			m.listAPI = pc
		}
		return
	}
	tc := newTorznabClient(m.baseURL, m.apiKey, hc)
	if m.api == nil {
		m.api = tc
	}
	if m.capsAPI == nil {
		m.capsAPI = tc
	}
}

func (m *Module) Info() contracts.ModuleInfo {
	return contracts.ModuleInfo{
		ID:           m.id,
		Name:         "Torznab Indexer",
		Version:      moduleVersion,
		Roles:        []string{"indexer"},
		Description:  "Aggregating indexer via Torznab/Newznab HTTP API (Prowlarr, Jackett)",
		Author:       "MuxCore",
		Capabilities: []string{"indexer", "indexer.torznab", "indexer.newznab"},
		Contracts: []contracts.ContractDeclaration{
			{Repo: "github.com/Muxcore-Media/contracts-indexer", Interface: "Indexer", Version: "v1"},
		},
		MinCoreVersion: "0.4.0",
	}
}

func (m *Module) Init(ctx context.Context) error {
	if err := enforceRemoteTorznabVPN(m.wgConfPath, m.baseURL, m.httpInjected); err != nil {
		return err
	}
	if liveRemoteRequiresVPN(m.baseURL, m.httpInjected) && m.vpnIface == "" {
		bound, iface, err := newVPNBoundHTTPClient(m.wgConfPath, m.httpTimeout)
		if err != nil {
			return fmt.Errorf("vpn-bound HTTP client: %w", err)
		}
		m.http = bound
		m.vpnIface = iface
		m.bindUpstreamClients(bound)
	}
	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", m.grpcAddr)
	if err != nil {
		return fmt.Errorf("listen gRPC %s: %w", m.grpcAddr, err)
	}
	m.lis = lis
	if ta, ok := lis.Addr().(*net.TCPAddr); ok && strings.HasPrefix(m.grpcAddr, ":") {
		m.grpcAddr = fmt.Sprintf(":%d", ta.Port)
	}
	slog.Info("indexer-torznab initialized",
		"grpc", m.grpcAddr,
		"configured", m.configured(),
		"prowlarr", m.prowlarr,
		"vpn_iface", m.vpnIface,
	)
	return nil
}

func (m *Module) Start(ctx context.Context) error {
	m.grpcSrv = grpc.NewServer()
	indexerv1.RegisterIndexerServiceServer(m.grpcSrv, m)
	go func() {
		slog.Info("indexer-torznab gRPC started", "addr", m.grpcAddr)
		if err := m.grpcSrv.Serve(m.lis); err != nil {
			slog.Error("indexer-torznab gRPC serve error", "error", err)
		}
	}()
	go m.dialCore(context.WithoutCancel(ctx))
	return nil
}

func (m *Module) Stop(ctx context.Context) error {
	if m.grpcSrv != nil {
		m.grpcSrv.GracefulStop()
	}
	m.mu.Lock()
	if m.mc != nil {
		_ = m.mc.Close()
		m.mc = nil
	}
	m.mu.Unlock()
	slog.Info("indexer-torznab stopped")
	return nil
}

func (m *Module) Health(ctx context.Context) error {
	if !m.configured() {
		return nil
	}
	m.mu.RLock()
	caps := m.capsAPI
	m.mu.RUnlock()
	if caps == nil {
		return nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, healthProbeTimeout)
	defer cancel()
	return caps.ProbeHealth(probeCtx)
}

func (m *Module) configured() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.baseURL != ""
}

func (m *Module) Search(ctx context.Context, req *indexerv1.SearchRequest) (*indexerv1.SearchResponse, error) {
	if !m.configured() {
		slog.Debug("indexer-torznab: not configured (TORZNAB_URL empty), returning no results")
		return &indexerv1.SearchResponse{}, nil
	}
	m.mu.RLock()
	base := m.baseURL
	injected := m.httpInjected
	wg := m.wgConfPath
	m.mu.RUnlock()
	if err := enforceRemoteTorznabVPN(wg, base, injected); err != nil {
		return nil, err
	}
	query := strings.TrimSpace(req.GetQuery())
	if query == "" {
		return &indexerv1.SearchResponse{}, nil
	}

	m.mu.RLock()
	api := m.api
	name := m.name
	m.mu.RUnlock()

	hits, err := api.Search(ctx, torznabQuery{
		Query:      query,
		Type:       req.GetType(),
		Categories: req.GetCategories(),
		Season:     int(req.GetSeason()),
		Episode:    int(req.GetEpisode()),
		Year:       int(req.GetYear()),
		Limit:      int(req.GetLimit()),
		Offset:     int(req.GetOffset()),
		IndexerIDs: req.GetIndexerIds(),
	})
	if err != nil {
		slog.Warn("indexer-torznab search failed", "error", err)
		if isResourceExhausted(err) {
			return nil, status.Errorf(codes.ResourceExhausted, "search: %v", err)
		}
		return nil, fmt.Errorf("search: %w", err)
	}
	results := mapHits(hits, name)
	go m.cacheHitsInStorage(context.WithoutCancel(ctx), results)
	return &indexerv1.SearchResponse{
		Results: results,
		Total:   clampInt32(len(results)),
	}, nil
}

func (m *Module) GetCapabilities(ctx context.Context, _ *indexerv1.GetCapabilitiesRequest) (*indexerv1.GetCapabilitiesResponse, error) {
	if !m.configured() {
		return unconfiguredCapabilities(), nil
	}
	m.mu.RLock()
	caps := m.capsAPI
	m.mu.RUnlock()
	if caps == nil {
		return unconfiguredCapabilities(), nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, healthProbeTimeout)
	defer cancel()
	return caps.FetchCapabilities(probeCtx)
}

func clampInt32(n int) int32 {
	if n > math.MaxInt32 {
		return math.MaxInt32
	}
	if n < math.MinInt32 {
		return math.MinInt32
	}
	return int32(n)
}

func (m *Module) ListIndexers(ctx context.Context, _ *indexerv1.ListIndexersRequest) (*indexerv1.ListIndexersResponse, error) {
	if !m.configured() {
		return &indexerv1.ListIndexersResponse{}, nil
	}
	m.mu.RLock()
	listAPI := m.listAPI
	name := m.name
	prowlarr := m.prowlarr
	m.mu.RUnlock()

	if prowlarr && listAPI != nil {
		indexers, err := listAPI.ListIndexers(ctx)
		if err != nil {
			return nil, fmt.Errorf("list indexers: %w", err)
		}
		out := make([]*indexerv1.IndexerInfo, 0, len(indexers))
		for _, ix := range indexers {
			proto := strings.TrimSpace(ix.Protocol)
			if proto == "" {
				proto = "torrent"
			}
			lang := strings.TrimSpace(ix.Language)
			if lang == "" {
				lang = "en"
			}
			out = append(out, &indexerv1.IndexerInfo{
				Id:         ix.ID,
				Name:       ix.Name,
				Protocol:   proto,
				Language:   lang,
				Configured: ix.Enable,
			})
		}
		return &indexerv1.ListIndexersResponse{Indexers: out}, nil
	}

	return &indexerv1.ListIndexersResponse{
		Indexers: []*indexerv1.IndexerInfo{
			{
				Id:         defaultTorznabIndexerID,
				Name:       name,
				Protocol:   "torrent",
				Language:   "en",
				Configured: true,
			},
		},
	}, nil
}
