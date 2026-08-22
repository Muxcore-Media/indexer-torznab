package internal

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"

	indexerv1 "github.com/Muxcore-Media/contracts-indexer/muxcore/indexer/v1"
	"github.com/Muxcore-Media/core/pkg/contracts"
	"github.com/Muxcore-Media/core/sdk/go/client"
)

const (
	defaultIndexerName = "Torznab"
	indexerID          = int32(2)
)

type Module struct {
	indexerv1.UnimplementedIndexerServiceServer

	mu           sync.RWMutex
	api          searchAPI
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
	mc           *client.Client
}

type Config struct {
	ID          string
	GRPCAddr    string
	BaseURL     string
	APIKey      string
	Name        string
	Timeout     time.Duration
	HTTP        *http.Client
	API         searchAPI
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
		api:          cfg.API,
		httpInjected: httpInjected,
		wgConfPath:   cfg.WGConfPath,
		vpnIface:     vpnIface,
		prowlarr:     prowlarr,
	}
	if m.api == nil {
		m.api = m.newSearchAPI(hc)
	}
	return m
}

func (m *Module) newSearchAPI(hc *http.Client) searchAPI {
	if m.prowlarr {
		return newProwlarrClient(m.baseURL, m.apiKey, hc)
	}
	return newTorznabClient(m.baseURL, m.apiKey, hc)
}

func (m *Module) Info() contracts.ModuleInfo {
	return contracts.ModuleInfo{
		ID:           m.id,
		Name:         "Torznab Indexer",
		Version:      "0.1.4",
		Roles:        []string{"indexer"},
		Description:  "Aggregating indexer via Torznab/Newznab HTTP API (Prowlarr, Jackett)",
		Author:       "MuxCore",
		Capabilities: []string{"indexer", "indexer.torznab", "indexer.newznab"},
		Contracts: []contracts.ContractDeclaration{
			{Repo: "github.com/Muxcore-Media/contracts-indexer", Interface: "Indexer", Version: "v1"},
		},
		MinCoreVersion: "0.4.0",
		HTTPAddr:       m.grpcAddr,
	}
}

func (m *Module) Init(ctx context.Context) error {
	if err := enforceRemoteTorznabVPN(m.wgConfPath, m.baseURL, m.httpInjected); err != nil {
		return err
	}
	if liveRemoteRequiresVPN(m.baseURL, m.httpInjected) && m.vpnIface == "" {
		bound, iface, err := newVPNBoundHTTPClient(m.wgConfPath, 90*time.Second)
		if err != nil {
			return fmt.Errorf("vpn-bound HTTP client: %w", err)
		}
		m.http = bound
		m.vpnIface = iface
		m.api = m.newSearchAPI(bound)
	}
	lis, err := net.Listen("tcp", m.grpcAddr)
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
	go m.dialCore(context.Background())
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

func (m *Module) Health(ctx context.Context) error { return nil }

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
		Limit:      int(req.GetLimit()),
		Offset:     int(req.GetOffset()),
	})
	if err != nil {
		slog.Warn("indexer-torznab search failed", "error", err)
		return nil, fmt.Errorf("search: %w", err)
	}
	results := mapHits(hits, name)
	go m.cacheHitsInStorage(results)
	return &indexerv1.SearchResponse{
		Results: results,
		Total:   int32(len(results)),
	}, nil
}

func (m *Module) GetCapabilities(ctx context.Context, _ *indexerv1.GetCapabilitiesRequest) (*indexerv1.GetCapabilitiesResponse, error) {
	return &indexerv1.GetCapabilitiesResponse{
		SupportsSearch:      true,
		SupportsMovieSearch: true,
		SupportsTvSearch:    true,
		SupportedCategories: []string{"movie", "tv", "audio", "book", "other"},
		SupportedProtocols:  []string{"torrent", "usenet"},
	}, nil
}

func (m *Module) ListIndexers(ctx context.Context, _ *indexerv1.ListIndexersRequest) (*indexerv1.ListIndexersResponse, error) {
	m.mu.RLock()
	name := m.name
	m.mu.RUnlock()
	return &indexerv1.ListIndexersResponse{
		Indexers: []*indexerv1.IndexerInfo{
			{
				Id:         indexerID,
				Name:       name,
				Protocol:   "torrent",
				Language:   "en",
				Configured: m.configured(),
			},
		},
	}, nil
}
