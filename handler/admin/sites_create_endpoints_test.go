package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tokendancelab/metapi-go/handler/admin/payloads"
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

func TestNormalizeAPIEndpointsInput_ForbiddenMetadata(t *testing.T) {
	forbid := []string{
		"http://169.254.169.254/latest/meta-data",
		"https://169.254.169.254/",
		"http://metadata.google.internal/computeMetadata/v1/",
		"http://[fe80::1]/",
	}
	for _, u := range forbid {
		_, errMsg := normalizeAPIEndpointsInput([]payloads.SiteAPIEndpointInput{
			{URL: u, Enabled: true, SortOrder: 0},
		})
		if errMsg == "" {
			t.Fatalf("expected forbid for %q", u)
		}
		if !strings.Contains(errMsg, "Cloud metadata / link-local") {
			t.Fatalf("unexpected error for %q: %s", u, errMsg)
		}
	}
}

func TestNormalizeAPIEndpointsInput_AllowsRFC1918LocalhostPublic(t *testing.T) {
	allow := []string{
		"https://api.openai.com/v1",
		"http://10.0.0.5:8080/v1",
		"http://192.168.1.10/v1",
		"http://127.0.0.1:4000/v1",
		"http://localhost:8080/v1",
	}
	for _, u := range allow {
		eps, errMsg := normalizeAPIEndpointsInput([]payloads.SiteAPIEndpointInput{
			{URL: u, Enabled: true, SortOrder: 0},
		})
		if errMsg != "" {
			t.Fatalf("expected allow for %q, got %s", u, errMsg)
		}
		if len(eps) != 1 || eps[0].URL == "" {
			t.Fatalf("expected normalized endpoint for %q, got %+v", u, eps)
		}
	}
}

func TestSites_Create_APIEndpoints_RejectsForbiddenMetadata(t *testing.T) {
	_, r := setupSitesTest(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	cases := []string{
		"http://169.254.169.254/latest/meta-data",
		"http://metadata.google.internal/",
		"http://[fe80::1]/",
	}
	for i, epURL := range cases {
		resp := doPostJSON(t, r, "/api/sites", map[string]any{
			"name":     "Forbidden EP " + suffix + "-" + strconv.Itoa(i),
			"url":      "https://forbid-ep-" + suffix + "-" + strconv.Itoa(i) + ".example.com",
			"platform": "openai",
			"apiEndpoints": []map[string]any{
				{"url": epURL, "enabled": true, "sortOrder": 0},
			},
		})
		if resp.Code != http.StatusBadRequest {
			t.Fatalf("create forbidden endpoint %q: expected 400, got %d %s", epURL, resp.Code, resp.Body.String())
		}
		if !strings.Contains(resp.Body.String(), "Cloud metadata / link-local") {
			t.Fatalf("create forbidden endpoint %q: body=%s", epURL, resp.Body.String())
		}
	}
}

func TestSites_Create_APIEndpoints_AllowsRFC1918LocalhostPublic(t *testing.T) {
	_, r := setupSitesTest(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	allow := []string{
		"https://api.openai.com/v1",
		"http://10.0.0.5:8080/v1",
		"http://192.168.1.10/v1",
		"http://127.0.0.1:4000/v1",
		"http://localhost:8080/v1",
	}
	for i, epURL := range allow {
		resp := doPostJSON(t, r, "/api/sites", map[string]any{
			"name":     "Allow EP " + suffix + "-" + strconv.Itoa(i),
			"url":      "https://allow-ep-" + suffix + "-" + strconv.Itoa(i) + ".example.com",
			"platform": "openai",
			"apiEndpoints": []map[string]any{
				{"url": epURL, "enabled": true, "sortOrder": 0},
			},
		})
		if resp.Code != http.StatusOK {
			t.Fatalf("create allowed endpoint %q: expected 200, got %d %s", epURL, resp.Code, resp.Body.String())
		}
	}
}

func TestSites_Update_APIEndpoints_RejectsForbiddenMetadata(t *testing.T) {
	_, r := setupSitesTest(t)
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	createResp := doPostJSON(t, r, "/api/sites", map[string]any{
		"name":     "Update Forbid EP " + suffix,
		"url":      "https://update-forbid-ep-" + suffix + ".example.com",
		"platform": "openai",
	})
	if createResp.Code != http.StatusOK {
		t.Fatalf("create site: %d %s", createResp.Code, createResp.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatalf("json: %v", err)
	}
	siteID := int64(created["id"].(float64))

	resp := doPutJSON(t, r, "/api/sites/"+itoa(siteID), map[string]any{
		"apiEndpoints": []map[string]any{
			{"url": "http://169.254.169.254/", "enabled": true, "sortOrder": 0},
		},
	})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("update forbidden endpoint: expected 400, got %d %s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "Cloud metadata / link-local") {
		t.Fatalf("update forbidden endpoint body=%s", resp.Body.String())
	}
}
