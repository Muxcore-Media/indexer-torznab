package internal

import "testing"

func TestDetectDownloadProtocol(t *testing.T) {
	if got := detectDownloadProtocol("https://indexer/get.php?id=1", "application/x-nzb"); got != "usenet" {
		t.Fatalf("nzb enclosure: %s", got)
	}
	if got := detectDownloadProtocol("magnet:?xt=urn:btih:abc", ""); got != "torrent" {
		t.Fatalf("magnet: %s", got)
	}
}

func TestPreferDownloadURLPrefersNZB(t *testing.T) {
	got := preferDownloadURL("magnet:?xt=urn:btih:abc", "https://indexer/get.php?guid=x")
	if got != "magnet:?xt=urn:btih:abc" {
		t.Fatalf("magnet when no nzb: %q", got)
	}
	got = preferDownloadURL("magnet:?xt=urn:btih:abc", "https://indexer/file.nzb")
	if got != "https://indexer/file.nzb" {
		t.Fatalf("nzb wins: %q", got)
	}
}

func TestNormalizeIndexerProtocol(t *testing.T) {
	if normalizeIndexerProtocol("usenet", "") != "usenet" {
		t.Fatal("usenet proto")
	}
	if normalizeIndexerProtocol("", "https://x/y.nzb") != "usenet" {
		t.Fatal("nzb url")
	}
}
