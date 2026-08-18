package internal

import (
	"fmt"
	"net"
	"net/http"
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
	u := strings.ToLower(raw)
	switch {
	case strings.HasPrefix(u, "http://127.0.0.1"), strings.HasPrefix(u, "https://127.0.0.1"),
		strings.HasPrefix(u, "http://localhost"), strings.HasPrefix(u, "https://localhost"),
		strings.HasPrefix(u, "http://[::1]"), strings.HasPrefix(u, "https://[::1]"):
		return true
	default:
		return false
	}
}

func validateWGConfReadable(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("WG_CONF is required for remote Torznab HTTP (set WG_CONF, or use loopback/empty TORZNAB_URL)")
	}
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("WG_CONF missing or unreadable (%s): %w", path, err)
	}
	if fi.IsDir() {
		return fmt.Errorf("WG_CONF is a directory, not a file: %s", path)
	}
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
