package service

import (
	"testing"

	"github.com/tokendancelab/metapi-go/store"
)

func TestCreateSite_WithAPIEndpointsSQLite(t *testing.T) {
	db, err := store.Open(store.DialectSQLite, ":memory:", false)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := store.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	id, err := CreateSite(db.DB, map[string]any{
		"name":         "CreateSite Endpoints",
		"url":          "https://create-site-endpoints.example.com",
		"platform":     "openai",
		"status":       "active",
		"isPinned":     false,
		"globalWeight": 1.0,
		"maxConcurrency": int64(0),
		"proxyUrl":     nil,
		"useSystemProxy": false,
		"customHeaders": nil,
		"externalCheckinUrl": nil,
		"postRefreshProbeEnabled": false,
		"postRefreshProbeModel": "",
		"postRefreshProbeScope": "single",
		"postRefreshProbeLatencyThresholdMs": 0,
		"apiEndpoints": []store.SiteAPIEndpoint{
			{URL: "https://create-site-endpoints.example.com/v1", Enabled: true, SortOrder: 0},
		},
	})
	if err != nil {
		t.Fatalf("CreateSite: %v", err)
	}
	if id <= 0 {
		t.Fatalf("id=%d", id)
	}

	eps, err := LoadSiteAPIEndpoints(db.DB, []int64{id})
	if err != nil {
		t.Fatalf("LoadSiteAPIEndpoints: %v", err)
	}
	list := eps[id]
	if len(list) != 1 {
		t.Fatalf("endpoints=%d want 1", len(list))
	}
	if list[0].SiteID != id {
		t.Fatalf("endpoint site_id=%d want %d", list[0].SiteID, id)
	}
}
