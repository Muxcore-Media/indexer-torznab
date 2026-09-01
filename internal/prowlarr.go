package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type prowlarrClient struct { //nolint:govet // fieldalignment: http client last for readability
	base   string
	apiKey string
	http   *http.Client
}

func newProwlarrClient(base, apiKey string, hc *http.Client) *prowlarrClient {
	return &prowlarrClient{base: strings.TrimRight(strings.TrimSpace(base), "/"), apiKey: apiKey, http: hc}
}

type prowlarrRelease struct { //nolint:govet // fieldalignment: JSON field order matches API
	GUID        string    `json:"guid"`
	Title       string    `json:"title"`
	InfoURL     string    `json:"infoUrl"`
	DownloadURL string    `json:"downloadUrl"`
	MagnetURL   string    `json:"magnetUrl"`
	Size        int64     `json:"size"`
	Seeders     int32     `json:"seeders"`
	Leechers    int32     `json:"leechers"`
	Indexer     string    `json:"indexer"`
	IndexerID   int32     `json:"indexerId"`
	IMDBID      string    `json:"imdbId"`
	TMDBID      int32     `json:"tmdbId"`
	TVDBID      int32     `json:"tvdbId"`
	Protocol    string    `json:"protocol"`
	Category    string    `json:"category"`
	PublishDate time.Time `json:"publishDate"`
}

type prowlarrIndexer struct { //nolint:govet // fieldalignment: JSON field order matches API
	ID       int32  `json:"id"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Language string `json:"language"`
	Enable   bool   `json:"enable"`
	Priority int32  `json:"priority"`
}

func (c *prowlarrClient) ListIndexers(ctx context.Context) ([]prowlarrIndexer, error) {
	if c.base == "" {
		return nil, nil
	}
	u := c.base + "/api/v1/indexer"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("X-Api-Key", c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, newRateLimited(fmt.Sprintf("prowlarr indexer HTTP 429: %s", truncate(string(body), 200)))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("prowlarr indexer HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var indexers []prowlarrIndexer
	if err := json.Unmarshal(body, &indexers); err != nil {
		return nil, fmt.Errorf("prowlarr indexer json: %w", err)
	}
	return indexers, nil
}

func (c *prowlarrClient) Search(ctx context.Context, q torznabQuery) ([]torznabHit, error) {
	if c.base == "" {
		return nil, nil
	}
	u, err := url.Parse(c.base + "/api/v1/search")
	if err != nil {
		return nil, fmt.Errorf("prowlarr url: %w", err)
	}
	vals := u.Query()
	vals.Set("query", q.Query)
	vals.Set("type", prowlarrType(q.Type))
	if cat := categoryCSV(q.Type, q.Categories); cat != "" {
		vals.Set("categories", cat)
	}
	if q.Limit > 0 {
		vals.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.Offset > 0 {
		vals.Set("offset", strconv.Itoa(q.Offset))
	}
	if q.Year > 0 {
		vals.Set("year", strconv.Itoa(q.Year))
	}
	if len(q.IndexerIDs) > 0 {
		parts := make([]string, 0, len(q.IndexerIDs))
		for _, id := range q.IndexerIDs {
			if id > 0 {
				parts = append(parts, strconv.Itoa(int(id)))
			}
		}
		if len(parts) > 0 {
			vals.Set("indexerIds", strings.Join(parts, ","))
		}
	}
	u.RawQuery = vals.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("X-Api-Key", c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, newRateLimited(fmt.Sprintf("prowlarr HTTP 429: %s", truncate(string(body), 200)))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("prowlarr HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var releases []prowlarrRelease
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("prowlarr json: %w", err)
	}
	hits := make([]torznabHit, 0, len(releases))
	for _, r := range releases {
		dl := preferDownloadURL(r.MagnetURL, r.DownloadURL)
		guid := r.GUID
		if guid == "" {
			guid = dl
		}
		imdb := strings.TrimSpace(r.IMDBID)
		if imdb != "" && !strings.HasPrefix(strings.ToLower(imdb), "tt") {
			imdb = "tt" + imdb
		}
		proto := strings.TrimSpace(r.Protocol)
		if proto == "" {
			proto = detectDownloadProtocol(dl, "")
		}
		hits = append(hits, torznabHit{
			Title:       r.Title,
			GUID:        guid,
			Link:        r.InfoURL,
			InfoURL:     stripSensitiveQueryParams(r.InfoURL),
			DownloadURL: dl,
			Protocol:    proto,
			Size:        r.Size,
			Seeders:     r.Seeders,
			Peers:       r.Seeders + r.Leechers,
			Category:    r.Category,
			PubDate:     r.PublishDate,
			IMDB:        imdb,
			TMDB:        r.TMDBID,
			TVDB:        r.TVDBID,
			IndexerName: r.Indexer,
			IndexerID:   r.IndexerID,
		})
	}
	return hits, nil
}

func prowlarrType(searchType string) string {
	switch strings.ToLower(strings.TrimSpace(searchType)) {
	case "movie":
		return "movie"
	case "tv":
		return "tvsearch"
	case "music":
		return "music"
	case "book", "audiobook":
		return "book"
	default:
		return "search"
	}
}
