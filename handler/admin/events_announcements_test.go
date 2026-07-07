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

func setupEventsAnnouncementsTest(t *testing.T) (*store.DB, chi.Router) {
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
	RegisterEventsRoutes(r, db.DB)
	RegisterSiteAnnouncementsRoutes(r, db.DB)
	return db, r
}

func setupEventsAnnouncementsPostgresTest(t *testing.T) (*store.DB, chi.Router) {
	t.Helper()
	db, r := setupSitesPostgresTest(t)
	RegisterEventsRoutes(r, db.DB)
	RegisterSiteAnnouncementsRoutes(r, db.DB)
	return db, r
}

func seedEvent(t *testing.T, db *store.DB, suffix string) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	var id int64
	if db.Dialect == store.DialectPostgres {
		err := db.QueryRowx(
			`INSERT INTO events (type, title, message, level, read, related_id, related_type, created_at)
			 VALUES (?, ?, ?, 'info', ?, 1, 'test', ?) RETURNING id`,
			"audit-"+suffix, "Event "+suffix, "message", false, now,
		).Scan(&id)
		if err != nil {
			t.Fatalf("insert postgres event: %v", err)
		}
		return id
	}

	res, err := db.Exec(
		`INSERT INTO events (type, title, message, level, read, related_id, related_type, created_at)
		 VALUES (?, ?, ?, 'info', ?, 1, 'test', ?)`,
		"audit-"+suffix, "Event "+suffix, "message", false, now,
	)
	if err != nil {
		t.Fatalf("insert sqlite event: %v", err)
	}
	id, _ = res.LastInsertId()
	return id
}

func seedSiteAnnouncement(t *testing.T, db *store.DB, suffix string) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	siteID := seedSiteForAnnouncement(t, db, suffix)
	var id int64
	if db.Dialect == store.DialectPostgres {
		err := db.QueryRowx(
			`INSERT INTO site_announcements (site_id, platform, source_key, title, content, level, first_seen_at, last_seen_at)
			 VALUES (?, 'openai', ?, ?, 'content', 'info', ?, ?) RETURNING id`,
			siteID, "announcement-"+suffix, "Announcement "+suffix, now, now,
		).Scan(&id)
		if err != nil {
			t.Fatalf("insert postgres announcement: %v", err)
		}
		return id
	}

	res, err := db.Exec(
		`INSERT INTO site_announcements (site_id, platform, source_key, title, content, level, first_seen_at, last_seen_at)
		 VALUES (?, 'openai', ?, ?, 'content', 'info', ?, ?)`,
		siteID, "announcement-"+suffix, "Announcement "+suffix, now, now,
	)
	if err != nil {
		t.Fatalf("insert sqlite announcement: %v", err)
	}
	id, _ = res.LastInsertId()
	return id
}

func seedSiteForAnnouncement(t *testing.T, db *store.DB, suffix string) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	url := "https://announcement-" + suffix + ".example.com"
	var id int64
	if db.Dialect == store.DialectPostgres {
		err := db.QueryRowx(
			`INSERT INTO sites (name, url, platform, status, created_at, updated_at)
			 VALUES (?, ?, 'openai', 'active', ?, ?) RETURNING id`,
			"Announcement Site "+suffix, url, now, now,
		).Scan(&id)
		if err != nil {
			t.Fatalf("insert postgres site: %v", err)
		}
		return id
	}

	res, err := db.Exec(
		`INSERT INTO sites (name, url, platform, status, created_at, updated_at)
		 VALUES (?, ?, 'openai', 'active', ?, ?)`,
		"Announcement Site "+suffix, url, now, now,
	)
	if err != nil {
		t.Fatalf("insert sqlite site: %v", err)
	}
	id, _ = res.LastInsertId()
	return id
}

func TestEventsAndAnnouncements_SQLiteLifecycle(t *testing.T) {
	db, r := setupEventsAnnouncementsTest(t)
	runEventsAndAnnouncementsLifecycle(t, db, r, "sqlite-"+strconv.FormatInt(time.Now().UnixNano(), 36))
}

func TestEventsAndAnnouncements_PostgresLifecycle(t *testing.T) {
	db, r := setupEventsAnnouncementsPostgresTest(t)
	suffix := "pg-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	t.Cleanup(func() {
		_, _ = db.Exec("DELETE FROM sites WHERE url = ?", "https://announcement-"+suffix+".example.com")
	})

	runEventsAndAnnouncementsLifecycle(t, db, r, suffix)
}

func runEventsAndAnnouncementsLifecycle(t *testing.T, db *store.DB, r chi.Router, suffix string) {
	t.Helper()
	eventID := seedEvent(t, db, suffix)
	announcementID := seedSiteAnnouncement(t, db, suffix)

	eventsResp := doGet(t, r, "/api/events?type=audit-"+suffix+"&read=false")
	if eventsResp.Code != http.StatusOK {
		t.Fatalf("list events returned %d: %s", eventsResp.Code, eventsResp.Body.String())
	}
	var events []map[string]any
	if err := json.Unmarshal(eventsResp.Body.Bytes(), &events); err != nil {
		t.Fatalf("unmarshal events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events count = %d, want 1; body=%s", len(events), eventsResp.Body.String())
	}

	countResp := doGet(t, r, "/api/events/count")
	if countResp.Code != http.StatusOK {
		t.Fatalf("event count returned %d: %s", countResp.Code, countResp.Body.String())
	}

	readResp := doPostJSON(t, r, "/api/events/"+itoa(eventID)+"/read", nil)
	if readResp.Code != http.StatusOK {
		t.Fatalf("mark event read returned %d: %s", readResp.Code, readResp.Body.String())
	}

	annResp := doGet(t, r, "/api/site-announcements?platform=openai&read=false&status=active")
	if annResp.Code != http.StatusOK {
		t.Fatalf("list announcements returned %d: %s", annResp.Code, annResp.Body.String())
	}
	var announcements []map[string]any
	if err := json.Unmarshal(annResp.Body.Bytes(), &announcements); err != nil {
		t.Fatalf("unmarshal announcements: %v", err)
	}
	if len(announcements) == 0 {
		t.Fatalf("announcements count = 0, want at least 1; body=%s", annResp.Body.String())
	}

	annReadResp := doPostJSON(t, r, "/api/site-announcements/"+itoa(announcementID)+"/read", nil)
	if annReadResp.Code != http.StatusOK {
		t.Fatalf("mark announcement read returned %d: %s", annReadResp.Code, annReadResp.Body.String())
	}

	readAllResp := doPostJSON(t, r, "/api/site-announcements/read-all", nil)
	if readAllResp.Code != http.StatusOK {
		t.Fatalf("mark all announcements read returned %d: %s", readAllResp.Code, readAllResp.Body.String())
	}

	deleteAnnouncementsResp := doDelete(t, r, "/api/site-announcements")
	if deleteAnnouncementsResp.Code != http.StatusOK {
		t.Fatalf("delete announcements returned %d: %s", deleteAnnouncementsResp.Code, deleteAnnouncementsResp.Body.String())
	}

	deleteEventsResp := doDelete(t, r, "/api/events")
	if deleteEventsResp.Code != http.StatusOK {
		t.Fatalf("delete events returned %d: %s", deleteEventsResp.Code, deleteEventsResp.Body.String())
	}
}
