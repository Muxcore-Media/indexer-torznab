package internal

import (
	"net/http"
	"strings"
	"time"

	"github.com/Muxcore-Media/core/sdk/go/module/netguard"
)

// indexerGuardOptions is the Integration profile for a Torznab/Prowlarr
// endpoint. A household indexer lives on the LAN or loopback; public indexers
// are also allowed. Link-local, cloud metadata, and non-HTTP schemes stay
// refused. A VPN-bound transport still uses this check before dialing; that
// transport cannot use netguard's dialer, so DNS rebinding on the VPN path
// remains residual.
func indexerGuardOptions(timeout time.Duration) netguard.Options {
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	return netguard.Options{
		AllowPrivate:  true,
		AllowLoopback: true,
		Timeout:       timeout,
	}
}

func newGuardedClient(timeout time.Duration) *http.Client {
	return netguard.NewClient(netguard.Integration, indexerGuardOptions(timeout))
}

func guardOutboundURL(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return netguard.ValidateURL(raw, netguard.Integration, indexerGuardOptions(90*time.Second))
}

func doGuarded(client *http.Client, req *http.Request) (*http.Response, error) {
	if req != nil && req.URL != nil {
		if err := guardOutboundURL(req.URL.String()); err != nil {
			return nil, err
		}
	}
	return client.Do(req)
}
