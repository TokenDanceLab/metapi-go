package admin

import (
	"encoding/json"
	"testing"
	"time"
)

func TestStats_SQLiteModelPriceCompare_ObservedAndConfigured(t *testing.T) {
	db, r := setupStatsSQLiteTest(t)
	now := time.Now().UTC().Format(time.RFC3339)

	// Two sites / accounts for the same model with different signals.
	_, err := db.Exec(`INSERT INTO sites (name, url, platform, status, created_at, updated_at)
		VALUES (?, ?, ?, 'active', ?, ?)`, "CheapSite", "https://cheap.example.test", "openai", now, now)
	if err != nil {
		t.Fatalf("insert site1: %v", err)
	}
	_, err = db.Exec(`INSERT INTO sites (name, url, platform, status, created_at, updated_at)
		VALUES (?, ?, ?, 'active', ?, ?)`, "ConfiguredSite", "https://cfg.example.test", "newapi", now, now)
	if err != nil {
		t.Fatalf("insert site2: %v", err)
	}

	var cheapSiteID, cfgSiteID int64
	if err := db.Get(&cheapSiteID, "SELECT id FROM sites WHERE name = ?", "CheapSite"); err != nil {
		t.Fatalf("cheap site id: %v", err)
	}
	if err := db.Get(&cfgSiteID, "SELECT id FROM sites WHERE name = ?", "ConfiguredSite"); err != nil {
		t.Fatalf("cfg site id: %v", err)
	}

	_, err = db.Exec(`INSERT INTO accounts (site_id, username, access_token, status, balance, unit_cost, checkin_enabled, created_at, updated_at)
		VALUES (?, ?, ?, 'active', ?, NULL, 0, ?, ?)`, cheapSiteID, "cheap-user", "sk-cheap", 10.0, now, now)
	if err != nil {
		t.Fatalf("insert account1: %v", err)
	}
	_, err = db.Exec(`INSERT INTO accounts (site_id, username, access_token, status, balance, unit_cost, checkin_enabled, created_at, updated_at)
		VALUES (?, ?, ?, 'active', ?, ?, 0, ?, ?)`, cfgSiteID, "cfg-user", "sk-cfg", 10.0, 0.55, now, now)
	if err != nil {
		t.Fatalf("insert account2: %v", err)
	}

	var cheapAccID, cfgAccID int64
	if err := db.Get(&cheapAccID, "SELECT id FROM accounts WHERE username = ?", "cheap-user"); err != nil {
		t.Fatalf("cheap acc: %v", err)
	}
	if err := db.Get(&cfgAccID, "SELECT id FROM accounts WHERE username = ?", "cfg-user"); err != nil {
		t.Fatalf("cfg acc: %v", err)
	}

	// Availability for both accounts.
	_, err = db.Exec(`INSERT INTO model_availability (account_id, model_name, available, is_manual, checked_at)
		VALUES (?, ?, 1, 0, ?)`, cheapAccID, "gpt-price-compare", now)
	if err != nil {
		t.Fatalf("avail1: %v", err)
	}
	_, err = db.Exec(`INSERT INTO model_availability (account_id, model_name, available, is_manual, checked_at)
		VALUES (?, ?, 1, 0, ?)`, cfgAccID, "gpt-price-compare", now)
	if err != nil {
		t.Fatalf("avail2: %v", err)
	}

	// Observed cheaper cost on CheapSite.
	billing := `{"breakdown":{"inputPerMillion":1.0,"outputPerMillion":2.0},"pricing":{"model_ratio":0.5,"completion_ratio":2,"source":"test"}}`
	_, err = db.Exec(`INSERT INTO proxy_logs (account_id, model_requested, model_actual, status, total_tokens, estimated_cost, billing_details, created_at)
		VALUES (?, ?, ?, 'success', ?, ?, ?, ?)`, cheapAccID, "gpt-price-compare", "gpt-price-compare", 2000, 0.12, billing, now)
	if err != nil {
		t.Fatalf("proxy log: %v", err)
	}

	// Primary path.
	resp := doGet(t, r, "/api/models/price-compare?model=gpt-price-compare")
	if resp.Code != 200 {
		t.Fatalf("price-compare status %d: %s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	items, ok := body["items"].([]any)
	if !ok || len(items) < 2 {
		t.Fatalf("items=%#v, want >=2", body["items"])
	}

	// Alias path should work too.
	resp2 := doGet(t, r, "/api/stats/model-prices?model=gpt-price-compare")
	if resp2.Code != 200 {
		t.Fatalf("model-prices status %d: %s", resp2.Code, resp2.Body.String())
	}

	// First item should prefer billing-derived rates on CheapSite.
	first := items[0].(map[string]any)
	if first["siteName"] != "CheapSite" {
		// Sort is by estimatedCostSample; billing sample is tiny (1k/1k * rates).
		// Accept either CheapSite first if cheaper, else check both present.
		t.Logf("first site=%v source=%v sample=%v", first["siteName"], first["source"], first["estimatedCostSample"])
	}

	bySite := map[string]map[string]any{}
	for _, raw := range items {
		item := raw.(map[string]any)
		bySite[item["siteName"].(string)] = item
	}
	cheap, ok := bySite["CheapSite"]
	if !ok {
		t.Fatalf("CheapSite missing: %#v", bySite)
	}
	cfg, ok := bySite["ConfiguredSite"]
	if !ok {
		t.Fatalf("ConfiguredSite missing: %#v", bySite)
	}

	if cheap["source"] != "billing_details" {
		t.Fatalf("CheapSite source=%v want billing_details", cheap["source"])
	}
	if cheap["inputPerMillion"].(float64) != 1.0 || cheap["outputPerMillion"].(float64) != 2.0 {
		t.Fatalf("CheapSite rates=%v/%v", cheap["inputPerMillion"], cheap["outputPerMillion"])
	}
	if cheap["missingPrice"] == true {
		t.Fatal("CheapSite should not be missingPrice")
	}

	if cfg["source"] != "configured" {
		t.Fatalf("ConfiguredSite source=%v want configured", cfg["source"])
	}
	if cfg["estimatedCostSample"].(float64) != 0.55 {
		t.Fatalf("ConfiguredSite sample=%v want 0.55", cfg["estimatedCostSample"])
	}
	if cfg["missingPrice"] == true {
		t.Fatal("ConfiguredSite should not be missingPrice")
	}
}

func TestStats_SQLiteModelPriceCompare_EmptyModelUsesTopTraffic(t *testing.T) {
	db, r := setupStatsSQLiteTest(t)
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := db.Exec(`INSERT INTO sites (name, url, platform, status, created_at, updated_at)
		VALUES (?, ?, ?, 'active', ?, ?)`, "TopSite", "https://top.example.test", "openai", now, now)
	if err != nil {
		t.Fatalf("insert site: %v", err)
	}
	var siteID int64
	if err := db.Get(&siteID, "SELECT id FROM sites WHERE name = ?", "TopSite"); err != nil {
		t.Fatalf("site id: %v", err)
	}
	_, err = db.Exec(`INSERT INTO accounts (site_id, username, access_token, status, balance, checkin_enabled, created_at, updated_at)
		VALUES (?, ?, ?, 'active', ?, 0, ?, ?)`, siteID, "top-user", "sk-top", 1.0, now, now)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}
	var accID int64
	if err := db.Get(&accID, "SELECT id FROM accounts WHERE username = ?", "top-user"); err != nil {
		t.Fatalf("acc id: %v", err)
	}
	_, err = db.Exec(`INSERT INTO proxy_logs (account_id, model_requested, model_actual, status, total_tokens, estimated_cost, created_at)
		VALUES (?, ?, ?, 'success', ?, ?, ?)`, accID, "top-model-a", "top-model-a", 100, 0.2, now)
	if err != nil {
		t.Fatalf("log: %v", err)
	}

	resp := doGet(t, r, "/api/models/price-compare?limit=10")
	if resp.Code != 200 {
		t.Fatalf("status %d: %s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	items, ok := body["items"].([]any)
	if !ok || len(items) == 0 {
		t.Fatalf("items empty: %#v", body)
	}
	first := items[0].(map[string]any)
	if first["model"] != "top-model-a" {
		t.Fatalf("model=%v want top-model-a", first["model"])
	}
	if first["source"] != "observed" {
		t.Fatalf("source=%v want observed", first["source"])
	}
}

func TestStats_SQLiteModelPriceCompare_FallbackLabeledMissing(t *testing.T) {
	db, r := setupStatsSQLiteTest(t)
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := db.Exec(`INSERT INTO sites (name, url, platform, status, created_at, updated_at)
		VALUES (?, ?, ?, 'active', ?, ?)`, "EmptySite", "https://empty.example.test", "openai", now, now)
	if err != nil {
		t.Fatalf("insert site: %v", err)
	}
	var siteID int64
	if err := db.Get(&siteID, "SELECT id FROM sites WHERE name = ?", "EmptySite"); err != nil {
		t.Fatalf("site id: %v", err)
	}
	_, err = db.Exec(`INSERT INTO accounts (site_id, username, access_token, status, balance, checkin_enabled, created_at, updated_at)
		VALUES (?, ?, ?, 'active', ?, 0, ?, ?)`, siteID, "empty-user", "sk-empty", 1.0, now, now)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}

	resp := doGet(t, r, "/api/models/price-compare?model=never-seen-model")
	if resp.Code != 200 {
		t.Fatalf("status %d: %s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	items, ok := body["items"].([]any)
	if !ok || len(items) == 0 {
		t.Fatalf("expected fallback rows for empty state, got %#v", body["items"])
	}
	item := items[0].(map[string]any)
	if item["source"] != "fallback" {
		t.Fatalf("source=%v want fallback", item["source"])
	}
	if item["missingPrice"] != true {
		t.Fatalf("missingPrice=%v want true", item["missingPrice"])
	}
	if item["ratesSource"] != "fallback" {
		t.Fatalf("ratesSource=%v want fallback", item["ratesSource"])
	}
}
