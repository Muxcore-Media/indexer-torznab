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
	if !caps.SupportsSearch {
		t.Fatal("caps should work without TORZNAB_URL")
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
	if !isLoopbackOrEmptyURL("") || !isLoopbackOrEmptyURL("http://localhost:1/api") {
		t.Fatal("expected loopback/empty true")
	}
	if isLoopbackOrEmptyURL("https://prowlarr.lan/1") {
		t.Fatal("remote should not be loopback")
	}
}
