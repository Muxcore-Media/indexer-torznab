package internal

import (
	"errors"
	"testing"

	"github.com/Muxcore-Media/core/sdk/go/module/netguard"
)

func TestGuardOutboundURL(t *testing.T) {
	for _, raw := range []string{
		"http://127.0.0.1:9696/api/v1/indexer",
		"http://prowlarr:9696/1/api",
		"https://indexer.example/api",
	} {
		if err := guardOutboundURL(raw); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
	}
	for _, raw := range []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://metadata.google.internal/",
		"file:///etc/passwd",
	} {
		if err := guardOutboundURL(raw); !errors.Is(err, netguard.ErrBlocked) {
			t.Fatalf("%s: err=%v", raw, err)
		}
	}
}
