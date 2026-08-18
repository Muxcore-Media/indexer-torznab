package internal

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type searchAPI interface {
	Search(ctx context.Context, q torznabQuery) ([]torznabHit, error)
}

type torznabQuery struct {
	Query      string
	Type       string
	Categories []string
	Season     int
	Episode    int
	Limit      int
	Offset     int
}

type torznabHit struct {
	Title       string
	GUID        string
	Link        string
	InfoURL     string
	DownloadURL string
	Size        int64
	Seeders     int32
	Peers       int32
	Category    string
	PubDate     time.Time
	IMDB        string
	TVDB        int32
}

type torznabClient struct {
	base   string
	apiKey string
	http   *http.Client
}

func newTorznabClient(base, apiKey string, hc *http.Client) *torznabClient {
	return &torznabClient{base: normalizeAPIBase(base), apiKey: apiKey, http: hc}
}

func normalizeAPIBase(raw string) string {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return ""
	}
	lower := strings.ToLower(raw)
	if strings.HasSuffix(lower, "/api") {
		return raw
	}
	return raw + "/api"
}

func (c *torznabClient) Search(ctx context.Context, q torznabQuery) ([]torznabHit, error) {
	if c.base == "" {
		return nil, nil
	}
	u, err := url.Parse(c.base)
	if err != nil {
		return nil, fmt.Errorf("torznab url: %w", err)
	}
	vals := u.Query()
	vals.Set("t", torznabT(q.Type))
	vals.Set("q", q.Query)
	if c.apiKey != "" {
		vals.Set("apikey", c.apiKey)
	}
	if cat := categoryCSV(q.Type, q.Categories); cat != "" {
		vals.Set("cat", cat)
	}
	if q.Limit > 0 {
		vals.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.Offset > 0 {
		vals.Set("offset", strconv.Itoa(q.Offset))
	}
	if strings.EqualFold(strings.TrimSpace(q.Type), "tv") {
		if q.Season > 0 {
			vals.Set("season", strconv.Itoa(q.Season))
		}
		if q.Episode > 0 {
			vals.Set("ep", strconv.Itoa(q.Episode))
		}
	}
	u.RawQuery = vals.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/xml, text/xml, */*")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("torznab HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	return parseTorznabRSS(body)
}

func torznabT(searchType string) string {
	switch strings.ToLower(strings.TrimSpace(searchType)) {
	case "movie":
		return "movie"
	case "tv":
		return "tvsearch"
	default:
		return "search"
	}
}

// Newznab/Torznab category IDs (subset).
func categoryCSV(searchType string, categories []string) string {
	var ids []string
	for _, c := range categories {
		c = strings.ToLower(strings.TrimSpace(c))
		if c == "" {
			continue
		}
		if n, err := strconv.Atoi(c); err == nil && n > 0 {
			ids = append(ids, strconv.Itoa(n))
			continue
		}
		switch c {
		case "movie", "movies", "hd-movie":
			ids = append(ids, "2000")
		case "tv", "television", "tvshows", "hd-tv":
			ids = append(ids, "5000")
		case "audio", "music", "flac":
			ids = append(ids, "3000")
		case "book", "ebook", "ebooks":
			ids = append(ids, "7000")
		}
	}
	if len(ids) == 0 {
		switch strings.ToLower(strings.TrimSpace(searchType)) {
		case "movie":
			ids = append(ids, "2000")
		case "tv":
			ids = append(ids, "5000")
		}
	}
	return strings.Join(ids, ",")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// --- XML ---

type rssRoot struct {
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Items []rssItem `xml:"item"`
}

type rssItem struct {
	Title     string         `xml:"title"`
	GUID      rssGUID        `xml:"guid"`
	Link      string         `xml:"link"`
	Comments  string         `xml:"comments"`
	PubDate   string         `xml:"pubDate"`
	Enclosure rssEnclosure   `xml:"enclosure"`
	Attrs     []torznabAttr  `xml:"http://torznab.com/schemas/2015/feed attr"`
	NNAttrs   []torznabAttr  `xml:"http://www.newznab.com/DTD/2010/feeds/attributes/ attr"`
}

type rssGUID struct {
	Value string `xml:",chardata"`
}

type rssEnclosure struct {
	URL    string `xml:"url,attr"`
	Length string `xml:"length,attr"`
	Type   string `xml:"type,attr"`
}

type torznabAttr struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

func parseTorznabRSS(body []byte) ([]torznabHit, error) {
	var root rssRoot
	if err := xml.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("torznab xml: %w", err)
	}
	out := make([]torznabHit, 0, len(root.Channel.Items))
	for _, it := range root.Channel.Items {
		title := strings.TrimSpace(it.Title)
		if title == "" {
			continue
		}
		attrs := map[string]string{}
		for _, a := range append(it.Attrs, it.NNAttrs...) {
			name := strings.ToLower(strings.TrimSpace(a.Name))
			if name == "" {
				continue
			}
			// category may repeat; keep first for display
			if _, ok := attrs[name]; !ok || name != "category" {
				attrs[name] = a.Value
			}
		}
		dl := preferMagnetDownload(attrs["magneturl"], it.Enclosure.URL, it.Link)
		size := parseInt64(attrs["size"])
		if size == 0 {
			size = parseInt64(it.Enclosure.Length)
		}
		seeders := int32(parseInt64(attrs["seeders"]))
		peers := int32(parseInt64(attrs["peers"]))
		if peers == 0 {
			leech := int32(parseInt64(attrs["leechers"]))
			if seeders > 0 || leech > 0 {
				peers = seeders + leech
			}
		}
		guid := strings.TrimSpace(it.GUID.Value)
		if guid == "" {
			guid = strings.TrimSpace(attrs["guid"])
		}
		if guid == "" {
			guid = dl
		}
		info := strings.TrimSpace(it.Comments)
		if info == "" {
			info = strings.TrimSpace(it.Link)
		}
		imdb := strings.TrimSpace(attrs["imdb"])
		if imdb == "" {
			imdb = strings.TrimSpace(attrs["imdbid"])
		}
		if imdb != "" && !strings.HasPrefix(strings.ToLower(imdb), "tt") {
			imdb = "tt" + imdb
		}
		hit := torznabHit{
			Title:       title,
			GUID:        guid,
			Link:        it.Link,
			InfoURL:     info,
			DownloadURL: dl,
			Size:        size,
			Seeders:     seeders,
			Peers:       peers,
			Category:    attrs["category"],
			PubDate:     parsePubDate(it.PubDate),
			IMDB:        imdb,
			TVDB:        int32(parseInt64(attrs["tvdbid"])),
		}
		out = append(out, hit)
	}
	return out, nil
}

func parseInt64(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func preferMagnetDownload(candidates ...string) string {
	var fallback string
	for _, c := range candidates {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(c), "magnet:") {
			return c
		}
		if fallback == "" {
			fallback = c
		}
	}
	return fallback
}

func parsePubDate(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{
		time.RFC1123Z,
		time.RFC1123,
		time.RFC3339,
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
