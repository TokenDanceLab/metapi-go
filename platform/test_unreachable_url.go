package platform

import (
	"net"
	"testing"
)

// unreachableBaseURL returns an HTTP base URL that fails immediately with
// connection refused (closed local listener). Prefer this over fixed ports
// like :1 which may blackhole until dial timeout under WSL/firewall.
func unreachableBaseURL(t testing.TB) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for unreachable URL: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return "http://" + addr
}
