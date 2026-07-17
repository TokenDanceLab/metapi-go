package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tokendancelab/metapi-go/config"
	proxyhandler "github.com/tokendancelab/metapi-go/handler/proxy"
	"github.com/tokendancelab/metapi-go/store"
)

func TestHealthAndReadySemantics(t *testing.T) {
	_ = store.CloseDatabase()
	defer store.CloseDatabase()

	rec := httptest.NewRecorder()
	Health(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("health without db: got %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("health body = %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	Ready(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("ready without db: got %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}

	dataDir := t.TempDir()
	cfg := &config.Config{
		DbType:  store.DialectSQLite,
		DbUrl:   filepath.Join(dataDir, "ready.db"),
		DataDir: dataDir,
	}
	if err := store.EnsureRuntimeDatabase(cfg); err != nil {
		t.Fatalf("EnsureRuntimeDatabase: %v", err)
	}

	rec = httptest.NewRecorder()
	Ready(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("ready with db: got %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"database":"ok"`) {
		t.Fatalf("ready body = %s", rec.Body.String())
	}
}

func TestShutdownDrainsBeforeCleanupAndRunsCleanupOnce(t *testing.T) {
	_ = store.CloseDatabase()
	dataDir := t.TempDir()
	cfg := &config.Config{
		DbType:  store.DialectSQLite,
		DbUrl:   filepath.Join(dataDir, "shutdown.db"),
		DataDir: dataDir,
	}
	if err := store.EnsureRuntimeDatabase(cfg); err != nil {
		t.Fatalf("EnsureRuntimeDatabase: %v", err)
	}
	t.Cleanup(func() {
		_ = store.CloseDatabase()
		setReadinessDrainingForTest(false)
	})

	started := make(chan struct{})
	release := make(chan struct{})
	var cleanupCount atomic.Int64

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusNoContent)
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	a := New(&config.Config{}, nil)
	a.Server = ts.Config
	a.RegisterOnClose(func() {
		cleanupCount.Add(1)
	})

	clientDone := make(chan error, 1)
	go func() {
		resp, err := http.Get(ts.URL)
		if err == nil {
			resp.Body.Close()
		}
		clientDone <- err
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not start")
	}

	shutdownDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		shutdownDone <- a.Shutdown(ctx)
	}()

	select {
	case err := <-shutdownDone:
		t.Fatalf("shutdown returned before in-flight handler drained: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if got := cleanupCount.Load(); got != 0 {
		t.Fatalf("cleanup ran before HTTP drain completed: got %d", got)
	}

	rec := httptest.NewRecorder()
	Ready(rec, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), `"status":"draining"`) {
		t.Fatalf("ready during shutdown = %d %s, want 503 draining", rec.Code, rec.Body.String())
	}

	close(release)
	if err := <-shutdownDone; err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if err := <-clientDone; err != nil {
		t.Fatalf("client request: %v", err)
	}
	if got := cleanupCount.Load(); got != 1 {
		t.Fatalf("cleanup count after first shutdown = %d, want 1", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := a.Shutdown(ctx); err != nil {
		t.Fatalf("second Shutdown: %v", err)
	}
	if got := cleanupCount.Load(); got != 1 {
		t.Fatalf("cleanup count after second shutdown = %d, want 1", got)
	}
}

func TestNewHTTPServerUsesHardenedDefaults(t *testing.T) {
	handler := http.NewServeMux()

	server := newHTTPServer("127.0.0.1:0", handler)

	if server.Addr != "127.0.0.1:0" {
		t.Fatalf("Addr = %q, want %q", server.Addr, "127.0.0.1:0")
	}
	if server.Handler != handler {
		t.Fatal("Handler was not installed")
	}
	if server.ReadHeaderTimeout != 10*time.Second {
		t.Fatalf("ReadHeaderTimeout = %s, want %s", server.ReadHeaderTimeout, 10*time.Second)
	}
	if server.ReadTimeout != 30*time.Second {
		t.Fatalf("ReadTimeout = %s, want %s", server.ReadTimeout, 30*time.Second)
	}
	if server.WriteTimeout != 60*time.Second {
		t.Fatalf("WriteTimeout = %s, want %s", server.WriteTimeout, 60*time.Second)
	}
	if server.IdleTimeout != 120*time.Second {
		t.Fatalf("IdleTimeout = %s, want %s", server.IdleTimeout, 120*time.Second)
	}
	if server.MaxHeaderBytes != 1<<20 {
		t.Fatalf("MaxHeaderBytes = %d, want %d", server.MaxHeaderBytes, 1<<20)
	}
}

// TestWireResponsesWebsocketTransport_RegistersResidual covers #217 boot path:
// App.Start calls WireResponsesWebsocketTransport so Ensure is not a silent uncalled stub.
func TestWireResponsesWebsocketTransport_RegistersResidual(t *testing.T) {
	proxyhandler.ResetResponsesWebsocketTransportForTest()
	if proxyhandler.ResponsesWebsocketTransportRegistered() {
		t.Fatal("expected unregistered before wire")
	}

	a := New(&config.Config{ListenHost: "127.0.0.1", Port: 0}, http.NewServeMux())
	a.Server = newHTTPServer("127.0.0.1:0", a.Router)
	a.WireResponsesWebsocketTransport()

	if !proxyhandler.ResponsesWebsocketTransportRegistered() {
		t.Fatal("WireResponsesWebsocketTransport must call EnsureResponsesWebsocketTransport")
	}

	// nil / missing server must not panic
	(&App{}).WireResponsesWebsocketTransport()
	var nilApp *App
	nilApp.WireResponsesWebsocketTransport()
}
