package scheduler

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/tokendancelab/metapi-go/config"
)

// OAuthLoopbackScheduler manages local HTTP callback listeners for OAuth flows.
// This is not a cron scheduler but a persistent listener that handles OAuth
// redirect callbacks from providers like Claude, Codex, and Gemini CLI.
type OAuthLoopbackScheduler struct {
	cfg      *config.Config
	servers  []*http.Server
	listeners []net.Listener
	mu       sync.Mutex
	running  bool
	stopCh   chan struct{}
}

// NewOAuthLoopbackScheduler creates a new OAuth loopback callback scheduler.
func NewOAuthLoopbackScheduler(cfg *config.Config) *OAuthLoopbackScheduler {
	return &OAuthLoopbackScheduler{
		cfg: cfg,
	}
}

func (s *OAuthLoopbackScheduler) Name() string { return "oauth-loopback" }

func (s *OAuthLoopbackScheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil
	}

	s.stopCh = make(chan struct{})
	s.running = true

	// Start OAuth callback servers for each provider
	providers := []struct {
		name string
		port int
	}{
		{"claude", 9844},
		{"codex", 9845},
		{"gemini-cli", 9846},
	}

	for _, p := range providers {
		go s.startProviderServer(p.name, p.port)
	}

	slog.Info("oauth-loopback scheduler started", "providers", len(providers))
	return nil
}

func (s *OAuthLoopbackScheduler) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}
	s.running = false

	if s.stopCh != nil {
		close(s.stopCh)
	}

	// Shutdown all servers
	for _, srv := range s.servers {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		srv.Shutdown(ctx)
		cancel()
	}

	// Close all listeners
	for _, ln := range s.listeners {
		ln.Close()
	}

	s.servers = nil
	s.listeners = nil

	slog.Info("oauth-loopback scheduler stopped")
	return nil
}

func (s *OAuthLoopbackScheduler) startProviderServer(name string, port int) {
	mux := http.NewServeMux()

	// Generic callback handler
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		slog.Info("oauth-loopback: received callback",
			"provider", name,
			"method", r.Method,
			"path", r.URL.Path,
			"query", r.URL.RawQuery,
		)

		// Return success page
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<!DOCTYPE html>
<html><head><title>OAuth Callback Received</title></head>
<body><h1>OAuth Callback Received</h1>
<p>The authentication callback has been received. You may close this window.</p>
</body></html>`))
	})

	addr := net.JoinHostPort("127.0.0.1", intToStr(port))

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		slog.Warn("oauth-loopback: failed to listen",
			"provider", name,
			"addr", addr,
			"error", err,
		)
		return
	}

	s.mu.Lock()
	s.listeners = append(s.listeners, listener)
	s.mu.Unlock()

	srv := &http.Server{
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	s.mu.Lock()
	s.servers = append(s.servers, srv)
	s.mu.Unlock()

	slog.Info("oauth-loopback: provider server started",
		"provider", name,
		"addr", addr,
	)

	if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
		slog.Warn("oauth-loopback: provider server stopped unexpectedly",
			"provider", name,
			"error", err,
		)
	}
}

func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}
	digits := make([]byte, 0, 10)
	for n > 0 {
		digits = append(digits, byte('0'+n%10))
		n /= 10
	}
	// reverse
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	return sign + string(digits)
}
