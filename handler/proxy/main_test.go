package proxyhandler

import (
	"os"
	"testing"

	"github.com/tokendancelab/metapi-go/config"
)

func TestMain(m *testing.M) {
	// Initialize config before tests run (PrepareCtx calls config.Get())
	config.Set(&config.Config{
		ProxyMaxChannelAttempts: 10,
	})
	// Most proxy surface tests exercise the historical local stub path.
	// Production keeps the stub disabled unless this flag is set explicitly.
	os.Setenv("METAPI_ENABLE_PROXY_STUB", "1")
	os.Exit(m.Run())
}
