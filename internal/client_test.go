package internal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	indexerv1 "github.com/Muxcore-Media/contracts-indexer/muxcore/indexer/v1"
)

func TestParseTorznabFixture(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "search_sample.xml"))
	if err != nil {
		t.Fatal(err)
	}
	hits, err := parseTorznabRSS(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits: %d", len(hits))
	}
	if hits[0].Title != "Fight.Club.1999.1080p.BluRay.x264" {
		t.Errorf("title: %q", hits[0].Title)
	}
	if hits[0].Seeders != 42 {
		t.Errorf("seeders: %d", hits[0].Seeders)
	}
	if hits[0].Peers != 45 {
		t.Errorf("peers (seeders+leechers): %d", hits[0].Peers)
	}
	if hits[0].Size != 8589934592 {
		t.Errorf("size: %d", hits[0].Size)
	}
	if hits[0].IMDB != "tt0137523" {
		t.Errorf("imdb: %q", hits[0].IMDB)
	}
	if hits[1].Peers != 20 {
		t.Errorf("peers attr: %d", hits[1].Peers)
	}
}

func TestParseTorznabPrefersMagnetURL(t *testing.T) {
	body := []byte(`<?xml version="1.0"?>
<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed">
  <channel>
    <item>
      <title>Show.S01E01</title>
      <enclosure url="http://127.0.0.1:9696/2/download?apikey=x" length="100" type="application/x-bittorrent" />
      <torznab:attr name="magneturl" value="magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" />
      <torznab:attr name="seeders" value="3" />
    </item>
  </channel>
</rss>`)
	hits, err := parseTorznabRSS(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits %d", len(hits))
	}
	if hits[0].DownloadURL != "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("download url %q", hits[0].DownloadURL)
	}
}

func TestParseTorznabPrefersMagnetLinkOverHTTPEnclosure(t *testing.T) {
	body := []byte(`<?xml version="1.0"?>
<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed">
  <channel>
    <item>
      <title>Show.S01E01</title>
      <link>magnet:?xt=urn:btih:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb</link>
      <enclosure url="http://127.0.0.1:9696/2/download?apikey=x" length="100" type="application/x-bittorrent" />
      <torznab:attr name="seeders" value="3" />
    </item>
  </channel>
</rss>`)
	hits, err := parseTorznabRSS(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits %d", len(hits))
	}
	if hits[0].DownloadURL != "magnet:?xt=urn:btih:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("download url %q", hits[0].DownloadURL)
	}
}

func TestPreferMagnetDownload(t *testing.T) {
	t.Parallel()
	got := preferMagnetDownload("http://proxy/dl", "magnet:?xt=urn:btih:aa", "")
	if got != "magnet:?xt=urn:btih:aa" {
		t.Fatalf("got %q", got)
	}
	got = preferMagnetDownload("http://proxy/dl", "", "https://details")
	if got != "http://proxy/dl" {
		t.Fatalf("http fallback %q", got)
	}
}

func TestTorznabClientEmptyBaseNoNetwork(t *testing.T) {
	var dials int
	hc := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		dials++
		t.Fatal("must not dial")
		return nil, nil
	})}
	c := newTorznabClient("", "key", hc)
	hits, err := c.Search(context.Background(), torznabQuery{Query: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if hits != nil || dials != 0 {
		t.Fatalf("hits=%v dials=%d", hits, dials)
	}
}

func TestTorznabClientHTTPtest(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "search_sample.xml"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/1/api" {
			t.Errorf("path: %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("t") != "movie" {
			t.Errorf("t: %s", q.Get("t"))
		}
		if q.Get("q") != "Fight Club" {
			t.Errorf("q: %s", q.Get("q"))
		}
		if q.Get("apikey") != "secret" {
			t.Errorf("apikey: %s", q.Get("apikey"))
		}
		if q.Get("cat") != "2000" {
			t.Errorf("cat: %s", q.Get("cat"))
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	c := newTorznabClient(srv.URL+"/1", "secret", srv.Client())
	hits, err := c.Search(context.Background(), torznabQuery{
		Query: "Fight Club",
		Type:  "movie",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits: %d", len(hits))
	}
}

func TestSearchUnconfigured(t *testing.T) {
	t.Setenv("TORZNAB_URL", "")
	t.Setenv("PROWLARR_URL", "")
	m := NewModule(Config{GRPCAddr: ":0"})
	if m.configured() {
		t.Fatal("expected unconfigured")
	}
	resp, err := m.Search(context.Background(), &indexerv1.SearchRequest{Query: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.GetResults()) != 0 {
		t.Fatalf("results: %d", len(resp.GetResults()))
	}
}

func TestNormalizeAPIBase(t *testing.T) {
	if got := normalizeAPIBase("http://x/1"); got != "http://x/1/api" {
		t.Errorf("got %q", got)
	}
	if got := normalizeAPIBase("http://x/1/api/"); got != "http://x/1/api" {
		t.Errorf("got %q", got)
	}
}

func TestProwlarrSearchJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/search" {
			t.Errorf("path %s", r.URL.Path)
		}
		if r.Header.Get("X-Api-Key") != "k" {
			t.Errorf("api key %q", r.Header.Get("X-Api-Key"))
		}
		if r.URL.Query().Get("type") != "tvsearch" {
			t.Errorf("type %q", r.URL.Query().Get("type"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"title":"Show.S01E01","guid":"g1","magnetUrl":"magnet:?xt=urn:btih:abc","size":100,"seeders":3,"leechers":1,"indexer":"Knaben"}]`))
	}))
	t.Cleanup(srv.Close)

	c := newProwlarrClient(srv.URL, "k", srv.Client())
	hits, err := c.Search(context.Background(), torznabQuery{Query: "Show S01", Type: "tv", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Title != "Show.S01E01" || hits[0].DownloadURL != "magnet:?xt=urn:btih:abc" {
		t.Fatalf("hits %+v", hits)
	}
}

func TestProwlarrURLConfiguresWithoutTorznabURL(t *testing.T) {
	t.Setenv("TORZNAB_URL", "")
	t.Setenv("PROWLARR_URL", "http://127.0.0.1:9696")
	t.Setenv("PROWLARR_API_KEY", "k")
	m := NewModule(Config{GRPCAddr: ":0", HTTP: &http.Client{}, SkipVPNGate: true})
	if !m.configured() || !m.prowlarr {
		t.Fatalf("configured=%v prowlarr=%v", m.configured(), m.prowlarr)
	}
}

func TestParseTorznabStripsAPIKeyFromEnclosure(t *testing.T) {
	body := []byte(`<?xml version="1.0"?>
<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed">
  <channel>
    <item>
      <title>Show.S01E01</title>
      <enclosure url="http://127.0.0.1:9696/2/download?apikey=secret&amp;id=1" length="100" type="application/x-bittorrent" />
      <torznab:attr name="seeders" value="3" />
    </item>
  </channel>
</rss>`)
	hits, err := parseTorznabRSS(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits %d", len(hits))
	}
	if strings.Contains(hits[0].DownloadURL, "apikey") {
		t.Fatalf("download url must not contain apikey: %q", hits[0].DownloadURL)
	}
}

func TestParseTorznabEmptySample(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "empty_sample.xml"))
	if err != nil {
		t.Fatal(err)
	}
	hits, err := parseTorznabRSS(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("expected 0 hits, got %d", len(hits))
	}
}

func TestParseTorznabErrorSampleRateLimit(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "error_sample.xml"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = parseTorznabRSS(body)
	if err == nil {
		t.Fatal("expected error")
	}
	if !isResourceExhausted(err) {
		t.Fatalf("expected rate limit error, got %v", err)
	}
}

func TestParseTorznabCapsFixture(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("testdata", "caps_sample.xml"))
	if err != nil {
		t.Fatal(err)
	}
	caps, err := parseTorznabCaps(body)
	if err != nil {
		t.Fatal(err)
	}
	if !caps.SupportsMovieSearch || !caps.SupportsTvSearch || !caps.SupportsMusicSearch || !caps.SupportsBookSearch {
		t.Fatalf("caps: %+v", caps)
	}
}

func TestTorznabClientMusicSearchParams(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("t") != "music" {
			t.Errorf("t: %s", q.Get("t"))
		}
		if q.Get("cat") != "3000" {
			t.Errorf("cat: %s", q.Get("cat"))
		}
		if q.Get("year") != "1999" {
			t.Errorf("year: %s", q.Get("year"))
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		body, _ := os.ReadFile(filepath.Join("testdata", "empty_sample.xml"))
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	c := newTorznabClient(srv.URL+"/1", "", srv.Client())
	_, err := c.Search(context.Background(), torznabQuery{Query: "album", Type: "music", Year: 1999})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTorznabClientHTTP429(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("slow down"))
	}))
	t.Cleanup(srv.Close)

	c := newTorznabClient(srv.URL+"/1", "", srv.Client())
	_, err := c.Search(context.Background(), torznabQuery{Query: "x"})
	if err == nil || !isResourceExhausted(err) {
		t.Fatalf("expected rate limit, got %v", err)
	}
}

func TestSearchRateLimitResourceExhausted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	m := NewModule(Config{
		GRPCAddr: ":0",
		BaseURL:  srv.URL + "/1",
		HTTP:     srv.Client(),
	})
	ctx := context.Background()
	if err := m.Init(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop(ctx) })

	_, err := m.Search(ctx, &indexerv1.SearchRequest{Query: "x", Type: "movie"})
	if err == nil {
		t.Fatal("expected error")
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.ResourceExhausted {
		t.Fatalf("code: %v err: %v", st.Code(), err)
	}
}

func TestProwlarrListIndexers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/indexer":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":3,"name":"Knaben","protocol":"torrent","language":"en-US","enable":true}]`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)

	c := newProwlarrClient(srv.URL, "k", srv.Client())
	indexers, err := c.ListIndexers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(indexers) != 1 || indexers[0].ID != 3 || indexers[0].Name != "Knaben" {
		t.Fatalf("indexers %+v", indexers)
	}
}

func TestProwlarrSearchIndexerIDsAndOffset(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/search" {
			t.Errorf("path %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("indexerIds") != "3,5" {
			t.Errorf("indexerIds %q", q.Get("indexerIds"))
		}
		if q.Get("offset") != "10" {
			t.Errorf("offset %q", q.Get("offset"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)

	c := newProwlarrClient(srv.URL, "k", srv.Client())
	_, err := c.Search(context.Background(), torznabQuery{
		Query:      "Show",
		Type:       "tv",
		Offset:     10,
		IndexerIDs: []int32{3, 5},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestProwlarrReleaseFieldsMapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"title":"Album","guid":"g1","downloadUrl":"magnet:?xt=urn:btih:abc","size":100,"seeders":3,"leechers":1,"indexer":"Knaben","indexerId":7,"imdbId":"0137523","tmdbId":550,"tvdbId":0,"protocol":"torrent","category":"2000"}]`))
	}))
	t.Cleanup(srv.Close)

	c := newProwlarrClient(srv.URL, "k", srv.Client())
	hits, err := c.Search(context.Background(), torznabQuery{Query: "Album", Type: "music"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits %+v", hits)
	}
	h := hits[0]
	if h.IndexerName != "Knaben" || h.IndexerID != 7 || h.Category != "2000" || h.IMDB != "tt0137523" || h.TMDB != 550 {
		t.Fatalf("hit %+v", h)
	}
	results := mapHits(hits, "Torznab")
	if results[0].IndexerName != "Knaben" || results[0].IndexerId != 7 || results[0].Category != "2000" {
		t.Fatalf("result %+v", results[0])
	}
}

func TestHealthUpstreamDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") == "caps" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
	}))
	t.Cleanup(srv.Close)

	m := NewModule(Config{GRPCAddr: ":0", BaseURL: srv.URL + "/1", HTTP: srv.Client()})
	ctx := context.Background()
	if err := m.Init(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop(ctx) })
	if err := m.Health(ctx); err == nil {
		t.Fatal("expected health failure")
	}
}

func TestModuleListIndexersProwlarr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/indexer" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":3,"name":"Knaben","protocol":"torrent","language":"en-US","enable":true}]`))
		}
	}))
	t.Cleanup(srv.Close)

	m := NewModule(Config{
		GRPCAddr: ":0",
		BaseURL:  srv.URL,
		APIKey:   "k",
		HTTP:     srv.Client(),
	})
	// Force prowlarr mode without env
	m.prowlarr = true
	m.baseURL = srv.URL
	m.bindUpstreamClients(srv.Client())

	ctx := context.Background()
	list, err := m.ListIndexers(ctx, &indexerv1.ListIndexersRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Indexers) != 1 || list.Indexers[0].Name != "Knaben" || list.Indexers[0].Id != 3 {
		t.Fatalf("list %+v", list.Indexers)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
