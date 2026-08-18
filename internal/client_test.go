package internal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
