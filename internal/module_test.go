package internal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	indexerv1 "github.com/Muxcore-Media/contracts-indexer/muxcore/indexer/v1"
)

func TestSearchUnconfiguredNoNetwork(t *testing.T) {
	t.Setenv("TORZNAB_URL", "")
	t.Setenv("TORZNAB_API_KEY", "")
	t.Setenv("TORZNAB_NAME", "")

	var dials int
	hc := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		dials++
		t.Fatal("HTTP must not be called when TORZNAB_URL is empty")
		return nil, nil
	})}

	m := NewModule(Config{GRPCAddr: ":0", BaseURL: "", HTTP: hc})
	ctx := context.Background()
	if err := m.Init(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop(ctx) })

	if m.configured() {
		t.Fatal("expected unconfigured")
	}
	resp, err := m.Search(ctx, &indexerv1.SearchRequest{Query: "Fight Club", Type: "movie"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.GetResults()) != 0 {
		t.Fatalf("expected soft-empty results, got %d", len(resp.GetResults()))
	}
	if dials != 0 {
		t.Fatalf("unexpected dials: %d", dials)
	}

	list, err := m.ListIndexers(ctx, &indexerv1.ListIndexersRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if list.Indexers[0].Configured {
		t.Error("expected configured=false")
	}
}

func TestSearchViaHTTPtest(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "search_sample.xml"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/api") {
			t.Errorf("path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	m := NewModule(Config{
		GRPCAddr: ":0",
		BaseURL:  srv.URL + "/1",
		APIKey:   "secret",
		HTTP:     srv.Client(),
		Name:     "Fixture Torznab",
	})
	ctx := context.Background()
	if err := m.Init(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop(ctx) })

	resp, err := m.Search(ctx, &indexerv1.SearchRequest{
		Query: "Fight Club",
		Type:  "movie",
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(resp.Results))
	}
	if resp.Results[0].IndexerName != "Fixture Torznab" {
		t.Errorf("indexer name: %s", resp.Results[0].IndexerName)
	}
	if !strings.Contains(resp.Results[0].DownloadUrl, "magnet:?xt=urn:btih:") {
		t.Errorf("download_url: %s", resp.Results[0].DownloadUrl)
	}

	caps, err := m.GetCapabilities(ctx, &indexerv1.GetCapabilitiesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !caps.SupportsMovieSearch || !caps.SupportsTvSearch {
		t.Errorf("capabilities: %+v", caps)
	}

	list, err := m.ListIndexers(ctx, &indexerv1.ListIndexersRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !list.Indexers[0].Configured {
		t.Error("expected configured=true")
	}
}

func TestTVSearchViaHTTPtest(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "tv_search_sample.xml"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("t") != "tvsearch" {
			t.Errorf("t: %s", q.Get("t"))
		}
		if q.Get("season") != "1" {
			t.Errorf("season: %s", q.Get("season"))
		}
		if q.Get("ep") != "2" {
			t.Errorf("ep: %s", q.Get("ep"))
		}
		if q.Get("cat") != "5000" {
			t.Errorf("cat: %s", q.Get("cat"))
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	m := NewModule(Config{
		GRPCAddr: ":0",
		BaseURL:  srv.URL + "/1/api",
		HTTP:     srv.Client(),
	})
	ctx := context.Background()
	if err := m.Init(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop(ctx) })

	resp, err := m.Search(ctx, &indexerv1.SearchRequest{
		Query:   "Example Show",
		Type:    "tv",
		Season:  1,
		Episode: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("results: %d", len(resp.Results))
	}
	if resp.Results[0].TvdbId != 12345 {
		t.Errorf("tvdb: %d", resp.Results[0].TvdbId)
	}
}

func TestModuleInfo(t *testing.T) {
	t.Setenv("TORZNAB_URL", "")
	t.Setenv("TORZNAB_GRPC_ADDR", "")
	m := NewModule(Config{})
	info := m.Info()
	if info.ID != "indexer-torznab" {
		t.Errorf("id: %s", info.ID)
	}
	if len(info.Capabilities) == 0 || info.Capabilities[0] != "indexer" {
		t.Errorf("capabilities: %v", info.Capabilities)
	}
	if info.HTTPAddr != ":9486" {
		t.Errorf("HTTPAddr: %s (want :9486; avoid clash with piratebay :9485 / ffprobe :9480)", info.HTTPAddr)
	}
}
