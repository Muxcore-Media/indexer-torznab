package internal

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type searchAPI interface {
	Search(ctx context.Context, q torznabQuery) ([]torznabHit, error)
}

type torznabQuery struct { //nolint:govet // fieldalignment: stable query field order
	Query      string
	Type       string
	Categories []string
	Season     int
	Episode    int
	Year       int
	Limit      int
	Offset     int
	IndexerIDs []int32
}

type torznabHit struct { //nolint:govet // fieldalignment: stable API field order
	Title       string
	GUID        string
	Link        string
	InfoURL     string
	DownloadURL string
	Protocol    string
	Size        int64
	Seeders     int32
	Peers       int32
	Category    string
	PubDate     time.Time
	IMDB        string
	TMDB        int32
	TVDB        int32
	IndexerName string
	IndexerID   int32
}

type torznabClient struct { //nolint:govet // fieldalignment: http client last for readability
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
	if q.Year > 0 {
		vals.Set("year", strconv.Itoa(q.Year))
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, newRateLimited(fmt.Sprintf("torznab HTTP 429: %s", truncate(string(body), 200)))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("torznab HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	if apiErr := parseTorznabError(body); apiErr != nil {
		return nil, apiErr
	}
	return parseTorznabRSS(body)
}

func torznabT(searchType string) string {
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
		case "book", "ebook", "ebooks", "audiobook":
			ids = append(ids, "7000")
		}
	}
	if len(ids) == 0 {
		switch strings.ToLower(strings.TrimSpace(searchType)) {
		case "movie":
			ids = append(ids, "2000")
		case "tv":
			ids = append(ids, "5000")
		case "music":
			ids = append(ids, "3000")
		case "book", "audiobook":
			ids = append(ids, "7000")
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

type rssRoot struct { //nolint:govet // fieldalignment: XML field order
	Channel rssChannel   `xml:"channel"`
	Error   torznabError `xml:"error"`
}

type torznabError struct {
	Code        string `xml:"code,attr"`
	Description string `xml:"description,attr"`
}

type rssChannel struct {
	Items []rssItem `xml:"item"`
}

type rssItem struct {
	Title     string        `xml:"title"`
	GUID      rssGUID       `xml:"guid"`
	Link      string        `xml:"link"`
	Comments  string        `xml:"comments"`
	PubDate   string        `xml:"pubDate"`
	Enclosure rssEnclosure  `xml:"enclosure"`
	Attrs     []torznabAttr `xml:"http://torznab.com/schemas/2015/feed attr"`
	NNAttrs   []torznabAttr `xml:"http://www.newznab.com/DTD/2010/feeds/attributes/ attr"`
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

func parseTorznabError(body []byte) error {
	var errEl struct {
		XMLName     xml.Name `xml:"error"`
		Code        string   `xml:"code,attr"`
		Description string   `xml:"description,attr"`
	}
	if err := xml.Unmarshal(body, &errEl); err != nil {
		return nil //nolint:nilerr // body is not a standalone Newznab <error> document
	}
	if errEl.XMLName.Local != "error" && errEl.Code == "" && errEl.Description == "" {
		return nil
	}
	code := strings.TrimSpace(errEl.Code)
	desc := strings.TrimSpace(errEl.Description)
	msg := "torznab error"
	if code != "" {
		msg += " code=" + code
	}
	if desc != "" {
		msg += ": " + desc
	}
	if code == "429" || strings.Contains(strings.ToLower(desc), "rate limit") {
		return newRateLimited(msg)
	}
	return fmt.Errorf("%s", msg)
}

func parseTorznabRSS(body []byte) ([]torznabHit, error) {
	if apiErr := parseTorznabError(body); apiErr != nil {
		return nil, apiErr
	}
	var root rssRoot
	if err := xml.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("torznab xml: %w", err)
	}
	if root.Error.Code != "" || root.Error.Description != "" {
		code := strings.TrimSpace(root.Error.Code)
		desc := strings.TrimSpace(root.Error.Description)
		msg := "torznab error"
		if code != "" {
			msg += " code=" + code
		}
		if desc != "" {
			msg += ": " + desc
		}
		if code == "429" || strings.Contains(strings.ToLower(desc), "rate limit") {
			return nil, newRateLimited(msg)
		}
		return nil, fmt.Errorf("%s", msg)
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
		dl := stripSensitiveQueryParams(preferDownloadURL(attrs["magneturl"], it.Enclosure.URL, it.Link))
		size := parseInt64(attrs["size"])
		if size == 0 {
			size = parseInt64(it.Enclosure.Length)
		}
		seeders := parseInt32(attrs["seeders"])
		peers := parseInt32(attrs["peers"])
		if peers == 0 {
			leech := parseInt32(attrs["leechers"])
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
		info := stripSensitiveQueryParams(strings.TrimSpace(it.Comments))
		if info == "" {
			info = stripSensitiveQueryParams(strings.TrimSpace(it.Link))
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
			Protocol:    detectDownloadProtocol(dl, it.Enclosure.Type),
			Size:        size,
			Seeders:     seeders,
			Peers:       peers,
			Category:    attrs["category"],
			PubDate:     parsePubDate(it.PubDate),
			IMDB:        imdb,
			TVDB:        parseInt32(attrs["tvdbid"]),
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

func parseInt32(s string) int32 {
	n := parseInt64(s)
	if n > int64(math.MaxInt32) {
		return math.MaxInt32
	}
	if n < int64(math.MinInt32) {
		return math.MinInt32
	}
	return int32(n)
}

func preferDownloadURL(candidates ...string) string {
	var nzb, magnet, fallback string
	for _, c := range candidates {
		c = stripSensitiveQueryParams(strings.TrimSpace(c))
		if c == "" {
			continue
		}
		lower := strings.ToLower(c)
		if strings.HasSuffix(lower, ".nzb") || strings.Contains(lower, "/nzb") {
			if nzb == "" {
				nzb = c
			}
			continue
		}
		if strings.HasPrefix(lower, "magnet:") {
			if magnet == "" {
				magnet = c
			}
			continue
		}
		if fallback == "" {
			fallback = c
		}
	}
	if nzb != "" {
		return nzb
	}
	if magnet != "" {
		return magnet
	}
	return fallback
}

var sensitiveQueryKeys = map[string]struct{}{
	"apikey": {}, "api_key": {}, "passkey": {}, "token": {}, "authkey": {},
}

func stripSensitiveQueryParams(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || (!strings.Contains(raw, "?") && !strings.Contains(raw, "&")) {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return raw
	}
	q := u.Query()
	changed := false
	for key := range q {
		if _, ok := sensitiveQueryKeys[strings.ToLower(key)]; ok {
			q.Del(key)
			changed = true
		}
	}
	if !changed {
		return raw
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func detectDownloadProtocol(rawURL, enclosureType string) string {
	u := strings.ToLower(strings.TrimSpace(rawURL))
	t := strings.ToLower(strings.TrimSpace(enclosureType))
	if strings.HasSuffix(u, ".nzb") || strings.Contains(u, "/nzb") || strings.Contains(t, "nzb") {
		return "usenet"
	}
	if strings.HasPrefix(u, "magnet:") || strings.HasSuffix(u, ".torrent") {
		return "torrent"
	}
	return "torrent"
}

func preferMagnetDownload(candidates ...string) string {
	return preferDownloadURL(candidates...)
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
