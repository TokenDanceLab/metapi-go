package proxy

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
	os.Exit(m.Run())
}
