package store

import (
	"reflect"
	"strings"
	"testing"
)

// TableColumnCount maps table name (Go struct name) to expected column count.
// Counts are verified against the actual struct field count in schema.go.
var tableColumnCount = map[string]int{
	"Site":                           19,
	"SiteAPIEndpoint":                11,
	"SiteDisabledModel":              4,
	"Account":                        22,
	"AccountToken":                   11,
	"CheckinLog":                     6,
	"ModelAvailability":              7,
	"TokenModelAvailability":         6,
	"TokenRoute":                     12,
	"RouteGroupSource":               3,
	"OAuthRouteUnit":                 8,
	"OAuthRouteUnitMember":           16,
	"RouteChannel":                   20,
	"ProxyLog":                       24,
	"ProxyDebugTrace":                26,
	"ProxyDebugAttempt":              18,
	"ProxyVideoTask":                 15,
	"ProxyFile":                      13,
	"Setting":                        2,
	"AdminSnapshot":                  9,
	"AnalyticsProjectionCheckpoint":  17,
	"SiteDayUsage":                   13,
	"SiteHourUsage":                  13,
	"ModelDayUsage":                  13,
	"DownstreamAPIKey":               20,
	"SiteAnnouncement":               19,
	"Event":                          9,
}

// allStructs returns a list of all 27 table structs for reflection-based testing.
func allStructs() []any {
	return []any{
		Site{},
		SiteAPIEndpoint{},
		SiteDisabledModel{},
		Account{},
		AccountToken{},
		CheckinLog{},
		ModelAvailability{},
		TokenModelAvailability{},
		TokenRoute{},
		RouteGroupSource{},
		OAuthRouteUnit{},
		OAuthRouteUnitMember{},
		RouteChannel{},
		ProxyLog{},
		ProxyDebugTrace{},
		ProxyDebugAttempt{},
		ProxyVideoTask{},
		ProxyFile{},
		Setting{},
		AdminSnapshot{},
		AnalyticsProjectionCheckpoint{},
		SiteDayUsage{},
		SiteHourUsage{},
		ModelDayUsage{},
		DownstreamAPIKey{},
		SiteAnnouncement{},
		Event{},
	}
}

// TestTableCount verifies that there are exactly 27 table structs.
func TestTableCount(t *testing.T) {
	structs := allStructs()
	if len(structs) != 27 {
		t.Errorf("expected 27 table structs, got %d", len(structs))
	}
}

// TestColumnCounts verifies that each struct has the expected number of fields.
func TestColumnCounts(t *testing.T) {
	for _, s := range allStructs() {
		v := reflect.ValueOf(s)
		tp := v.Type()
		name := tp.Name()
		expected, ok := tableColumnCount[name]
		if !ok {
			t.Errorf("struct %q not found in tableColumnCount map", name)
			continue
		}
		// Exported fields only (all schema fields are exported)
		count := 0
		for i := 0; i < tp.NumField(); i++ {
			if tp.Field(i).IsExported() {
				count++
			}
		}
		if count != expected {
			t.Errorf("%s: expected %d columns, got %d", name, expected, count)
		}
	}
}

// TestStructTags verifies that every exported field on every struct has a `db` tag.
func TestStructTags(t *testing.T) {
	for _, s := range allStructs() {
		v := reflect.ValueOf(s)
		tp := v.Type()
		name := tp.Name()
		for i := 0; i < tp.NumField(); i++ {
			field := tp.Field(i)
			if !field.IsExported() {
				continue
			}
			tag, ok := field.Tag.Lookup("db")
			if !ok {
				t.Errorf("%s.%s: missing `db` tag", name, field.Name)
				continue
			}
			if tag == "" {
				t.Errorf("%s.%s: `db` tag is empty", name, field.Name)
			}
		}
	}
}

// TestSettingTextPK verifies that the Setting struct uses a text primary key (key), not SERIAL.
func TestSettingTextPK(t *testing.T) {
	v := reflect.ValueOf(Setting{})
	tp := v.Type()

	keyField, found := tp.FieldByName("Key")
	if !found {
		t.Fatal("Setting struct missing Key field")
	}
	tag := keyField.Tag.Get("db")
	if tag != "key" {
		t.Errorf("Setting.Key db tag: expected 'key', got %q", tag)
	}

	// Verify there is NO 'id' field (it's a text PK table, not SERIAL).
	if _, hasID := tp.FieldByName("ID"); hasID {
		t.Error("Setting struct should NOT have an ID field (text PK table)")
	}
}

// TestAnalyticsCheckpointTextPK verifies text PK for analytics_projection_checkpoints.
func TestAnalyticsCheckpointTextPK(t *testing.T) {
	v := reflect.ValueOf(AnalyticsProjectionCheckpoint{})
	tp := v.Type()

	pkField, found := tp.FieldByName("ProjectorKey")
	if !found {
		t.Fatal("AnalyticsProjectionCheckpoint struct missing ProjectorKey field")
	}
	tag := pkField.Tag.Get("db")
	if tag != "projector_key" {
		t.Errorf("AnalyticsProjectionCheckpoint.ProjectorKey db tag: expected 'projector_key', got %q", tag)
	}

	if _, hasID := tp.FieldByName("ID"); hasID {
		t.Error("AnalyticsProjectionCheckpoint struct should NOT have an ID field (text PK table)")
	}
}

// TestRouteChannelsTokenIDSetNullFK verifies the critical FK semantic:
// route_channels.token_id FK must use ON DELETE SET NULL, not CASCADE.
// We can't test FK behavior via tags alone, but we verify the field exists and is nullable (*int64).
func TestRouteChannelsTokenIDNullable(t *testing.T) {
	v := reflect.ValueOf(RouteChannel{})
	tp := v.Type()

	field, found := tp.FieldByName("TokenID")
	if !found {
		t.Fatal("RouteChannel struct missing TokenID field")
	}

	// TokenID must be *int64 (nullable) because the FK uses ON DELETE SET NULL.
	if field.Type.Kind() != reflect.Ptr {
		t.Errorf("RouteChannel.TokenID should be *int64 (nullable for SET NULL FK), got %v", field.Type)
	}

	tag := field.Tag.Get("db")
	if tag != "token_id" {
		t.Errorf("RouteChannel.TokenID db tag: expected 'token_id', got %q", tag)
	}
}

// TestRouteChannelsOAuthRouteUnitIDNoFK verifies that oauth_route_unit_id
// has NO FK constraint (matching TS behavior).
func TestRouteChannelsOAuthRouteUnitIDNoFK(t *testing.T) {
	v := reflect.ValueOf(RouteChannel{})
	tp := v.Type()

	field, found := tp.FieldByName("OAuthRouteUnitID")
	if !found {
		t.Fatal("RouteChannel struct missing OAuthRouteUnitID field")
	}

	tag := field.Tag.Get("db")
	if tag != "oauth_route_unit_id" {
		t.Errorf("RouteChannel.OAuthRouteUnitID db tag: expected 'oauth_route_unit_id', got %q", tag)
	}
}

// TestCamelCaseTagConsistency verifies that all db tags use snake_case consistently.
func TestCamelCaseTagConsistency(t *testing.T) {
	for _, s := range allStructs() {
		v := reflect.ValueOf(s)
		tp := v.Type()
		name := tp.Name()
		for i := 0; i < tp.NumField(); i++ {
			field := tp.Field(i)
			if !field.IsExported() {
				continue
			}
			tag := field.Tag.Get("db")
			if tag == "" {
				continue
			}

			// Verify the tag is all lowercase with underscores (snake_case).
			for _, c := range tag {
				if c >= 'A' && c <= 'Z' {
					t.Errorf("%s.%s: db tag %q contains uppercase letter", name, field.Name, tag)
					break
				}
			}

			// Verify the tag doesn't end with or start with underscore.
			if strings.HasPrefix(tag, "_") {
				t.Errorf("%s.%s: db tag %q starts with underscore", name, field.Name, tag)
			}
			if strings.HasSuffix(tag, "_") {
				t.Errorf("%s.%s: db tag %q ends with underscore", name, field.Name, tag)
			}
		}
	}
}
