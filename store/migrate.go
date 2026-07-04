package store

import (
	"fmt"
	"log/slog"

	"github.com/tokendancelab/metapi-go/config"
)

// AutoMigrate creates all 27 tables with indexes, unique constraints, foreign keys,
// and check constraints. Uses CREATE TABLE IF NOT EXISTS for idempotency.
// Run on startup after Open().
func AutoMigrate(db *DB) error {
	dialect := db.Dialect
	slog.Info("store: running auto-migration", "dialect", dialect)

	migrations := []struct {
		name string
		sql  string
	}{
		// Table 1: sites
		{"sites", buildSitesDDL(dialect)},
		// Table 2: site_api_endpoints
		{"site_api_endpoints", buildSiteAPIEndpointsDDL(dialect)},
		// Table 3: site_disabled_models
		{"site_disabled_models", buildSiteDisabledModelsDDL(dialect)},
		// Table 4: accounts
		{"accounts", buildAccountsDDL(dialect)},
		// Table 5: account_tokens
		{"account_tokens", buildAccountTokensDDL(dialect)},
		// Table 6: checkin_logs
		{"checkin_logs", buildCheckinLogsDDL(dialect)},
		// Table 7: model_availability
		{"model_availability", buildModelAvailabilityDDL(dialect)},
		// Table 8: token_model_availability
		{"token_model_availability", buildTokenModelAvailabilityDDL(dialect)},
		// Table 9: token_routes
		{"token_routes", buildTokenRoutesDDL(dialect)},
		// Table 10: route_group_sources
		{"route_group_sources", buildRouteGroupSourcesDDL(dialect)},
		// Table 11: oauth_route_units
		{"oauth_route_units", buildOAuthRouteUnitsDDL(dialect)},
		// Table 12: oauth_route_unit_members
		{"oauth_route_unit_members", buildOAuthRouteUnitMembersDDL(dialect)},
		// Table 13: route_channels
		{"route_channels", buildRouteChannelsDDL(dialect)},
		// Table 14: proxy_logs
		{"proxy_logs", buildProxyLogsDDL(dialect)},
		// Table 15: proxy_debug_traces
		{"proxy_debug_traces", buildProxyDebugTracesDDL(dialect)},
		// Table 16: proxy_debug_attempts
		{"proxy_debug_attempts", buildProxyDebugAttemptsDDL(dialect)},
		// Table 17: proxy_video_tasks
		{"proxy_video_tasks", buildProxyVideoTasksDDL(dialect)},
		// Table 18: proxy_files
		{"proxy_files", buildProxyFilesDDL(dialect)},
		// Table 19: settings (text PK)
		{"settings", buildSettingsDDL(dialect)},
		// Table 20: admin_snapshots
		{"admin_snapshots", buildAdminSnapshotsDDL(dialect)},
		// Table 21: analytics_projection_checkpoints (text PK)
		{"analytics_projection_checkpoints", buildAnalyticsProjectionCheckpointsDDL(dialect)},
		// Table 22: site_day_usage
		{"site_day_usage", buildSiteDayUsageDDL(dialect)},
		// Table 23: site_hour_usage
		{"site_hour_usage", buildSiteHourUsageDDL(dialect)},
		// Table 24: model_day_usage
		{"model_day_usage", buildModelDayUsageDDL(dialect)},
		// Table 25: downstream_api_keys
		{"downstream_api_keys", buildDownstreamAPIKeysDDL(dialect)},
		// Table 26: site_announcements
		{"site_announcements", buildSiteAnnouncementsDDL(dialect)},
		// Table 27: events
		{"events", buildEventsDDL(dialect)},
	}

	// Non-UNIQUE indexes are created separately via CREATE INDEX IF NOT EXISTS
	// for both SQLite and PostgreSQL. SQLite inline CREATE TABLE only supports
	// PRIMARY KEY and UNIQUE constraints, not plain indexes.
	migrations = append(migrations, buildIndexes()...)

	for _, m := range migrations {
		if _, err := db.Exec(m.sql); err != nil {
			return fmt.Errorf("store: migrate %s: %w", m.name, err)
		}
	}

	slog.Info("store: auto-migration complete", "dialect", dialect)
	return nil
}

// ---- DDL helper functions ----

// btype returns the boolean column type for a given dialect.
func btype(d string) string {
	if d == DialectPostgres {
		return "BOOLEAN"
	}
	return "INTEGER" // SQLite stores 0/1
}

// rtype returns the real/float column type for a given dialect.
func rtype(d string) string {
	if d == DialectPostgres {
		return "DOUBLE PRECISION"
	}
	return "REAL"
}

// serialPK returns the auto-increment PK column definition.
func serialPK(d string) string {
	if d == DialectPostgres {
		return "SERIAL PRIMARY KEY"
	}
	return "INTEGER PRIMARY KEY AUTOINCREMENT"
}

// textPK returns the text PK column definition (for settings, checkpoints).
func textPK(d string) string {
	return "TEXT PRIMARY KEY"
}

// isPostgres is a short helper.
func isPG(d string) bool { return d == DialectPostgres }

// pgInlineFKNullable creates nullable inline FK
func pgInlineFKNullable(col, refTable, refCol, onDelete string) string {
	return fmt.Sprintf("%s INTEGER REFERENCES %s(%s) ON DELETE %s", col, refTable, refCol, onDelete)
}

// sqFK builds a SQLite FK constraint (separate from column defs).
func sqFK(col, refTable, refCol, onDelete string) string {
	return fmt.Sprintf("FOREIGN KEY (%s) REFERENCES %s(%s) ON DELETE %s", col, refTable, refCol, onDelete)
}

// ---- Table DDL builders ----

func buildSitesDDL(d string) string {
	if isPG(d) {
		return `CREATE TABLE IF NOT EXISTS sites (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			url TEXT NOT NULL,
			external_checkin_url TEXT,
			platform TEXT NOT NULL,
			proxy_url TEXT,
			use_system_proxy BOOLEAN DEFAULT FALSE,
			custom_headers TEXT,
			status TEXT NOT NULL DEFAULT 'active',
			is_pinned BOOLEAN DEFAULT FALSE,
			sort_order INTEGER DEFAULT 0,
			global_weight DOUBLE PRECISION DEFAULT 1,
			api_key TEXT,
			post_refresh_probe_enabled BOOLEAN DEFAULT FALSE,
			post_refresh_probe_model TEXT DEFAULT '',
			post_refresh_probe_scope TEXT DEFAULT 'single',
			post_refresh_probe_latency_threshold_ms INTEGER DEFAULT 0,
			created_at TEXT,
			updated_at TEXT,
			CONSTRAINT sites_platform_url_unique UNIQUE (platform, url)
		)`
	}
	return `CREATE TABLE IF NOT EXISTS sites (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		url TEXT NOT NULL,
		external_checkin_url TEXT,
		platform TEXT NOT NULL,
		proxy_url TEXT,
		use_system_proxy INTEGER DEFAULT 0,
		custom_headers TEXT,
		status TEXT NOT NULL DEFAULT 'active',
		is_pinned INTEGER DEFAULT 0,
		sort_order INTEGER DEFAULT 0,
		global_weight REAL DEFAULT 1,
		api_key TEXT,
		post_refresh_probe_enabled INTEGER DEFAULT 0,
		post_refresh_probe_model TEXT DEFAULT '',
		post_refresh_probe_scope TEXT DEFAULT 'single',
		post_refresh_probe_latency_threshold_ms INTEGER DEFAULT 0,
		created_at TEXT,
		updated_at TEXT,
		CONSTRAINT sites_platform_url_unique UNIQUE (platform, url)
	)`
}

func buildSiteAPIEndpointsDDL(d string) string {
	if isPG(d) {
		return `CREATE TABLE IF NOT EXISTS site_api_endpoints (
			id SERIAL PRIMARY KEY,
			site_id INTEGER NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
			url TEXT NOT NULL,
			enabled BOOLEAN DEFAULT TRUE,
			sort_order INTEGER DEFAULT 0,
			cooldown_until TEXT,
			last_selected_at TEXT,
			last_failed_at TEXT,
			last_failure_reason TEXT,
			created_at TEXT,
			updated_at TEXT,
			CONSTRAINT site_api_endpoints_site_url_unique UNIQUE (site_id, url)
		)`
	}
	return `CREATE TABLE IF NOT EXISTS site_api_endpoints (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		site_id INTEGER NOT NULL,
		url TEXT NOT NULL,
		enabled INTEGER DEFAULT 1,
		sort_order INTEGER DEFAULT 0,
		cooldown_until TEXT,
		last_selected_at TEXT,
		last_failed_at TEXT,
		last_failure_reason TEXT,
		created_at TEXT,
		updated_at TEXT,
		CONSTRAINT site_api_endpoints_site_url_unique UNIQUE (site_id, url),
		FOREIGN KEY (site_id) REFERENCES sites(id) ON DELETE CASCADE
	)`
}

func buildSiteDisabledModelsDDL(d string) string {
	if isPG(d) {
		return `CREATE TABLE IF NOT EXISTS site_disabled_models (
			id SERIAL PRIMARY KEY,
			site_id INTEGER NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
			model_name TEXT NOT NULL,
			created_at TEXT,
			CONSTRAINT site_disabled_models_site_model_unique UNIQUE (site_id, model_name)
		)`
	}
	return `CREATE TABLE IF NOT EXISTS site_disabled_models (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		site_id INTEGER NOT NULL,
		model_name TEXT NOT NULL,
		created_at TEXT,
		CONSTRAINT site_disabled_models_site_model_unique UNIQUE (site_id, model_name),
		FOREIGN KEY (site_id) REFERENCES sites(id) ON DELETE CASCADE
	)`
}

func buildAccountsDDL(d string) string {
	if isPG(d) {
		return `CREATE TABLE IF NOT EXISTS accounts (
			id SERIAL PRIMARY KEY,
			site_id INTEGER NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
			username TEXT,
			access_token TEXT NOT NULL,
			api_token TEXT,
			balance DOUBLE PRECISION DEFAULT 0,
			balance_used DOUBLE PRECISION DEFAULT 0,
			quota DOUBLE PRECISION DEFAULT 0,
			unit_cost DOUBLE PRECISION,
			value_score DOUBLE PRECISION DEFAULT 0,
			status TEXT DEFAULT 'active',
			is_pinned BOOLEAN DEFAULT FALSE,
			sort_order INTEGER DEFAULT 0,
			checkin_enabled BOOLEAN DEFAULT TRUE,
			last_checkin_at TEXT,
			last_balance_refresh TEXT,
			oauth_provider TEXT,
			oauth_account_key TEXT,
			oauth_project_id TEXT,
			extra_config TEXT,
			created_at TEXT,
			updated_at TEXT
		)`
	}
	return `CREATE TABLE IF NOT EXISTS accounts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		site_id INTEGER NOT NULL,
		username TEXT,
		access_token TEXT NOT NULL,
		api_token TEXT,
		balance REAL DEFAULT 0,
		balance_used REAL DEFAULT 0,
		quota REAL DEFAULT 0,
		unit_cost REAL,
		value_score REAL DEFAULT 0,
		status TEXT DEFAULT 'active',
		is_pinned INTEGER DEFAULT 0,
		sort_order INTEGER DEFAULT 0,
		checkin_enabled INTEGER DEFAULT 1,
		last_checkin_at TEXT,
		last_balance_refresh TEXT,
		oauth_provider TEXT,
		oauth_account_key TEXT,
		oauth_project_id TEXT,
		extra_config TEXT,
		created_at TEXT,
		updated_at TEXT,
		FOREIGN KEY (site_id) REFERENCES sites(id) ON DELETE CASCADE
	)`
}

func buildAccountTokensDDL(d string) string {
	if isPG(d) {
		return `CREATE TABLE IF NOT EXISTS account_tokens (
			id SERIAL PRIMARY KEY,
			account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			token TEXT NOT NULL,
			token_group TEXT,
			value_status TEXT NOT NULL DEFAULT 'ready',
			source TEXT DEFAULT 'manual',
			enabled BOOLEAN DEFAULT TRUE,
			is_default BOOLEAN DEFAULT FALSE,
			created_at TEXT,
			updated_at TEXT
		)`
	}
	return `CREATE TABLE IF NOT EXISTS account_tokens (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		account_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		token TEXT NOT NULL,
		token_group TEXT,
		value_status TEXT NOT NULL DEFAULT 'ready',
		source TEXT DEFAULT 'manual',
		enabled INTEGER DEFAULT 1,
		is_default INTEGER DEFAULT 0,
		created_at TEXT,
		updated_at TEXT,
		FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE
	)`
}

func buildCheckinLogsDDL(d string) string {
	if isPG(d) {
		return `CREATE TABLE IF NOT EXISTS checkin_logs (
			id SERIAL PRIMARY KEY,
			account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			status TEXT NOT NULL,
			message TEXT,
			reward TEXT,
			created_at TEXT
		)`
	}
	return `CREATE TABLE IF NOT EXISTS checkin_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		account_id INTEGER NOT NULL,
		status TEXT NOT NULL,
		message TEXT,
		reward TEXT,
		created_at TEXT,
		FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE
	)`
}

func buildModelAvailabilityDDL(d string) string {
	if isPG(d) {
		return `CREATE TABLE IF NOT EXISTS model_availability (
			id SERIAL PRIMARY KEY,
			account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			model_name TEXT NOT NULL,
			available BOOLEAN,
			is_manual BOOLEAN DEFAULT FALSE,
			latency_ms INTEGER,
			checked_at TEXT,
			CONSTRAINT model_availability_account_model_unique UNIQUE (account_id, model_name)
		)`
	}
	return `CREATE TABLE IF NOT EXISTS model_availability (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		account_id INTEGER NOT NULL,
		model_name TEXT NOT NULL,
		available INTEGER,
		is_manual INTEGER DEFAULT 0,
		latency_ms INTEGER,
		checked_at TEXT,
		CONSTRAINT model_availability_account_model_unique UNIQUE (account_id, model_name),
		FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE
	)`
}

func buildTokenModelAvailabilityDDL(d string) string {
	if isPG(d) {
		return `CREATE TABLE IF NOT EXISTS token_model_availability (
			id SERIAL PRIMARY KEY,
			token_id INTEGER NOT NULL REFERENCES account_tokens(id) ON DELETE CASCADE,
			model_name TEXT NOT NULL,
			available BOOLEAN,
			latency_ms INTEGER,
			checked_at TEXT,
			CONSTRAINT token_model_availability_token_model_unique UNIQUE (token_id, model_name)
		)`
	}
	return `CREATE TABLE IF NOT EXISTS token_model_availability (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		token_id INTEGER NOT NULL,
		model_name TEXT NOT NULL,
		available INTEGER,
		latency_ms INTEGER,
		checked_at TEXT,
		CONSTRAINT token_model_availability_token_model_unique UNIQUE (token_id, model_name),
		FOREIGN KEY (token_id) REFERENCES account_tokens(id) ON DELETE CASCADE
	)`
}

func buildTokenRoutesDDL(d string) string {
	if isPG(d) {
		return `CREATE TABLE IF NOT EXISTS token_routes (
			id SERIAL PRIMARY KEY,
			model_pattern TEXT NOT NULL,
			display_name TEXT,
			display_icon TEXT,
			route_mode TEXT DEFAULT 'pattern',
			model_mapping TEXT,
			decision_snapshot TEXT,
			decision_refreshed_at TEXT,
			routing_strategy TEXT DEFAULT 'weighted',
			enabled BOOLEAN DEFAULT TRUE,
			created_at TEXT,
			updated_at TEXT
		)`
	}
	return `CREATE TABLE IF NOT EXISTS token_routes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		model_pattern TEXT NOT NULL,
		display_name TEXT,
		display_icon TEXT,
		route_mode TEXT DEFAULT 'pattern',
		model_mapping TEXT,
		decision_snapshot TEXT,
		decision_refreshed_at TEXT,
		routing_strategy TEXT DEFAULT 'weighted',
		enabled INTEGER DEFAULT 1,
		created_at TEXT,
		updated_at TEXT
	)`
}

func buildRouteGroupSourcesDDL(d string) string {
	if isPG(d) {
		return `CREATE TABLE IF NOT EXISTS route_group_sources (
			id SERIAL PRIMARY KEY,
			group_route_id INTEGER NOT NULL REFERENCES token_routes(id) ON DELETE CASCADE,
			source_route_id INTEGER NOT NULL REFERENCES token_routes(id) ON DELETE CASCADE,
			CONSTRAINT route_group_sources_group_source_unique UNIQUE (group_route_id, source_route_id)
		)`
	}
	return `CREATE TABLE IF NOT EXISTS route_group_sources (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		group_route_id INTEGER NOT NULL,
		source_route_id INTEGER NOT NULL,
		CONSTRAINT route_group_sources_group_source_unique UNIQUE (group_route_id, source_route_id),
		FOREIGN KEY (group_route_id) REFERENCES token_routes(id) ON DELETE CASCADE,
		FOREIGN KEY (source_route_id) REFERENCES token_routes(id) ON DELETE CASCADE
	)`
}

func buildOAuthRouteUnitsDDL(d string) string {
	if isPG(d) {
		return `CREATE TABLE IF NOT EXISTS oauth_route_units (
			id SERIAL PRIMARY KEY,
			site_id INTEGER NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
			provider TEXT NOT NULL,
			name TEXT NOT NULL,
			strategy TEXT NOT NULL DEFAULT 'round_robin',
			enabled BOOLEAN DEFAULT TRUE,
			created_at TEXT,
			updated_at TEXT
		)`
	}
	return `CREATE TABLE IF NOT EXISTS oauth_route_units (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		site_id INTEGER NOT NULL,
		provider TEXT NOT NULL,
		name TEXT NOT NULL,
		strategy TEXT NOT NULL DEFAULT 'round_robin',
		enabled INTEGER DEFAULT 1,
		created_at TEXT,
		updated_at TEXT,
		FOREIGN KEY (site_id) REFERENCES sites(id) ON DELETE CASCADE
	)`
}

func buildOAuthRouteUnitMembersDDL(d string) string {
	if isPG(d) {
		return `CREATE TABLE IF NOT EXISTS oauth_route_unit_members (
			id SERIAL PRIMARY KEY,
			unit_id INTEGER NOT NULL REFERENCES oauth_route_units(id) ON DELETE CASCADE,
			account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			sort_order INTEGER DEFAULT 0,
			success_count INTEGER DEFAULT 0,
			fail_count INTEGER DEFAULT 0,
			total_latency_ms INTEGER DEFAULT 0,
			total_cost DOUBLE PRECISION DEFAULT 0,
			last_used_at TEXT,
			last_selected_at TEXT,
			last_fail_at TEXT,
			consecutive_fail_count INTEGER NOT NULL DEFAULT 0,
			cooldown_level INTEGER NOT NULL DEFAULT 0,
			cooldown_until TEXT,
			created_at TEXT,
			updated_at TEXT,
			CONSTRAINT oauth_route_unit_members_unit_account_unique UNIQUE (unit_id, account_id),
			CONSTRAINT oauth_route_unit_members_account_unique UNIQUE (account_id)
		)`
	}
	return `CREATE TABLE IF NOT EXISTS oauth_route_unit_members (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		unit_id INTEGER NOT NULL,
		account_id INTEGER NOT NULL,
		sort_order INTEGER DEFAULT 0,
		success_count INTEGER DEFAULT 0,
		fail_count INTEGER DEFAULT 0,
		total_latency_ms INTEGER DEFAULT 0,
		total_cost REAL DEFAULT 0,
		last_used_at TEXT,
		last_selected_at TEXT,
		last_fail_at TEXT,
		consecutive_fail_count INTEGER NOT NULL DEFAULT 0,
		cooldown_level INTEGER NOT NULL DEFAULT 0,
		cooldown_until TEXT,
		created_at TEXT,
		updated_at TEXT,
		CONSTRAINT oauth_route_unit_members_unit_account_unique UNIQUE (unit_id, account_id),
		CONSTRAINT oauth_route_unit_members_account_unique UNIQUE (account_id),
		FOREIGN KEY (unit_id) REFERENCES oauth_route_units(id) ON DELETE CASCADE,
		FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE
	)`
}

func buildRouteChannelsDDL(d string) string {
	// CRITICAL: token_id FK uses ON DELETE SET NULL (not CASCADE).
	// oauth_route_unit_id has NO FK constraint.
	if isPG(d) {
		return `CREATE TABLE IF NOT EXISTS route_channels (
			id SERIAL PRIMARY KEY,
			route_id INTEGER NOT NULL REFERENCES token_routes(id) ON DELETE CASCADE,
			account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
			token_id INTEGER REFERENCES account_tokens(id) ON DELETE SET NULL,
			oauth_route_unit_id INTEGER,
			source_model TEXT,
			priority INTEGER DEFAULT 0,
			weight INTEGER DEFAULT 10,
			enabled BOOLEAN DEFAULT TRUE,
			manual_override BOOLEAN DEFAULT FALSE,
			success_count INTEGER DEFAULT 0,
			fail_count INTEGER DEFAULT 0,
			total_latency_ms INTEGER DEFAULT 0,
			total_cost DOUBLE PRECISION DEFAULT 0,
			last_used_at TEXT,
			last_selected_at TEXT,
			last_fail_at TEXT,
			consecutive_fail_count INTEGER NOT NULL DEFAULT 0,
			cooldown_level INTEGER NOT NULL DEFAULT 0,
			cooldown_until TEXT
		)`
	}
	return `CREATE TABLE IF NOT EXISTS route_channels (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		route_id INTEGER NOT NULL,
		account_id INTEGER NOT NULL,
		token_id INTEGER,
		oauth_route_unit_id INTEGER,
		source_model TEXT,
		priority INTEGER DEFAULT 0,
		weight INTEGER DEFAULT 10,
		enabled INTEGER DEFAULT 1,
		manual_override INTEGER DEFAULT 0,
		success_count INTEGER DEFAULT 0,
		fail_count INTEGER DEFAULT 0,
		total_latency_ms INTEGER DEFAULT 0,
		total_cost REAL DEFAULT 0,
		last_used_at TEXT,
		last_selected_at TEXT,
		last_fail_at TEXT,
		consecutive_fail_count INTEGER NOT NULL DEFAULT 0,
		cooldown_level INTEGER NOT NULL DEFAULT 0,
		cooldown_until TEXT,
		FOREIGN KEY (route_id) REFERENCES token_routes(id) ON DELETE CASCADE,
		FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE,
		FOREIGN KEY (token_id) REFERENCES account_tokens(id) ON DELETE SET NULL
	)`
}

func buildProxyLogsDDL(d string) string {
	if isPG(d) {
		return `CREATE TABLE IF NOT EXISTS proxy_logs (
			id SERIAL PRIMARY KEY,
			route_id INTEGER,
			channel_id INTEGER,
			account_id INTEGER,
			downstream_api_key_id INTEGER,
			model_requested TEXT,
			model_actual TEXT,
			status TEXT,
			http_status INTEGER,
			is_stream BOOLEAN,
			first_byte_latency_ms INTEGER,
			latency_ms INTEGER,
			prompt_tokens INTEGER,
			completion_tokens INTEGER,
			total_tokens INTEGER,
			estimated_cost DOUBLE PRECISION,
			billing_details TEXT,
			client_family TEXT,
			client_app_id TEXT,
			client_app_name TEXT,
			client_confidence TEXT,
			error_message TEXT,
			retry_count INTEGER DEFAULT 0,
			created_at TEXT
		)`
	}
	return `CREATE TABLE IF NOT EXISTS proxy_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		route_id INTEGER,
		channel_id INTEGER,
		account_id INTEGER,
		downstream_api_key_id INTEGER,
		model_requested TEXT,
		model_actual TEXT,
		status TEXT,
		http_status INTEGER,
		is_stream INTEGER,
		first_byte_latency_ms INTEGER,
		latency_ms INTEGER,
		prompt_tokens INTEGER,
		completion_tokens INTEGER,
		total_tokens INTEGER,
		estimated_cost REAL,
		billing_details TEXT,
		client_family TEXT,
		client_app_id TEXT,
		client_app_name TEXT,
		client_confidence TEXT,
		error_message TEXT,
		retry_count INTEGER DEFAULT 0,
		created_at TEXT
	)`
}

func buildProxyDebugTracesDDL(d string) string {
	if isPG(d) {
		return `CREATE TABLE IF NOT EXISTS proxy_debug_traces (
			id SERIAL PRIMARY KEY,
			downstream_path TEXT NOT NULL,
			client_kind TEXT,
			session_id TEXT,
			trace_hint TEXT,
			requested_model TEXT,
			downstream_api_key_id INTEGER,
			request_headers_json TEXT,
			request_body_json TEXT,
			sticky_session_key TEXT,
			sticky_hit_channel_id INTEGER,
			selected_channel_id INTEGER,
			selected_route_id INTEGER,
			selected_account_id INTEGER,
			selected_site_id INTEGER,
			selected_site_platform TEXT,
			endpoint_candidates_json TEXT,
			endpoint_runtime_state_json TEXT,
			decision_summary_json TEXT,
			final_status TEXT,
			final_http_status INTEGER,
			final_upstream_path TEXT,
			final_response_headers_json TEXT,
			final_response_body_json TEXT,
			created_at TEXT,
			updated_at TEXT
		)`
	}
	return `CREATE TABLE IF NOT EXISTS proxy_debug_traces (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		downstream_path TEXT NOT NULL,
		client_kind TEXT,
		session_id TEXT,
		trace_hint TEXT,
		requested_model TEXT,
		downstream_api_key_id INTEGER,
		request_headers_json TEXT,
		request_body_json TEXT,
		sticky_session_key TEXT,
		sticky_hit_channel_id INTEGER,
		selected_channel_id INTEGER,
		selected_route_id INTEGER,
		selected_account_id INTEGER,
		selected_site_id INTEGER,
		selected_site_platform TEXT,
		endpoint_candidates_json TEXT,
		endpoint_runtime_state_json TEXT,
		decision_summary_json TEXT,
		final_status TEXT,
		final_http_status INTEGER,
		final_upstream_path TEXT,
		final_response_headers_json TEXT,
		final_response_body_json TEXT,
		created_at TEXT,
		updated_at TEXT
	)`
}

func buildProxyDebugAttemptsDDL(d string) string {
	if isPG(d) {
		return `CREATE TABLE IF NOT EXISTS proxy_debug_attempts (
			id SERIAL PRIMARY KEY,
			trace_id INTEGER NOT NULL REFERENCES proxy_debug_traces(id) ON DELETE CASCADE,
			attempt_index INTEGER NOT NULL,
			endpoint TEXT NOT NULL,
			request_path TEXT NOT NULL,
			target_url TEXT NOT NULL,
			runtime_executor TEXT,
			request_headers_json TEXT,
			request_body_json TEXT,
			response_status INTEGER,
			response_headers_json TEXT,
			response_body_json TEXT,
			raw_error_text TEXT,
			recover_applied BOOLEAN DEFAULT FALSE,
			downgrade_decision BOOLEAN DEFAULT FALSE,
			downgrade_reason TEXT,
			memory_write_json TEXT,
			created_at TEXT,
			CONSTRAINT proxy_debug_attempts_trace_attempt_unique UNIQUE (trace_id, attempt_index)
		)`
	}
	return `CREATE TABLE IF NOT EXISTS proxy_debug_attempts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		trace_id INTEGER NOT NULL,
		attempt_index INTEGER NOT NULL,
		endpoint TEXT NOT NULL,
		request_path TEXT NOT NULL,
		target_url TEXT NOT NULL,
		runtime_executor TEXT,
		request_headers_json TEXT,
		request_body_json TEXT,
		response_status INTEGER,
		response_headers_json TEXT,
		response_body_json TEXT,
		raw_error_text TEXT,
		recover_applied INTEGER DEFAULT 0,
		downgrade_decision INTEGER DEFAULT 0,
		downgrade_reason TEXT,
		memory_write_json TEXT,
		created_at TEXT,
		CONSTRAINT proxy_debug_attempts_trace_attempt_unique UNIQUE (trace_id, attempt_index),
		FOREIGN KEY (trace_id) REFERENCES proxy_debug_traces(id) ON DELETE CASCADE
	)`
}

func buildProxyVideoTasksDDL(d string) string {
	if isPG(d) {
		return `CREATE TABLE IF NOT EXISTS proxy_video_tasks (
			id SERIAL PRIMARY KEY,
			public_id TEXT NOT NULL,
			upstream_video_id TEXT NOT NULL,
			site_url TEXT NOT NULL,
			token_value TEXT NOT NULL,
			requested_model TEXT,
			actual_model TEXT,
			channel_id INTEGER,
			account_id INTEGER,
			status_snapshot TEXT,
			upstream_response_meta TEXT,
			last_upstream_status INTEGER,
			last_polled_at TEXT,
			created_at TEXT,
			updated_at TEXT,
			CONSTRAINT proxy_video_tasks_public_id_unique UNIQUE (public_id)
		)`
	}
	return `CREATE TABLE IF NOT EXISTS proxy_video_tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		public_id TEXT NOT NULL,
		upstream_video_id TEXT NOT NULL,
		site_url TEXT NOT NULL,
		token_value TEXT NOT NULL,
		requested_model TEXT,
		actual_model TEXT,
		channel_id INTEGER,
		account_id INTEGER,
		status_snapshot TEXT,
		upstream_response_meta TEXT,
		last_upstream_status INTEGER,
		last_polled_at TEXT,
		created_at TEXT,
		updated_at TEXT,
		CONSTRAINT proxy_video_tasks_public_id_unique UNIQUE (public_id)
	)`
}

func buildProxyFilesDDL(d string) string {
	if isPG(d) {
		return `CREATE TABLE IF NOT EXISTS proxy_files (
			id SERIAL PRIMARY KEY,
			public_id TEXT NOT NULL,
			owner_type TEXT NOT NULL,
			owner_id TEXT NOT NULL,
			filename TEXT NOT NULL,
			mime_type TEXT NOT NULL,
			purpose TEXT,
			byte_size INTEGER NOT NULL,
			sha256 TEXT NOT NULL,
			content_base64 TEXT NOT NULL,
			created_at TEXT,
			updated_at TEXT,
			deleted_at TEXT,
			CONSTRAINT proxy_files_public_id_unique UNIQUE (public_id)
		)`
	}
	return `CREATE TABLE IF NOT EXISTS proxy_files (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		public_id TEXT NOT NULL,
		owner_type TEXT NOT NULL,
		owner_id TEXT NOT NULL,
		filename TEXT NOT NULL,
		mime_type TEXT NOT NULL,
		purpose TEXT,
		byte_size INTEGER NOT NULL,
		sha256 TEXT NOT NULL,
		content_base64 TEXT NOT NULL,
		created_at TEXT,
		updated_at TEXT,
		deleted_at TEXT,
		CONSTRAINT proxy_files_public_id_unique UNIQUE (public_id)
	)`
}

func buildSettingsDDL(d string) string {
	return `CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT
	)`
}

func buildAdminSnapshotsDDL(d string) string {
	if isPG(d) {
		return `CREATE TABLE IF NOT EXISTS admin_snapshots (
			id SERIAL PRIMARY KEY,
			namespace TEXT NOT NULL,
			snapshot_key TEXT NOT NULL,
			payload TEXT NOT NULL,
			generated_at TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			stale_until TEXT NOT NULL,
			created_at TEXT,
			updated_at TEXT,
			CONSTRAINT admin_snapshots_namespace_key_unique UNIQUE (namespace, snapshot_key)
		)`
	}
	return `CREATE TABLE IF NOT EXISTS admin_snapshots (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		namespace TEXT NOT NULL,
		snapshot_key TEXT NOT NULL,
		payload TEXT NOT NULL,
		generated_at TEXT NOT NULL,
		expires_at TEXT NOT NULL,
		stale_until TEXT NOT NULL,
		created_at TEXT,
		updated_at TEXT,
		CONSTRAINT admin_snapshots_namespace_key_unique UNIQUE (namespace, snapshot_key)
	)`
}

func buildAnalyticsProjectionCheckpointsDDL(d string) string {
	return `CREATE TABLE IF NOT EXISTS analytics_projection_checkpoints (
		projector_key TEXT PRIMARY KEY,
		time_zone TEXT NOT NULL DEFAULT 'Local',
		last_proxy_log_id INTEGER NOT NULL DEFAULT 0,
		watermark_created_at TEXT,
		lease_owner TEXT,
		lease_token TEXT,
		lease_expires_at TEXT,
		recompute_from_id INTEGER,
		recompute_requested_at TEXT,
		recompute_reason TEXT,
		recompute_started_at TEXT,
		recompute_completed_at TEXT,
		last_projected_at TEXT,
		last_successful_at TEXT,
		last_error TEXT,
		created_at TEXT,
		updated_at TEXT
	)`
}

func buildSiteDayUsageDDL(d string) string {
	if isPG(d) {
		return `CREATE TABLE IF NOT EXISTS site_day_usage (
			id SERIAL PRIMARY KEY,
			local_day TEXT NOT NULL,
			site_id INTEGER NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
			total_calls INTEGER NOT NULL DEFAULT 0,
			success_calls INTEGER NOT NULL DEFAULT 0,
			failed_calls INTEGER NOT NULL DEFAULT 0,
			total_tokens INTEGER NOT NULL DEFAULT 0,
			total_summary_spend DOUBLE PRECISION NOT NULL DEFAULT 0,
			total_site_spend DOUBLE PRECISION NOT NULL DEFAULT 0,
			total_latency_ms INTEGER NOT NULL DEFAULT 0,
			latency_count INTEGER NOT NULL DEFAULT 0,
			created_at TEXT,
			updated_at TEXT,
			CONSTRAINT site_day_usage_day_site_unique UNIQUE (local_day, site_id),
			CONSTRAINT site_day_usage_non_negative CHECK (
				total_calls >= 0 AND success_calls >= 0 AND failed_calls >= 0 AND
				total_tokens >= 0 AND total_summary_spend >= 0 AND total_site_spend >= 0 AND
				total_latency_ms >= 0 AND latency_count >= 0
			)
		)`
	}
	return `CREATE TABLE IF NOT EXISTS site_day_usage (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		local_day TEXT NOT NULL,
		site_id INTEGER NOT NULL,
		total_calls INTEGER NOT NULL DEFAULT 0,
		success_calls INTEGER NOT NULL DEFAULT 0,
		failed_calls INTEGER NOT NULL DEFAULT 0,
		total_tokens INTEGER NOT NULL DEFAULT 0,
		total_summary_spend REAL NOT NULL DEFAULT 0,
		total_site_spend REAL NOT NULL DEFAULT 0,
		total_latency_ms INTEGER NOT NULL DEFAULT 0,
		latency_count INTEGER NOT NULL DEFAULT 0,
		created_at TEXT,
		updated_at TEXT,
		CONSTRAINT site_day_usage_day_site_unique UNIQUE (local_day, site_id),
		CONSTRAINT site_day_usage_non_negative CHECK (
			total_calls >= 0 AND success_calls >= 0 AND failed_calls >= 0 AND
			total_tokens >= 0 AND total_summary_spend >= 0 AND total_site_spend >= 0 AND
			total_latency_ms >= 0 AND latency_count >= 0
		),
		FOREIGN KEY (site_id) REFERENCES sites(id) ON DELETE CASCADE
	)`
}

func buildSiteHourUsageDDL(d string) string {
	if isPG(d) {
		return `CREATE TABLE IF NOT EXISTS site_hour_usage (
			id SERIAL PRIMARY KEY,
			bucket_start_utc TEXT NOT NULL,
			site_id INTEGER NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
			total_calls INTEGER NOT NULL DEFAULT 0,
			success_calls INTEGER NOT NULL DEFAULT 0,
			failed_calls INTEGER NOT NULL DEFAULT 0,
			total_tokens INTEGER NOT NULL DEFAULT 0,
			total_summary_spend DOUBLE PRECISION NOT NULL DEFAULT 0,
			total_site_spend DOUBLE PRECISION NOT NULL DEFAULT 0,
			total_latency_ms INTEGER NOT NULL DEFAULT 0,
			latency_count INTEGER NOT NULL DEFAULT 0,
			created_at TEXT,
			updated_at TEXT,
			CONSTRAINT site_hour_usage_hour_site_unique UNIQUE (bucket_start_utc, site_id),
			CONSTRAINT site_hour_usage_non_negative CHECK (
				total_calls >= 0 AND success_calls >= 0 AND failed_calls >= 0 AND
				total_tokens >= 0 AND total_summary_spend >= 0 AND total_site_spend >= 0 AND
				total_latency_ms >= 0 AND latency_count >= 0
			)
		)`
	}
	return `CREATE TABLE IF NOT EXISTS site_hour_usage (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		bucket_start_utc TEXT NOT NULL,
		site_id INTEGER NOT NULL,
		total_calls INTEGER NOT NULL DEFAULT 0,
		success_calls INTEGER NOT NULL DEFAULT 0,
		failed_calls INTEGER NOT NULL DEFAULT 0,
		total_tokens INTEGER NOT NULL DEFAULT 0,
		total_summary_spend REAL NOT NULL DEFAULT 0,
		total_site_spend REAL NOT NULL DEFAULT 0,
		total_latency_ms INTEGER NOT NULL DEFAULT 0,
		latency_count INTEGER NOT NULL DEFAULT 0,
		created_at TEXT,
		updated_at TEXT,
		CONSTRAINT site_hour_usage_hour_site_unique UNIQUE (bucket_start_utc, site_id),
		CONSTRAINT site_hour_usage_non_negative CHECK (
			total_calls >= 0 AND success_calls >= 0 AND failed_calls >= 0 AND
			total_tokens >= 0 AND total_summary_spend >= 0 AND total_site_spend >= 0 AND
			total_latency_ms >= 0 AND latency_count >= 0
		),
		FOREIGN KEY (site_id) REFERENCES sites(id) ON DELETE CASCADE
	)`
}

func buildModelDayUsageDDL(d string) string {
	if isPG(d) {
		return `CREATE TABLE IF NOT EXISTS model_day_usage (
			id SERIAL PRIMARY KEY,
			local_day TEXT NOT NULL,
			site_id INTEGER NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
			model TEXT NOT NULL,
			total_calls INTEGER NOT NULL DEFAULT 0,
			success_calls INTEGER NOT NULL DEFAULT 0,
			failed_calls INTEGER NOT NULL DEFAULT 0,
			total_tokens INTEGER NOT NULL DEFAULT 0,
			total_spend DOUBLE PRECISION NOT NULL DEFAULT 0,
			total_latency_ms INTEGER NOT NULL DEFAULT 0,
			latency_count INTEGER NOT NULL DEFAULT 0,
			created_at TEXT,
			updated_at TEXT,
			CONSTRAINT model_day_usage_day_site_model_unique UNIQUE (local_day, site_id, model),
			CONSTRAINT model_day_usage_non_negative CHECK (
				total_calls >= 0 AND success_calls >= 0 AND failed_calls >= 0 AND
				total_tokens >= 0 AND total_spend >= 0 AND
				total_latency_ms >= 0 AND latency_count >= 0
			)
		)`
	}
	return `CREATE TABLE IF NOT EXISTS model_day_usage (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		local_day TEXT NOT NULL,
		site_id INTEGER NOT NULL,
		model TEXT NOT NULL,
		total_calls INTEGER NOT NULL DEFAULT 0,
		success_calls INTEGER NOT NULL DEFAULT 0,
		failed_calls INTEGER NOT NULL DEFAULT 0,
		total_tokens INTEGER NOT NULL DEFAULT 0,
		total_spend REAL NOT NULL DEFAULT 0,
		total_latency_ms INTEGER NOT NULL DEFAULT 0,
		latency_count INTEGER NOT NULL DEFAULT 0,
		created_at TEXT,
		updated_at TEXT,
		CONSTRAINT model_day_usage_day_site_model_unique UNIQUE (local_day, site_id, model),
		CONSTRAINT model_day_usage_non_negative CHECK (
			total_calls >= 0 AND success_calls >= 0 AND failed_calls >= 0 AND
			total_tokens >= 0 AND total_spend >= 0 AND
			total_latency_ms >= 0 AND latency_count >= 0
		),
		FOREIGN KEY (site_id) REFERENCES sites(id) ON DELETE CASCADE
	)`
}

func buildDownstreamAPIKeysDDL(d string) string {
	if isPG(d) {
		return `CREATE TABLE IF NOT EXISTS downstream_api_keys (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			key TEXT NOT NULL,
			description TEXT,
			group_name TEXT,
			tags TEXT,
			enabled BOOLEAN DEFAULT TRUE,
			expires_at TEXT,
			max_cost DOUBLE PRECISION,
			used_cost DOUBLE PRECISION DEFAULT 0,
			max_requests INTEGER,
			used_requests INTEGER DEFAULT 0,
			supported_models TEXT,
			allowed_route_ids TEXT,
			site_weight_multipliers TEXT,
			excluded_site_ids TEXT,
			excluded_credential_refs TEXT,
			last_used_at TEXT,
			created_at TEXT,
			updated_at TEXT,
			CONSTRAINT downstream_api_keys_key_unique UNIQUE (key)
		)`
	}
	return `CREATE TABLE IF NOT EXISTS downstream_api_keys (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		key TEXT NOT NULL,
		description TEXT,
		group_name TEXT,
		tags TEXT,
		enabled INTEGER DEFAULT 1,
		expires_at TEXT,
		max_cost REAL,
		used_cost REAL DEFAULT 0,
		max_requests INTEGER,
		used_requests INTEGER DEFAULT 0,
		supported_models TEXT,
		allowed_route_ids TEXT,
		site_weight_multipliers TEXT,
		excluded_site_ids TEXT,
		excluded_credential_refs TEXT,
		last_used_at TEXT,
		created_at TEXT,
		updated_at TEXT,
		CONSTRAINT downstream_api_keys_key_unique UNIQUE (key)
	)`
}

func buildSiteAnnouncementsDDL(d string) string {
	if isPG(d) {
		return `CREATE TABLE IF NOT EXISTS site_announcements (
			id SERIAL PRIMARY KEY,
			site_id INTEGER NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
			platform TEXT NOT NULL,
			source_key TEXT NOT NULL,
			title TEXT NOT NULL,
			content TEXT NOT NULL,
			level TEXT NOT NULL DEFAULT 'info',
			source_url TEXT,
			starts_at TEXT,
			ends_at TEXT,
			upstream_created_at TEXT,
			upstream_updated_at TEXT,
			first_seen_at TEXT,
			last_seen_at TEXT,
			read_at TEXT,
			dismissed_at TEXT,
			raw_payload TEXT,
			created_at TEXT,
			updated_at TEXT,
			CONSTRAINT site_announcements_site_source_key_unique UNIQUE (site_id, source_key)
		)`
	}
	return `CREATE TABLE IF NOT EXISTS site_announcements (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		site_id INTEGER NOT NULL,
		platform TEXT NOT NULL,
		source_key TEXT NOT NULL,
		title TEXT NOT NULL,
		content TEXT NOT NULL,
		level TEXT NOT NULL DEFAULT 'info',
		source_url TEXT,
		starts_at TEXT,
		ends_at TEXT,
		upstream_created_at TEXT,
		upstream_updated_at TEXT,
		first_seen_at TEXT,
		last_seen_at TEXT,
		read_at TEXT,
		dismissed_at TEXT,
		raw_payload TEXT,
		created_at TEXT,
		updated_at TEXT,
		CONSTRAINT site_announcements_site_source_key_unique UNIQUE (site_id, source_key),
		FOREIGN KEY (site_id) REFERENCES sites(id) ON DELETE CASCADE
	)`
}

func buildEventsDDL(d string) string {
	if isPG(d) {
		return `CREATE TABLE IF NOT EXISTS events (
			id SERIAL PRIMARY KEY,
			type TEXT NOT NULL,
			title TEXT NOT NULL,
			message TEXT,
			level TEXT NOT NULL DEFAULT 'info',
			read BOOLEAN DEFAULT FALSE,
			related_id INTEGER,
			related_type TEXT,
			created_at TEXT
		)`
	}
	return `CREATE TABLE IF NOT EXISTS events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		type TEXT NOT NULL,
		title TEXT NOT NULL,
		message TEXT,
		level TEXT NOT NULL DEFAULT 'info',
		read INTEGER DEFAULT 0,
		related_id INTEGER,
		related_type TEXT,
		created_at TEXT
	)`
}

// buildIndexes returns all 67 non-UNIQUE index creation statements.
// Both SQLite and PostgreSQL support CREATE INDEX IF NOT EXISTS syntax.
// UNIQUE constraints are already handled inside CREATE TABLE via CONSTRAINT ... UNIQUE.
func buildIndexes() []struct {
	name string
	sql  string
} {
	return []struct {
		name string
		sql  string
	}{
		// sites
		{"sites_status_idx", `CREATE INDEX IF NOT EXISTS sites_status_idx ON sites (status)`},
		// site_api_endpoints
		{"site_api_endpoints_site_enabled_sort_idx", `CREATE INDEX IF NOT EXISTS site_api_endpoints_site_enabled_sort_idx ON site_api_endpoints (site_id, enabled, sort_order)`},
		{"site_api_endpoints_site_cooldown_idx", `CREATE INDEX IF NOT EXISTS site_api_endpoints_site_cooldown_idx ON site_api_endpoints (site_id, cooldown_until)`},
		// site_disabled_models
		{"site_disabled_models_site_id_idx", `CREATE INDEX IF NOT EXISTS site_disabled_models_site_id_idx ON site_disabled_models (site_id)`},
		// accounts
		{"accounts_site_id_idx", `CREATE INDEX IF NOT EXISTS accounts_site_id_idx ON accounts (site_id)`},
		{"accounts_status_idx", `CREATE INDEX IF NOT EXISTS accounts_status_idx ON accounts (status)`},
		{"accounts_site_status_idx", `CREATE INDEX IF NOT EXISTS accounts_site_status_idx ON accounts (site_id, status)`},
		{"accounts_oauth_provider_idx", `CREATE INDEX IF NOT EXISTS accounts_oauth_provider_idx ON accounts (oauth_provider)`},
		{"accounts_oauth_identity_idx", `CREATE INDEX IF NOT EXISTS accounts_oauth_identity_idx ON accounts (oauth_provider, oauth_account_key, oauth_project_id)`},
		// account_tokens
		{"account_tokens_account_id_idx", `CREATE INDEX IF NOT EXISTS account_tokens_account_id_idx ON account_tokens (account_id)`},
		{"account_tokens_account_enabled_idx", `CREATE INDEX IF NOT EXISTS account_tokens_account_enabled_idx ON account_tokens (account_id, enabled)`},
		{"account_tokens_enabled_idx", `CREATE INDEX IF NOT EXISTS account_tokens_enabled_idx ON account_tokens (enabled)`},
		// checkin_logs
		{"checkin_logs_account_created_at_idx", `CREATE INDEX IF NOT EXISTS checkin_logs_account_created_at_idx ON checkin_logs (account_id, created_at)`},
		{"checkin_logs_created_at_idx", `CREATE INDEX IF NOT EXISTS checkin_logs_created_at_idx ON checkin_logs (created_at)`},
		{"checkin_logs_status_idx", `CREATE INDEX IF NOT EXISTS checkin_logs_status_idx ON checkin_logs (status)`},
		// model_availability
		{"model_availability_account_available_idx", `CREATE INDEX IF NOT EXISTS model_availability_account_available_idx ON model_availability (account_id, available)`},
		{"model_availability_model_name_idx", `CREATE INDEX IF NOT EXISTS model_availability_model_name_idx ON model_availability (model_name)`},
		// token_model_availability
		{"token_model_availability_token_available_idx", `CREATE INDEX IF NOT EXISTS token_model_availability_token_available_idx ON token_model_availability (token_id, available)`},
		{"token_model_availability_model_name_idx", `CREATE INDEX IF NOT EXISTS token_model_availability_model_name_idx ON token_model_availability (model_name)`},
		{"token_model_availability_available_idx", `CREATE INDEX IF NOT EXISTS token_model_availability_available_idx ON token_model_availability (available)`},
		// token_routes
		{"token_routes_model_pattern_idx", `CREATE INDEX IF NOT EXISTS token_routes_model_pattern_idx ON token_routes (model_pattern)`},
		{"token_routes_enabled_idx", `CREATE INDEX IF NOT EXISTS token_routes_enabled_idx ON token_routes (enabled)`},
		// route_group_sources
		{"route_group_sources_source_route_id_idx", `CREATE INDEX IF NOT EXISTS route_group_sources_source_route_id_idx ON route_group_sources (source_route_id)`},
		// oauth_route_units
		{"oauth_route_units_site_provider_idx", `CREATE INDEX IF NOT EXISTS oauth_route_units_site_provider_idx ON oauth_route_units (site_id, provider)`},
		{"oauth_route_units_enabled_idx", `CREATE INDEX IF NOT EXISTS oauth_route_units_enabled_idx ON oauth_route_units (enabled)`},
		// oauth_route_unit_members
		{"oauth_route_unit_members_unit_sort_idx", `CREATE INDEX IF NOT EXISTS oauth_route_unit_members_unit_sort_idx ON oauth_route_unit_members (unit_id, sort_order)`},
		{"oauth_route_unit_members_unit_cooldown_idx", `CREATE INDEX IF NOT EXISTS oauth_route_unit_members_unit_cooldown_idx ON oauth_route_unit_members (unit_id, cooldown_until)`},
		// route_channels
		{"route_channels_route_id_idx", `CREATE INDEX IF NOT EXISTS route_channels_route_id_idx ON route_channels (route_id)`},
		{"route_channels_account_id_idx", `CREATE INDEX IF NOT EXISTS route_channels_account_id_idx ON route_channels (account_id)`},
		{"route_channels_token_id_idx", `CREATE INDEX IF NOT EXISTS route_channels_token_id_idx ON route_channels (token_id)`},
		{"route_channels_oauth_route_unit_id_idx", `CREATE INDEX IF NOT EXISTS route_channels_oauth_route_unit_id_idx ON route_channels (oauth_route_unit_id)`},
		{"route_channels_route_enabled_idx", `CREATE INDEX IF NOT EXISTS route_channels_route_enabled_idx ON route_channels (route_id, enabled)`},
		{"route_channels_route_token_idx", `CREATE INDEX IF NOT EXISTS route_channels_route_token_idx ON route_channels (route_id, token_id)`},
		// proxy_logs
		{"proxy_logs_created_at_idx", `CREATE INDEX IF NOT EXISTS proxy_logs_created_at_idx ON proxy_logs (created_at)`},
		{"proxy_logs_account_created_at_idx", `CREATE INDEX IF NOT EXISTS proxy_logs_account_created_at_idx ON proxy_logs (account_id, created_at)`},
		{"proxy_logs_status_created_at_idx", `CREATE INDEX IF NOT EXISTS proxy_logs_status_created_at_idx ON proxy_logs (status, created_at)`},
		{"proxy_logs_model_actual_created_at_idx", `CREATE INDEX IF NOT EXISTS proxy_logs_model_actual_created_at_idx ON proxy_logs (model_actual, created_at)`},
		{"proxy_logs_downstream_api_key_created_at_idx", `CREATE INDEX IF NOT EXISTS proxy_logs_downstream_api_key_created_at_idx ON proxy_logs (downstream_api_key_id, created_at)`},
		{"proxy_logs_client_app_id_created_at_idx", `CREATE INDEX IF NOT EXISTS proxy_logs_client_app_id_created_at_idx ON proxy_logs (client_app_id, created_at)`},
		{"proxy_logs_client_family_created_at_idx", `CREATE INDEX IF NOT EXISTS proxy_logs_client_family_created_at_idx ON proxy_logs (client_family, created_at)`},
		// proxy_debug_traces
		{"proxy_debug_traces_created_at_idx", `CREATE INDEX IF NOT EXISTS proxy_debug_traces_created_at_idx ON proxy_debug_traces (created_at)`},
		{"proxy_debug_traces_session_created_at_idx", `CREATE INDEX IF NOT EXISTS proxy_debug_traces_session_created_at_idx ON proxy_debug_traces (session_id, created_at)`},
		{"proxy_debug_traces_model_created_at_idx", `CREATE INDEX IF NOT EXISTS proxy_debug_traces_model_created_at_idx ON proxy_debug_traces (requested_model, created_at)`},
		{"proxy_debug_traces_final_status_created_at_idx", `CREATE INDEX IF NOT EXISTS proxy_debug_traces_final_status_created_at_idx ON proxy_debug_traces (final_status, created_at)`},
		// proxy_debug_attempts
		{"proxy_debug_attempts_trace_created_at_idx", `CREATE INDEX IF NOT EXISTS proxy_debug_attempts_trace_created_at_idx ON proxy_debug_attempts (trace_id, created_at)`},
		// proxy_video_tasks
		{"proxy_video_tasks_upstream_video_id_idx", `CREATE INDEX IF NOT EXISTS proxy_video_tasks_upstream_video_id_idx ON proxy_video_tasks (upstream_video_id)`},
		{"proxy_video_tasks_created_at_idx", `CREATE INDEX IF NOT EXISTS proxy_video_tasks_created_at_idx ON proxy_video_tasks (created_at)`},
		// proxy_files
		{"proxy_files_owner_lookup_idx", `CREATE INDEX IF NOT EXISTS proxy_files_owner_lookup_idx ON proxy_files (owner_type, owner_id, deleted_at)`},
		// admin_snapshots
		{"admin_snapshots_expires_at_idx", `CREATE INDEX IF NOT EXISTS admin_snapshots_expires_at_idx ON admin_snapshots (expires_at)`},
		{"admin_snapshots_stale_until_idx", `CREATE INDEX IF NOT EXISTS admin_snapshots_stale_until_idx ON admin_snapshots (stale_until)`},
		// analytics_projection_checkpoints
		{"analytics_projection_checkpoints_recompute_from_id_idx", `CREATE INDEX IF NOT EXISTS analytics_projection_checkpoints_recompute_from_id_idx ON analytics_projection_checkpoints (recompute_from_id)`},
		{"analytics_projection_checkpoints_lease_expires_at_idx", `CREATE INDEX IF NOT EXISTS analytics_projection_checkpoints_lease_expires_at_idx ON analytics_projection_checkpoints (lease_expires_at)`},
		// site_day_usage
		{"site_day_usage_day_idx", `CREATE INDEX IF NOT EXISTS site_day_usage_day_idx ON site_day_usage (local_day)`},
		{"site_day_usage_site_id_idx", `CREATE INDEX IF NOT EXISTS site_day_usage_site_id_idx ON site_day_usage (site_id)`},
		// site_hour_usage
		{"site_hour_usage_hour_idx", `CREATE INDEX IF NOT EXISTS site_hour_usage_hour_idx ON site_hour_usage (bucket_start_utc)`},
		{"site_hour_usage_site_id_idx", `CREATE INDEX IF NOT EXISTS site_hour_usage_site_id_idx ON site_hour_usage (site_id)`},
		// model_day_usage
		{"model_day_usage_day_idx", `CREATE INDEX IF NOT EXISTS model_day_usage_day_idx ON model_day_usage (local_day)`},
		{"model_day_usage_site_id_idx", `CREATE INDEX IF NOT EXISTS model_day_usage_site_id_idx ON model_day_usage (site_id)`},
		{"model_day_usage_model_idx", `CREATE INDEX IF NOT EXISTS model_day_usage_model_idx ON model_day_usage (model)`},
		// downstream_api_keys
		{"downstream_api_keys_name_idx", `CREATE INDEX IF NOT EXISTS downstream_api_keys_name_idx ON downstream_api_keys (name)`},
		{"downstream_api_keys_enabled_idx", `CREATE INDEX IF NOT EXISTS downstream_api_keys_enabled_idx ON downstream_api_keys (enabled)`},
		{"downstream_api_keys_expires_at_idx", `CREATE INDEX IF NOT EXISTS downstream_api_keys_expires_at_idx ON downstream_api_keys (expires_at)`},
		// site_announcements
		{"site_announcements_site_id_first_seen_at_idx", `CREATE INDEX IF NOT EXISTS site_announcements_site_id_first_seen_at_idx ON site_announcements (site_id, first_seen_at)`},
		{"site_announcements_read_at_idx", `CREATE INDEX IF NOT EXISTS site_announcements_read_at_idx ON site_announcements (read_at)`},
		// events
		{"events_read_created_at_idx", `CREATE INDEX IF NOT EXISTS events_read_created_at_idx ON events (read, created_at)`},
		{"events_type_created_at_idx", `CREATE INDEX IF NOT EXISTS events_type_created_at_idx ON events (type, created_at)`},
		{"events_created_at_idx", `CREATE INDEX IF NOT EXISTS events_created_at_idx ON events (created_at)`},
	}
}

// Migrate is a backward-compatible entry point for the P0 stub API.
// It runs additional runtime schema migrations beyond the core CREATE TABLE
// statements. AutoMigrate (called from EnsureRuntimeDatabase) already handles
// the 27-table bootstrap. This function handles incremental compat checks.
func Migrate(cfg *config.Config) error {
	db := GetDB()
	if db == nil {
		slog.Warn("migrate: database not initialized, skipping")
		return nil
	}

	slog.Info("migrate: running runtime schema compatibility checks", "dialect", db.Dialect)

	// P1: Core tables already created by AutoMigrate.
	// Future P2+ work will add incremental schema checks here:
	//   - ensure columns exist, add missing via ALTER TABLE ADD COLUMN
	//   - ensure indexes exist, add missing via CREATE INDEX IF NOT EXISTS
	//   - deduplicate stale sites before creating unique index
	// These map to the TS compatibility functions in index.ts.

	slog.Info("migrate: runtime schema compatibility complete")
	return nil
}
