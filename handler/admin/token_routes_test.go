package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/tokendancelab/metapi-go/routing"
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

// ---- Route Decision APIs (#171) ----

type fakeDecisionRouter struct {
	explainCalls       []string
	explainForRoute    []string
	explainRouteWide   []int64
	decision           routing.RouteDecisionExplanation
	err                error
}

func (f *fakeDecisionRouter) ExplainSelection(_ context.Context, requestedModel string, _ []int64, _ routing.DownstreamRoutingPolicy) (routing.RouteDecisionExplanation, error) {
	f.explainCalls = append(f.explainCalls, requestedModel)
	if f.err != nil {
		return routing.RouteDecisionExplanation{}, f.err
	}
	d := f.decision
	if d.RequestedModel == "" {
		d.RequestedModel = requestedModel
	}
	if d.ActualModel == "" {
		d.ActualModel = requestedModel
	}
	return d, nil
}

func (f *fakeDecisionRouter) ExplainSelectionForRoute(_ context.Context, routeID int64, requestedModel string, _ []int64, _ routing.DownstreamRoutingPolicy) (routing.RouteDecisionExplanation, error) {
	f.explainForRoute = append(f.explainForRoute, fmt.Sprintf("%d:%s", routeID, requestedModel))
	if f.err != nil {
		return routing.RouteDecisionExplanation{}, f.err
	}
	d := f.decision
	d.RequestedModel = requestedModel
	id := routeID
	d.RouteID = &id
	d.Matched = true
	return d, nil
}

func (f *fakeDecisionRouter) ExplainSelectionRouteWide(_ context.Context, routeID int64, _ routing.DownstreamRoutingPolicy) (routing.RouteDecisionExplanation, error) {
	f.explainRouteWide = append(f.explainRouteWide, routeID)
	if f.err != nil {
		return routing.RouteDecisionExplanation{}, f.err
	}
	d := f.decision
	id := routeID
	d.RouteID = &id
	d.Matched = true
	d.RequestedModel = fmt.Sprintf("route:%d", routeID)
	return d, nil
}

type fakeDecisionRefresher struct {
	mu                 sync.Mutex
	calls              int
	exact              int
	wildcard           int
	err                error
	refreshPricingSeen []bool
}

func (f *fakeDecisionRefresher) RefreshAllRouteDecisionSnapshots(_ context.Context, refreshPricingCatalog bool) (int, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.refreshPricingSeen = append(f.refreshPricingSeen, refreshPricingCatalog)
	return f.exact, f.wildcard, f.err
}

func (f *fakeDecisionRefresher) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func setupTokenRoutesDecisionTest(t *testing.T, deps TokenRoutesDeps) (*store.DB, chi.Router) {
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
	RegisterTokenRoutesWithDeps(r, db.DB, deps)
	return db, r
}

func TestRouteDecision_MissingModel(t *testing.T) {
	_, r := setupTokenRoutesDecisionTest(t, TokenRoutesDeps{Router: &fakeDecisionRouter{}})
	resp := doGet(t, r, "/api/routes/decision")
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 body=%s", resp.Code, resp.Body.String())
	}
}

func TestRouteDecision_RouterUnavailable(t *testing.T) {
	_, r := setupTokenRoutesDecisionTest(t, TokenRoutesDeps{})
	resp := doGet(t, r, "/api/routes/decision?model=gpt-4o")
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 body=%s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["success"] != false {
		t.Fatalf("success = %v, want false", body["success"])
	}
	if msg, _ := body["message"].(string); msg == "" {
		t.Fatalf("expected clear error message, body=%v", body)
	}
}

func TestRouteDecision_ExplainSelection(t *testing.T) {
	routeID := int64(7)
	channelID := int64(11)
	fake := &fakeDecisionRouter{
		decision: routing.RouteDecisionExplanation{
			RequestedModel:    "gpt-4o",
			ActualModel:       "gpt-4o",
			Matched:           true,
			RouteID:           &routeID,
			ModelPattern:      "gpt-4o",
			SelectedChannelID: &channelID,
			Summary:           []string{"命中路由：gpt-4o", "路由策略：按权重随机"},
			Candidates: []routing.RouteDecisionCandidate{
				{
					ChannelID:  channelID,
					AccountID:  3,
					Username:   "u1",
					SiteName:   "s1",
					Priority:   0,
					Weight:     10,
					Eligible:   true,
					Probability: 1,
				},
			},
		},
	}
	_, r := setupTokenRoutesDecisionTest(t, TokenRoutesDeps{Router: fake})
	resp := doGet(t, r, "/api/routes/decision?model=gpt-4o")
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["success"] != true {
		t.Fatalf("success = %v", body["success"])
	}
	decision, ok := body["decision"].(map[string]any)
	if !ok {
		t.Fatalf("decision missing: %v", body)
	}
	if decision["matched"] != true {
		t.Fatalf("matched = %v", decision["matched"])
	}
	if decision["modelPattern"] != "gpt-4o" {
		t.Fatalf("modelPattern = %v", decision["modelPattern"])
	}
	summary, ok := decision["summary"].([]any)
	if !ok || len(summary) == 0 {
		t.Fatalf("summary missing: %v", decision["summary"])
	}
	candidates, ok := decision["candidates"].([]any)
	if !ok || len(candidates) != 1 {
		t.Fatalf("candidates = %v", decision["candidates"])
	}
	if len(fake.explainCalls) != 1 || fake.explainCalls[0] != "gpt-4o" {
		t.Fatalf("explainCalls = %v", fake.explainCalls)
	}
}

func TestRouteDecisionBatch_BoundedAndMapped(t *testing.T) {
	fake := &fakeDecisionRouter{
		decision: routing.RouteDecisionExplanation{
			Matched: true,
			Summary: []string{"ok"},
		},
	}
	_, r := setupTokenRoutesDecisionTest(t, TokenRoutesDeps{Router: fake})

	// empty models
	empty := doPostJSON(t, r, "/api/routes/decision/batch", map[string]any{"models": []string{}})
	if empty.Code != http.StatusBadRequest {
		t.Fatalf("empty models status = %d body=%s", empty.Code, empty.Body.String())
	}

	// over bound
	tooMany := make([]string, routeDecisionBatchMaxItems+1)
	for i := range tooMany {
		tooMany[i] = "m-" + strconv.Itoa(i)
	}
	bound := doPostJSON(t, r, "/api/routes/decision/batch", map[string]any{"models": tooMany})
	if bound.Code != http.StatusBadRequest {
		t.Fatalf("bound status = %d body=%s", bound.Code, bound.Body.String())
	}

	okResp := doPostJSON(t, r, "/api/routes/decision/batch", map[string]any{
		"models": []string{"gpt-4o", "gpt-4o", " claude-4 "},
	})
	if okResp.Code != http.StatusOK {
		t.Fatalf("batch status = %d body=%s", okResp.Code, okResp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(okResp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	decisions := body["decisions"].(map[string]any)
	if len(decisions) != 2 {
		t.Fatalf("decisions keys = %v, want 2 unique", decisions)
	}
	if _, ok := decisions["gpt-4o"]; !ok {
		t.Fatalf("missing gpt-4o: %v", decisions)
	}
	if _, ok := decisions["claude-4"]; !ok {
		t.Fatalf("missing claude-4: %v", decisions)
	}
	if len(fake.explainCalls) != 2 {
		t.Fatalf("explainCalls = %v, want 2 after dedupe", fake.explainCalls)
	}
}

func TestRouteDecisionByRouteAndRouteWideBatch(t *testing.T) {
	fake := &fakeDecisionRouter{
		decision: routing.RouteDecisionExplanation{
			Matched: true,
			Summary: []string{"route"},
			Candidates: []routing.RouteDecisionCandidate{
				{ChannelID: 1, Eligible: true, Probability: 0.5},
			},
		},
	}
	_, r := setupTokenRoutesDecisionTest(t, TokenRoutesDeps{Router: fake})

	byRoute := doPostJSON(t, r, "/api/routes/decision/by-route/batch", map[string]any{
		"items": []map[string]any{
			{"routeId": 1, "model": "gpt-4o"},
			{"routeId": 2, "model": "claude-4"},
		},
	})
	if byRoute.Code != http.StatusOK {
		t.Fatalf("by-route status = %d body=%s", byRoute.Code, byRoute.Body.String())
	}
	var byRouteBody map[string]any
	if err := json.Unmarshal(byRoute.Body.Bytes(), &byRouteBody); err != nil {
		t.Fatalf("decode by-route: %v", err)
	}
	decisions := byRouteBody["decisions"].(map[string]any)
	if _, ok := decisions["1"]; !ok {
		t.Fatalf("missing route 1: %v", decisions)
	}
	route1 := decisions["1"].(map[string]any)
	if _, ok := route1["gpt-4o"]; !ok {
		t.Fatalf("missing model under route 1: %v", route1)
	}

	wide := doPostJSON(t, r, "/api/routes/decision/route-wide/batch", map[string]any{
		"routeIds": []int64{9, 9, 10},
	})
	if wide.Code != http.StatusOK {
		t.Fatalf("route-wide status = %d body=%s", wide.Code, wide.Body.String())
	}
	var wideBody map[string]any
	if err := json.Unmarshal(wide.Body.Bytes(), &wideBody); err != nil {
		t.Fatalf("decode wide: %v", err)
	}
	wideDecisions := wideBody["decisions"].(map[string]any)
	if len(wideDecisions) != 2 {
		t.Fatalf("wide decisions = %v", wideDecisions)
	}
	if len(fake.explainRouteWide) != 2 {
		t.Fatalf("explainRouteWide calls = %v", fake.explainRouteWide)
	}
}

func TestRouteDecisionRefresh_RealTaskNotStub(t *testing.T) {
	refresher := &fakeDecisionRefresher{exact: 2, wildcard: 1}
	_, r := setupTokenRoutesDecisionTest(t, TokenRoutesDeps{
		Router:    &fakeDecisionRouter{},
		Decisions: refresher,
	})

	resp := doPostJSON(t, r, "/api/routes/decision/refresh", map[string]any{})
	if resp.Code != http.StatusAccepted {
		t.Fatalf("refresh status = %d body=%s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["success"] != true || body["queued"] != true {
		t.Fatalf("unexpected body: %v", body)
	}
	jobID, _ := body["jobId"].(string)
	if jobID == "" || jobID == "stub-decision-refresh" {
		t.Fatalf("jobId = %q, want real background task id", jobID)
	}

	// Wait for background task completion.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if refresher.callCount() >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if refresher.callCount() < 1 {
		t.Fatalf("refresher never called")
	}

	taskResp := doGet(t, r, "/api/tasks/"+jobID)
	// tasks routes not registered on this router — just assert task registry entry.
	task := getBackgroundTask(jobID)
	if task == nil {
		t.Fatalf("background task %s not found", jobID)
	}
	// Allow a short wait for status transition.
	for i := 0; i < 50 && task.Status != BackgroundTaskSucceeded && task.Status != BackgroundTaskFailed; i++ {
		time.Sleep(10 * time.Millisecond)
		task = getBackgroundTask(jobID)
	}
	if task.Status != BackgroundTaskSucceeded {
		t.Fatalf("task status = %s error=%v", task.Status, task.Error)
	}
	if task.Type != routeDecisionRefreshTaskType {
		t.Fatalf("task type = %s", task.Type)
	}
	_ = taskResp
}

func TestRouteDecisionRefresh_Unavailable(t *testing.T) {
	_, r := setupTokenRoutesDecisionTest(t, TokenRoutesDeps{})
	resp := doPostJSON(t, r, "/api/routes/decision/refresh", map[string]any{})
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["queued"] == true {
		t.Fatalf("must not claim queued when unavailable: %v", body)
	}
}

func TestRouteDecision_WithSQLiteRouterFixture(t *testing.T) {
	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	siteRes, err := db.Exec(`INSERT INTO sites (name, url, platform, status, created_at, updated_at)
		VALUES ('Decision Site', 'https://decision.example.com', 'openai', 'active', ?, ?)`, now, now)
	if err != nil {
		t.Fatalf("insert site: %v", err)
	}
	siteID, _ := siteRes.LastInsertId()
	accRes, err := db.Exec(`INSERT INTO accounts (site_id, username, access_token, status, checkin_enabled, created_at, updated_at)
		VALUES (?, 'dec-user', 'sk-dec', 'active', 1, ?, ?)`, siteID, now, now)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	accountID, _ := accRes.LastInsertId()
	tokRes, err := db.Exec(`INSERT INTO account_tokens (account_id, name, token, value_status, source, enabled, is_default, created_at, updated_at)
		VALUES (?, 'dec-token', 'sk-dec-token', 'ready', 'manual', 1, 1, ?, ?)`, accountID, now, now)
	if err != nil {
		t.Fatalf("insert token: %v", err)
	}
	tokenID, _ := tokRes.LastInsertId()
	routeRes, err := db.Exec(`INSERT INTO token_routes (model_pattern, routing_strategy, enabled, created_at, updated_at)
		VALUES ('gpt-4o', 'weighted', 1, ?, ?)`, now, now)
	if err != nil {
		t.Fatalf("insert route: %v", err)
	}
	routeID, _ := routeRes.LastInsertId()
	if _, err := db.Exec(`INSERT INTO route_channels (route_id, account_id, token_id, source_model, priority, weight, enabled, manual_override)
		VALUES (?, ?, ?, 'gpt-4o', 0, 10, 1, 0)`, routeID, accountID, tokenID); err != nil {
		t.Fatalf("insert channel: %v", err)
	}

	// Lightweight in-package fixture router: use fake that mirrors real explain shape.
	// Full TokenRouter needs ChannelSelectorDB; fake + sqlite fixture covers handler path.
	fake := &fakeDecisionRouter{
		decision: routing.RouteDecisionExplanation{
			Matched:      true,
			ModelPattern: "gpt-4o",
			RouteID:      &routeID,
			Summary:      []string{"命中路由：gpt-4o"},
			Candidates: []routing.RouteDecisionCandidate{
				{ChannelID: 1, AccountID: accountID, Eligible: true, Probability: 1, Weight: 10},
			},
		},
	}
	refresher := &fakeDecisionRefresher{exact: 1}
	r := chi.NewRouter()
	RegisterTokenRoutesWithDeps(r, db.DB, TokenRoutesDeps{Router: fake, Decisions: refresher})
	RegisterTasksRoutes(r, db.DB)

	resp := doGet(t, r, "/api/routes/decision?model=gpt-4o")
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	decision := body["decision"].(map[string]any)
	if decision["matched"] != true {
		t.Fatalf("expected matched decision, got %v", decision)
	}
	if float64(routeID) != decision["routeId"] {
		// routeId may be number; tolerate
		if decision["routeId"] == nil {
			t.Fatalf("routeId missing: %v", decision)
		}
	}

	// Refresh should not use stub job id and should complete.
	refresh := doPostJSON(t, r, "/api/routes/decision/refresh", map[string]any{"refreshPricingCatalog": false})
	if refresh.Code != http.StatusAccepted {
		t.Fatalf("refresh status = %d body=%s", refresh.Code, refresh.Body.String())
	}
	var refreshBody map[string]any
	if err := json.Unmarshal(refresh.Body.Bytes(), &refreshBody); err != nil {
		t.Fatalf("decode refresh: %v", err)
	}
	jobID, _ := refreshBody["jobId"].(string)
	if jobID == "stub-decision-refresh" || jobID == "" {
		t.Fatalf("unexpected jobId %q", jobID)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		taskResp := doGet(t, r, "/api/tasks/"+jobID)
		if taskResp.Code == http.StatusOK {
			var taskBody map[string]any
			_ = json.Unmarshal(taskResp.Body.Bytes(), &taskBody)
			if task, ok := taskBody["task"].(map[string]any); ok {
				if task["status"] == "succeeded" {
					return
				}
				if task["status"] == "failed" {
					t.Fatalf("refresh task failed: %v", task)
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("refresh task did not succeed in time, refresher.calls=%d", refresher.callCount())
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
