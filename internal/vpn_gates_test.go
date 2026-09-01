package internal

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoteInitRequiresWGConf(t *testing.T) {
	t.Setenv("WG_CONF", "")
	m := NewModule(Config{
		GRPCAddr: ":0",
		BaseURL:  "https://prowlarr.example.test/1",
	})
	err := m.Init(context.Background())
	if err == nil || !strings.Contains(err.Error(), "WG_CONF") {
		t.Fatalf("expected WG_CONF gate, got %v", err)
	}
}

func TestLoopbackInitWithoutVPN(t *testing.T) {
	t.Setenv("WG_CONF", "")
	m := NewModule(Config{
		GRPCAddr: ":0",
		BaseURL:  "http://127.0.0.1:9696/1",
	})
	if err := m.Init(context.Background()); err != nil {
		t.Fatalf("loopback should not require WG_CONF: %v", err)
	}
	t.Cleanup(func() { _ = m.Stop(context.Background()) })
}

func TestEmptyURLInitWithoutVPN(t *testing.T) {
	t.Setenv("WG_CONF", "")
	m := NewModule(Config{GRPCAddr: ":0", BaseURL: ""})
	if err := m.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop(context.Background()) })
	caps, err := m.GetCapabilities(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if caps.SupportsSearch {
		t.Fatal("unconfigured must not advertise search")
	}
}

func TestRemoteMissingWGFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.conf")
	m := NewModule(Config{
		GRPCAddr:   ":0",
		BaseURL:    "https://jackett.example/api",
		WGConfPath: missing,
	})
	err := m.Init(context.Background())
	if err == nil || !strings.Contains(err.Error(), "WG_CONF") {
		t.Fatalf("expected missing WG error, got %v", err)
	}
}

func TestIsLoopbackOrEmptyURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		url  string
		want bool
	}{
		{"", true},
		{"http://localhost:1/api", true},
		{"http://127.0.0.1:9696/1", true},
		{"http://[::1]:9696/1", true},
		{"http://192.168.1.10:9696/1", true},
		{"http://10.0.0.5:9696/1", true},
		{"http://172.16.0.2:9696/1", true},
		{"http://prowlarr:9696/1", true},
		{"http://host.local:9696/1", true},
		{"https://prowlarr.lan/1", false},
		{"https://prowlarr.example.test/1", false},
	}
	for _, tc := range cases {
		if got := isLoopbackOrEmptyURL(tc.url); got != tc.want {
			t.Errorf("isLoopbackOrEmptyURL(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
}

func TestDockerHostnameInitWithoutVPN(t *testing.T) {
	t.Setenv("WG_CONF", "")
	m := NewModule(Config{
		GRPCAddr: ":0",
		BaseURL:  "http://prowlarr:9696/1",
	})
	if err := m.Init(context.Background()); err != nil {
		t.Fatalf("docker hostname should not require WG_CONF: %v", err)
	}
	t.Cleanup(func() { _ = m.Stop(context.Background()) })
}
