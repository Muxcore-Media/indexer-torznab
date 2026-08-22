package internal

import "strings"

func normalizeIndexerProtocol(proto, url string) string {
	p := strings.ToLower(strings.TrimSpace(proto))
	switch p {
	case "usenet", "nzb", "newznab":
		return "usenet"
	case "torrent", "magnet", "http", "https":
		return "torrent"
	}
	u := strings.ToLower(strings.TrimSpace(url))
	if strings.HasSuffix(u, ".nzb") || strings.Contains(u, "/nzb") {
		return "usenet"
	}
	return "torrent"
}
