package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestSites_CreateWithAPIEndpoints_SQLite(t *testing.T) {
	_, r := setupSitesTest(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	siteURL := "https://sqlite-sites-ep-" + suffix + ".example.com"
	endpointURL := siteURL + "/v1"

	resp := doPostJSON(t, r, "/api/sites", map[string]any{
		"name":     "SQLite Sites EP " + suffix,
		"url":      siteURL,
		"platform": "openai",
		"apiEndpoints": []map[string]any{
			{"url": endpointURL, "enabled": true, "sortOrder": 0},
		},
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("create site with endpoints: %d %s", resp.Code, resp.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &created); err != nil {
		t.Fatalf("json: %v", err)
	}
	id, ok := created["id"].(float64)
	if !ok || id <= 0 {
		t.Fatalf("id=%v", created["id"])
	}
	eps, _ := created["apiEndpoints"].([]any)
	if len(eps) != 1 {
		t.Fatalf("apiEndpoints=%v", created["apiEndpoints"])
	}
}
