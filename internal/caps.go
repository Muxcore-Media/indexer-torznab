package internal

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	indexerv1 "github.com/Muxcore-Media/contracts-indexer/muxcore/indexer/v1"
)

type capsRoot struct {
	Searching capsSearching  `xml:"searching"`
	Category  []capsCategory `xml:"categories>category"`
}

type capsSearching struct {
	Search      capsSearchKind `xml:"search"`
	TVSearch    capsSearchKind `xml:"tv-search"`
	MovieSearch capsSearchKind `xml:"movie-search"`
	MusicSearch capsSearchKind `xml:"music-search"`
	BookSearch  capsSearchKind `xml:"book-search"`
}

type capsSearchKind struct {
	Available string `xml:"available,attr"`
}

type capsCategory struct {
	ID   string `xml:"id,attr"`
	Name string `xml:"name,attr"`
}

func parseTorznabCaps(body []byte) (*indexerv1.GetCapabilitiesResponse, error) {
	var root capsRoot
	if err := xml.Unmarshal(body, &root); err != nil {
		// Newznab may return a bare <error> on failure.
		if apiErr := parseTorznabError(body); apiErr != nil {
			return nil, apiErr
		}
		return nil, fmt.Errorf("torznab caps xml: %w", err)
	}
	cats := make([]string, 0, len(root.Category))
	for _, c := range root.Category {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			continue
		}
		cats = append(cats, strings.ToLower(name))
	}
	supportsSearch := capsAvailable(root.Searching.Search) ||
		capsAvailable(root.Searching.MovieSearch) ||
		capsAvailable(root.Searching.TVSearch) ||
		capsAvailable(root.Searching.MusicSearch) ||
		capsAvailable(root.Searching.BookSearch)
	return &indexerv1.GetCapabilitiesResponse{
		SupportsSearch:      supportsSearch,
		SupportsMovieSearch: capsAvailable(root.Searching.MovieSearch),
		SupportsTvSearch:    capsAvailable(root.Searching.TVSearch),
		SupportsMusicSearch: capsAvailable(root.Searching.MusicSearch),
		SupportsBookSearch:  capsAvailable(root.Searching.BookSearch),
		SupportedCategories: cats,
		SupportedProtocols:  []string{"torrent", "usenet"},
	}, nil
}

func capsAvailable(k capsSearchKind) bool {
	switch strings.ToLower(strings.TrimSpace(k.Available)) {
	case "yes", "true", "1":
		return true
	default:
		return false
	}
}

func unconfiguredCapabilities() *indexerv1.GetCapabilitiesResponse {
	return &indexerv1.GetCapabilitiesResponse{}
}

func (c *torznabClient) FetchCapabilities(ctx context.Context) (*indexerv1.GetCapabilitiesResponse, error) {
	if c.base == "" {
		return unconfiguredCapabilities(), nil
	}
	u, err := url.Parse(c.base)
	if err != nil {
		return nil, fmt.Errorf("torznab caps url: %w", err)
	}
	vals := u.Query()
	vals.Set("t", "caps")
	if c.apiKey != "" {
		vals.Set("apikey", c.apiKey)
	}
	u.RawQuery = vals.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/xml, text/xml, */*")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, newRateLimited(fmt.Sprintf("torznab caps HTTP 429: %s", truncate(string(body), 200)))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("torznab caps HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	if apiErr := parseTorznabError(body); apiErr != nil {
		return nil, apiErr
	}
	return parseTorznabCaps(body)
}

func (c *prowlarrClient) FetchCapabilities(ctx context.Context) (*indexerv1.GetCapabilitiesResponse, error) {
	if c.base == "" {
		return unconfiguredCapabilities(), nil
	}
	// Prowlarr exposes per-indexer caps; aggregate search modes from configured indexers list.
	indexers, err := c.ListIndexers(ctx)
	if err != nil {
		return nil, err
	}
	if len(indexers) == 0 {
		return unconfiguredCapabilities(), nil
	}
	return &indexerv1.GetCapabilitiesResponse{
		SupportsSearch:      true,
		SupportsMovieSearch: true,
		SupportsTvSearch:    true,
		SupportsMusicSearch: true,
		SupportsBookSearch:  true,
		SupportedCategories: []string{"movie", "tv", "audio", "book", "other"},
		SupportedProtocols:  []string{"torrent", "usenet"},
	}, nil
}

func (c *prowlarrClient) ProbeHealth(ctx context.Context) error {
	if c.base == "" {
		return nil
	}
	u := c.base + "/api/v1/system/status"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return err
	}
	if c.apiKey != "" {
		req.Header.Set("X-Api-Key", c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusTooManyRequests {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return newRateLimited("prowlarr status HTTP 429")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("prowlarr status HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	return nil
}

func (c *torznabClient) ProbeHealth(ctx context.Context) error {
	if c.base == "" {
		return nil
	}
	u, err := url.Parse(c.base)
	if err != nil {
		return fmt.Errorf("torznab health url: %w", err)
	}
	vals := u.Query()
	vals.Set("t", "caps")
	if c.apiKey != "" {
		vals.Set("apikey", c.apiKey)
	}
	u.RawQuery = vals.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), http.NoBody)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusTooManyRequests {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return newRateLimited("torznab caps HTTP 429")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("torznab caps HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if apiErr := parseTorznabError(body); apiErr != nil {
		return apiErr
	}
	return nil
}
