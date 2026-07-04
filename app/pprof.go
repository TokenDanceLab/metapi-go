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
	addr := ":" + strconv.Itoa(port)
	go func() {
		slog.Info("pprof debug server listening", "addr", addr)
		if err := http.ListenAndServe(addr, nil); err != nil {
			slog.Error("pprof debug server failed", "error", err)
		}
	}()
}
