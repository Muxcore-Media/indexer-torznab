package internal

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func ifaceFromWGConf(path string) string {
	base := filepath.Base(strings.TrimSpace(path))
	ext := filepath.Ext(base)
	if ext != "" {
		return strings.TrimSuffix(base, ext)
	}
	return base
}

func interfaceIPv4(iface string) (net.IP, error) {
	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		return nil, fmt.Errorf("vpn iface %q: %w", iface, err)
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return nil, err
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		if ip4 := ipnet.IP.To4(); ip4 != nil {
			return ip4, nil
		}
	}
	return nil, fmt.Errorf("vpn iface %q has no IPv4 address", iface)
}

func newVPNBoundHTTPClient(wgConf string, timeout time.Duration) (*http.Client, string, error) {
	wgConf = strings.TrimSpace(wgConf)
	if wgConf == "" {
		wgConf = strings.TrimSpace(os.Getenv("WG_CONF"))
	}
	if err := validateWGConfReadable(wgConf); err != nil {
		return nil, "", err
	}
	iface := ifaceFromWGConf(wgConf)
	localIP, err := interfaceIPv4(iface)
	if err != nil {
		return nil, "", err
	}
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	dialer := &net.Dialer{
		Timeout:   timeout,
		KeepAlive: 30 * time.Second,
		LocalAddr: &net.TCPAddr{IP: localIP},
		Control: func(network, address string, c syscall.RawConn) error {
			var ctrlErr error
			if err := c.Control(func(fd uintptr) {
				ctrlErr = syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, iface)
			}); err != nil {
				return err
			}
			return ctrlErr
		},
	}
	tr := &http.Transport{
		Proxy:               nil,
		DialContext:         dialer.DialContext,
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        8,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 15 * time.Second,
	}
	return &http.Client{Timeout: timeout, Transport: tr}, iface, nil
}

func isLoopbackOrEmptyURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return true
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		lower := strings.ToLower(raw)
		return strings.HasPrefix(lower, "http://127.0.0.1") ||
			strings.HasPrefix(lower, "https://127.0.0.1") ||
			strings.HasPrefix(lower, "http://localhost") ||
			strings.HasPrefix(lower, "https://localhost") ||
			strings.HasPrefix(lower, "http://[::1]") ||
			strings.HasPrefix(lower, "https://[::1]")
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	if strings.HasSuffix(host, ".local") {
		return true
	}
	// Docker Compose / k8s service names (no dot).
	if !strings.Contains(host, ".") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
	}
	return false
}

func validateWGConfReadable(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("WG_CONF is required for remote Torznab HTTP (set WG_CONF, or use loopback/empty TORZNAB_URL)")
	}
	//nolint:gosec // WG_CONF is an operator-controlled deployment path, not request input
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("WG_CONF missing or unreadable (%s): %w", path, err)
	}
	if fi.IsDir() {
		return fmt.Errorf("WG_CONF is a directory, not a file: %s", path)
	}
	//nolint:gosec // WG_CONF is an operator-controlled deployment path, not request input
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("WG_CONF unreadable (%s): %w", path, err)
	}
	_ = f.Close()
	return nil
}

func liveRemoteRequiresVPN(baseURL string, httpInjected bool) bool {
	if httpInjected {
		return false
	}
	return !isLoopbackOrEmptyURL(baseURL)
}

func enforceRemoteTorznabVPN(wgConf, baseURL string, httpInjected bool) error {
	if !liveRemoteRequiresVPN(baseURL, httpInjected) {
		return nil
	}
	path := strings.TrimSpace(wgConf)
	if path == "" {
		path = strings.TrimSpace(os.Getenv("WG_CONF"))
	}
	return validateWGConfReadable(path)
}
