//go:build debug

package app

import (
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"strconv"
)

// StartDebugServer starts a pprof debugging server on the given port.
// Only compiled when the "debug" build tag is set (go build -tags debug).
func StartDebugServer(port int) {
	server := newDebugServer(port, http.DefaultServeMux)
	go func() {
		slog.Info("pprof debug server listening", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil {
			slog.Error("pprof debug server failed", "error", err)
		}
	}()
}

func newDebugServer(port int, handler http.Handler) *http.Server {
	addr := "127.0.0.1:" + strconv.Itoa(port)
	return newHTTPServer(addr, handler)
}
