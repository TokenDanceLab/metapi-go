package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsWebsocketUpgradeRequest(t *testing.T) {
	t.Parallel()

	upgrade := httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	upgrade.Header.Set("Upgrade", "websocket")
	upgrade.Header.Set("Connection", "keep-alive, Upgrade")
	if !isWebsocketUpgradeRequest(upgrade) {
		t.Error("expected upgrade request")
	}

	plain := httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	if isWebsocketUpgradeRequest(plain) {
		t.Error("plain GET must not look like upgrade")
	}

	upgradeOnly := httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	upgradeOnly.Header.Set("Upgrade", "websocket")
	if isWebsocketUpgradeRequest(upgradeOnly) {
		t.Error("Upgrade without Connection: upgrade must not match")
	}

	if isWebsocketUpgradeRequest(nil) {
		t.Error("nil request must be false")
	}
}
