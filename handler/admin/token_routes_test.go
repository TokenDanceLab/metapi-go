package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tokendancelab/metapi-go/store"
)

func setupTokenRoutesTest(t *testing.T) (*store.DB, chi.Router) {
	t.Helper()
	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("failed to open SQLite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}

	r := chi.NewRouter()
	RegisterTokenRoutes(r, db.DB)
	return db, r
}

func setupTokenRoutesPostgresTest(t *testing.T) (*store.DB, chi.Router) {
	t.Helper()
	db, r := setupTokensPostgresTest(t)
	RegisterTokenRoutes(r, db.DB)
	return db, r
}

func seedRouteChannelRefs(t *testing.T, db *store.DB) (routeID, accountID, tokenID int64) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.Exec(
		`INSERT INTO sites (name, url, platform, status, created_at, updated_at)
		 VALUES ('Route Site', 'https://route.example.com', 'openai', 'active', ?, ?)`,
		now, now,
	)
	if err != nil {
		t.Fatalf("insert site: %v", err)
	}
	siteID, _ := res.LastInsertId()

	res, err = db.Exec(
		`INSERT INTO accounts (site_id, access_token, status, checkin_enabled, created_at, updated_at)
		 VALUES (?, 'session-token', 'active', 1, ?, ?)`,
		siteID, now, now,
	)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	accountID, _ = res.LastInsertId()

	res, err = db.Exec(
		`INSERT INTO account_tokens (account_id, name, token, value_status, source, enabled, is_default, created_at, updated_at)
		 VALUES (?, 'route-token', 'sk-route-token', 'ready', 'manual', 1, 0, ?, ?)`,
		accountID, now, now,
	)
	if err != nil {
		t.Fatalf("insert token: %v", err)
	}
	tokenID, _ = res.LastInsertId()

	res, err = db.Exec(
		`INSERT INTO token_routes (model_pattern, enabled, created_at, updated_at)
		 VALUES ('gpt-*', 1, ?, ?)`,
		now, now,
	)
	if err != nil {
		t.Fatalf("insert route: %v", err)
	}
	routeID, _ = res.LastInsertId()
	return routeID, accountID, tokenID
}

func TestTokenRoutes_Rebuild_InvalidatesCacheTruthfully(t *testing.T) {
	db, r := setupTokenRoutesTest(t)
	now := time.Now().UTC().Format(time.RFC3339)
	routeID, accountID, tokenID := seedRouteChannelRefs(t, db)
	if _, err := db.Exec(
		`INSERT INTO token_model_availability (token_id, model_name, available, checked_at)
		 VALUES (?, 'gpt-4o', 1, ?)`, tokenID, now); err != nil {
		t.Fatalf("seed availability: %v", err)
	}
	// Ensure pattern route exists (seed creates gpt-*)
	_ = routeID
	_ = accountID

	resp := doPostJSON(t, r, "/api/routes/rebuild", map[string]any{})
	if resp.Code != http.StatusOK {
		t.Fatalf("rebuild status = %d, want 200 body=%s", resp.Code, resp.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode rebuild response: %v", err)
	}
	if result["success"] != true {
		t.Fatalf("success = %v, want true", result["success"])
	}
	if result["queued"] != false {
		t.Fatalf("queued = %v, want false (rebuild is synchronous)", result["queued"])
	}
	if result["status"] != "completed" {
		t.Fatalf("status = %v, want completed", result["status"])
	}
	if _, ok := result["jobId"]; ok {
		t.Fatalf("unexpected fake jobId in truthful rebuild response: %v", result["jobId"])
	}
	// Stats surface must be present for operators (not a silent cache-only no-op).
	if _, ok := result["channelsInserted"]; !ok {
		t.Fatalf("missing channelsInserted in rebuild response: %v", result)
	}
	if _, ok := result["patternRoutes"]; !ok {
		t.Fatalf("missing patternRoutes in rebuild response: %v", result)
	}

	var chCount int
	if err := db.Get(&chCount, `SELECT COUNT(*) FROM route_channels WHERE route_id = ?`, routeID); err != nil {
		t.Fatalf("count channels: %v", err)
	}
	if chCount < 1 {
		t.Fatalf("rebuild did not materialize matching channels, count=%d", chCount)
	}
}

func TestRouteChannelsAllowSameCredentialForDifferentSourceModels(t *testing.T) {
	db, r := setupTokenRoutesTest(t)
	routeID, accountID, tokenID := seedRouteChannelRefs(t, db)

	first := doPostJSON(t, r, "/api/routes/"+itoa(routeID)+"/channels", map[string]any{
		"accountId":   accountID,
		"tokenId":     tokenID,
		"sourceModel": "gpt-4o",
	})
	if first.Code != http.StatusOK {
		t.Fatalf("first channel returned %d: %s", first.Code, first.Body.String())
	}

	second := doPostJSON(t, r, "/api/routes/"+itoa(routeID)+"/channels", map[string]any{
		"accountId":   accountID,
		"tokenId":     tokenID,
		"sourceModel": "gpt-4o-mini",
	})
	if second.Code != http.StatusOK {
		t.Fatalf("second channel returned %d: %s", second.Code, second.Body.String())
	}
}

func TestTokenRoutes_Postgres_CreateUpdateChannelAndDelete(t *testing.T) {
	db, r := setupTokenRoutesPostgresTest(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	siteURL := "https://pg-routes-" + suffix + ".example.com"
	t.Cleanup(func() {
		_, _ = db.Exec("DELETE FROM sites WHERE url = ?", siteURL)
	})

	siteResp := doPostJSON(t, r, "/api/sites", map[string]any{
		"name":     "PG Routes " + suffix,
		"url":      siteURL,
		"platform": "openai",
	})
	if siteResp.Code != http.StatusOK {
		t.Fatalf("postgres create site: %d %s", siteResp.Code, siteResp.Body.String())
	}
	var site map[string]any
	if err := json.Unmarshal(siteResp.Body.Bytes(), &site); err != nil {
		t.Fatalf("unmarshal site: %v", err)
	}
	siteID := int64(site["id"].(float64))

	accountResp := doPostJSON(t, r, "/api/accounts", map[string]any{
		"siteId":      siteID,
		"accessToken": "pg-route-session-" + suffix,
		"username":    "pg-route-user-" + suffix,
	})
	if accountResp.Code != http.StatusOK {
		t.Fatalf("postgres create account: %d %s", accountResp.Code, accountResp.Body.String())
	}
	var account map[string]any
	if err := json.Unmarshal(accountResp.Body.Bytes(), &account); err != nil {
		t.Fatalf("unmarshal account: %v", err)
	}
	accountID := int64(account["id"].(float64))

	tokenResp := doPostJSON(t, r, "/api/account-tokens", map[string]any{
		"accountId": accountID,
		"token":     "sk-pg-route-token-" + suffix,
		"isDefault": true,
	})
	if tokenResp.Code != http.StatusOK {
		t.Fatalf("postgres create token: %d %s", tokenResp.Code, tokenResp.Body.String())
	}
	var tokenResult map[string]any
	if err := json.Unmarshal(tokenResp.Body.Bytes(), &tokenResult); err != nil {
		t.Fatalf("unmarshal token: %v", err)
	}
	token := tokenResult["token"].(map[string]any)
	tokenID := int64(token["id"].(float64))

	routeResp := doPostJSON(t, r, "/api/routes", map[string]any{
		"modelPattern": "pg-route-" + suffix + "-*",
		"displayName":  "PG Route " + suffix,
		"enabled":      true,
	})
	if routeResp.Code != http.StatusOK {
		t.Fatalf("postgres create route: expected 200, got %d: %s", routeResp.Code, routeResp.Body.String())
	}
	var route map[string]any
	if err := json.Unmarshal(routeResp.Body.Bytes(), &route); err != nil {
		t.Fatalf("unmarshal route: %v", err)
	}
	routeID := int64(route["id"].(float64))

	updateRouteResp := doPutJSON(t, r, "/api/routes/"+itoa(routeID), map[string]any{
		"displayName":     "PG Route Updated " + suffix,
		"routingStrategy": "priority",
		"enabled":         true,
	})
	if updateRouteResp.Code != http.StatusOK {
		t.Fatalf("postgres update route: expected 200, got %d: %s", updateRouteResp.Code, updateRouteResp.Body.String())
	}

	channelResp := doPostJSON(t, r, "/api/routes/"+itoa(routeID)+"/channels", map[string]any{
		"accountId":   accountID,
		"tokenId":     tokenID,
		"sourceModel": "pg-source-" + suffix,
		"priority":    1,
		"weight":      10,
	})
	if channelResp.Code != http.StatusOK {
		t.Fatalf("postgres add channel: expected 200, got %d: %s", channelResp.Code, channelResp.Body.String())
	}
	var channel map[string]any
	if err := json.Unmarshal(channelResp.Body.Bytes(), &channel); err != nil {
		t.Fatalf("unmarshal channel: %v", err)
	}
	channelID := int64(channel["id"].(float64))

	updateChannelResp := doPutJSON(t, r, "/api/channels/"+itoa(channelID), map[string]any{
		"enabled":  false,
		"priority": 2,
	})
	if updateChannelResp.Code != http.StatusOK {
		t.Fatalf("postgres update channel: expected 200, got %d: %s", updateChannelResp.Code, updateChannelResp.Body.String())
	}

	clearResp := doPostJSON(t, r, "/api/routes/"+itoa(routeID)+"/cooldown/clear", nil)
	if clearResp.Code != http.StatusOK {
		t.Fatalf("postgres clear cooldown: expected 200, got %d: %s", clearResp.Code, clearResp.Body.String())
	}

	deleteChannelResp := doDelete(t, r, "/api/channels/"+itoa(channelID))
	if deleteChannelResp.Code != http.StatusOK {
		t.Fatalf("postgres delete channel: expected 200, got %d: %s", deleteChannelResp.Code, deleteChannelResp.Body.String())
	}

	deleteRouteResp := doDelete(t, r, "/api/routes/"+itoa(routeID))
	if deleteRouteResp.Code != http.StatusOK {
		t.Fatalf("postgres delete route: expected 200, got %d: %s", deleteRouteResp.Code, deleteRouteResp.Body.String())
	}
}

func TestRouteChannelsBatchAllowsSameCredentialForDifferentSourceModels(t *testing.T) {
	db, r := setupTokenRoutesTest(t)
	routeID, accountID, tokenID := seedRouteChannelRefs(t, db)

	resp := doPostJSON(t, r, "/api/routes/"+itoa(routeID)+"/channels/batch", map[string]any{
		"channels": []map[string]any{
			{"accountId": accountID, "tokenId": tokenID, "sourceModel": "gpt-4o"},
			{"accountId": accountID, "tokenId": tokenID, "sourceModel": "gpt-4o-mini"},
		},
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("batch returned %d: %s", resp.Code, resp.Body.String())
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM route_channels WHERE route_id = ?", routeID).Scan(&count); err != nil {
		t.Fatalf("count channels: %v", err)
	}
	if count != 2 {
		t.Fatalf("route channel count = %d, want 2; body=%s", count, resp.Body.String())
	}
}
