//go:build debug

package app

import (
	"net/http"
	"testing"
	"time"
)

func TestNewDebugServerIsLocalAndHardened(t *testing.T) {
	server := newDebugServer(6060, http.NewServeMux())

	if server.Addr != "127.0.0.1:6060" {
		t.Fatalf("Addr = %q, want %q", server.Addr, "127.0.0.1:6060")
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
