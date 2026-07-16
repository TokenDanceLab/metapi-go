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



func isTruthyJSON(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case float64:
		return t != 0
	case int:
		return t != 0
	case int64:
		return t != 0
	default:
		return false
	}
}

// TestTokenRoutes_ManualChannelSurvivesRebuild covers upstream #409 / backlog #46:
// operator-added in-route models must keep manual_override and survive rebuild.
func TestTokenRoutes_ManualChannelSurvivesRebuild(t *testing.T) {
	db, r := setupTokenRoutesTest(t)
	routeID, accountID, tokenID := seedRouteChannelRefs(t, db)
	now := time.Now().UTC().Format(time.RFC3339)

	// Availability for a different model so rebuild still has auto work to do.
	if _, err := db.Exec(
		`INSERT INTO token_model_availability (token_id, model_name, available, checked_at)
		 VALUES (?, 'gpt-4o', 1, ?)`, tokenID, now); err != nil {
		t.Fatalf("seed availability: %v", err)
	}

	// Operator manually attaches a non-discovered model to the route.
	addResp := doPostJSON(t, r, "/api/routes/"+itoa(routeID)+"/channels", map[string]any{
		"accountId":   accountID,
		"tokenId":     tokenID,
		"sourceModel": "Qwen/Qwen3-Embedding-8B",
		"priority":    7,
		"weight":      3,
	})
	if addResp.Code != http.StatusOK {
		t.Fatalf("add manual channel: %d %s", addResp.Code, addResp.Body.String())
	}
	var added map[string]any
	if err := json.Unmarshal(addResp.Body.Bytes(), &added); err != nil {
		t.Fatalf("decode add response: %v", err)
	}
	if !isTruthyJSON(added["manualOverride"]) {
		t.Fatalf("manualOverride = %v, want true for operator-added channel; body=%s", added["manualOverride"], addResp.Body.String())
	}
	channelID := int64(added["id"].(float64))

	// Rebuild must not delete the intentional channel.
	rebuildResp := doPostJSON(t, r, "/api/routes/rebuild", map[string]any{})
	if rebuildResp.Code != http.StatusOK {
		t.Fatalf("rebuild: %d %s", rebuildResp.Code, rebuildResp.Body.String())
	}

	var manualCount int
	if err := db.Get(&manualCount,
		`SELECT COUNT(*) FROM route_channels
		 WHERE id = ? AND source_model = 'Qwen/Qwen3-Embedding-8B' AND manual_override = 1
		   AND priority = 7 AND weight = 3`, channelID); err != nil {
		t.Fatalf("count manual: %v", err)
	}
	if manualCount != 1 {
		t.Fatalf("manual in-route model wiped or reset by rebuild")
	}

	// Auto channel for gpt-4o should also exist alongside the manual one.
	var autoCount int
	if err := db.Get(&autoCount,
		`SELECT COUNT(*) FROM route_channels WHERE route_id = ? AND source_model = 'gpt-4o'`, routeID); err != nil {
		t.Fatalf("count auto: %v", err)
	}
	if autoCount != 1 {
		t.Fatalf("auto channel missing after rebuild, count=%d", autoCount)
	}
}

// TestTokenRoutes_PartialUpdatePreservesModelMapping covers intentional modelMapping
// updates and ensures unrelated partial updates do not clear mapping or channels.
func TestTokenRoutes_PartialUpdatePreservesModelMapping(t *testing.T) {
	db, r := setupTokenRoutesTest(t)
	routeID, accountID, tokenID := seedRouteChannelRefs(t, db)

	// Seed an intentional mapping + a manual channel.
	mapping := map[string]any{"gpt-*": "gpt-4o"}
	createMapping := doPutJSON(t, r, "/api/routes/"+itoa(routeID), map[string]any{
		"modelMapping": mapping,
	})
	if createMapping.Code != http.StatusOK {
		t.Fatalf("set modelMapping: %d %s", createMapping.Code, createMapping.Body.String())
	}

	addResp := doPostJSON(t, r, "/api/routes/"+itoa(routeID)+"/channels", map[string]any{
		"accountId":   accountID,
		"tokenId":     tokenID,
		"sourceModel": "manual-model-x",
		"priority":    5,
	})
	if addResp.Code != http.StatusOK {
		t.Fatalf("add channel: %d %s", addResp.Code, addResp.Body.String())
	}

	// Unrelated partial update: only displayName.
	partial := doPutJSON(t, r, "/api/routes/"+itoa(routeID), map[string]any{
		"displayName": "renamed-route",
	})
	if partial.Code != http.StatusOK {
		t.Fatalf("partial update: %d %s", partial.Code, partial.Body.String())
	}
	var updated map[string]any
	if err := json.Unmarshal(partial.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode partial: %v", err)
	}
	if updated["displayName"] != "renamed-route" {
		t.Fatalf("displayName = %v, want renamed-route", updated["displayName"])
	}

	// modelMapping must still be present (not clobbered by omit).
	var rawMapping *string
	if err := db.Get(&rawMapping, `SELECT model_mapping FROM token_routes WHERE id = ?`, routeID); err != nil {
		t.Fatalf("load model_mapping: %v", err)
	}
	if rawMapping == nil || *rawMapping == "" {
		t.Fatalf("model_mapping cleared by partial update")
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(*rawMapping), &parsed); err != nil {
		t.Fatalf("parse model_mapping: %v raw=%s", err, *rawMapping)
	}
	if parsed["gpt-*"] != "gpt-4o" {
		t.Fatalf("model_mapping content reset: %v", parsed)
	}

	// Manual channel still present.
	var manualCount int
	if err := db.Get(&manualCount,
		`SELECT COUNT(*) FROM route_channels WHERE route_id = ? AND source_model = 'manual-model-x' AND manual_override = 1`,
		routeID); err != nil {
		t.Fatalf("count manual: %v", err)
	}
	if manualCount != 1 {
		t.Fatalf("manual channel lost after partial route update")
	}

	// Intentional mapping change.
	next := doPutJSON(t, r, "/api/routes/"+itoa(routeID), map[string]any{
		"modelMapping": map[string]any{"gpt-*": "gpt-4o-mini"},
	})
	if next.Code != http.StatusOK {
		t.Fatalf("intentional mapping update: %d %s", next.Code, next.Body.String())
	}
	if err := db.Get(&rawMapping, `SELECT model_mapping FROM token_routes WHERE id = ?`, routeID); err != nil {
		t.Fatalf("reload mapping: %v", err)
	}
	if err := json.Unmarshal([]byte(*rawMapping), &parsed); err != nil {
		t.Fatalf("parse next mapping: %v", err)
	}
	if parsed["gpt-*"] != "gpt-4o-mini" {
		t.Fatalf("intentional mapping update not applied: %v", parsed)
	}
}

// TestTokenRoutes_ChannelUpdatePreservesIntentionalConfig ensures channel edits set
// manual_override and that rebuild keeps the operator-tuned sourceModel/priority.
func TestTokenRoutes_ChannelUpdatePreservesIntentionalConfig(t *testing.T) {
	db, r := setupTokenRoutesTest(t)
	routeID, accountID, tokenID := seedRouteChannelRefs(t, db)
	now := time.Now().UTC().Format(time.RFC3339)

	// Start as an auto-looking channel (simulate insert without override), then edit.
	res, err := db.Exec(
		`INSERT INTO route_channels (route_id, account_id, token_id, source_model, priority, weight, enabled, manual_override)
		 VALUES (?, ?, ?, 'gpt-4o', 0, 10, 1, 0)`, routeID, accountID, tokenID)
	if err != nil {
		t.Fatalf("seed auto channel: %v", err)
	}
	channelID, _ := res.LastInsertId()

	// Intentional edit of source model + priority.
	upd := doPutJSON(t, r, "/api/channels/"+itoa(channelID), map[string]any{
		"sourceModel": "MiMo-V2-Flash-Free",
		"priority":    9,
		"weight":      4,
	})
	if upd.Code != http.StatusOK {
		t.Fatalf("update channel: %d %s", upd.Code, upd.Body.String())
	}
	var updated map[string]any
	if err := json.Unmarshal(upd.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode update: %v", err)
	}
	if !isTruthyJSON(updated["manualOverride"]) {
		t.Fatalf("manualOverride = %v after intentional edit, want true", updated["manualOverride"])
	}
	if updated["sourceModel"] != "MiMo-V2-Flash-Free" {
		t.Fatalf("sourceModel = %v, want MiMo-V2-Flash-Free", updated["sourceModel"])
	}

	// Availability for gpt-4o (original auto model) - rebuild would prefer this if override ignored.
	if _, err := db.Exec(
		`INSERT INTO token_model_availability (token_id, model_name, available, checked_at)
		 VALUES (?, 'gpt-4o', 1, ?)`, tokenID, now); err != nil {
		t.Fatalf("seed avail: %v", err)
	}

	rebuild := doPostJSON(t, r, "/api/routes/rebuild", map[string]any{})
	if rebuild.Code != http.StatusOK {
		t.Fatalf("rebuild: %d %s", rebuild.Code, rebuild.Body.String())
	}

	var kept int
	if err := db.Get(&kept,
		`SELECT COUNT(*) FROM route_channels
		 WHERE id = ? AND source_model = 'MiMo-V2-Flash-Free' AND manual_override = 1
		   AND priority = 9 AND weight = 4`, channelID); err != nil {
		t.Fatalf("count kept: %v", err)
	}
	if kept != 1 {
		t.Fatalf("intentional channel config reset by rebuild")
	}
}
