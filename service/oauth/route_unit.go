package oauth

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/tokendancelab/metapi-go/store"
)

// OAuthRouteUnitStrategy is the strategy for route unit member selection.
type OAuthRouteUnitStrategy string

const (
	RouteUnitStrategyRoundRobin         OAuthRouteUnitStrategy = "round_robin"
	RouteUnitStrategyStickUntilUnavailable OAuthRouteUnitStrategy = "stick_until_unavailable"
)

// OAuthRouteUnitSummary holds a summary of a route unit.
type OAuthRouteUnitSummary struct {
	ID          int64                  `json:"id"`
	SiteID      int64                  `json:"siteId"`
	Provider    string                 `json:"provider"`
	Name        string                 `json:"name"`
	Strategy    OAuthRouteUnitStrategy `json:"strategy"`
	Enabled     bool                   `json:"enabled"`
	MemberCount int64                  `json:"memberCount"`
}

// OAuthRouteUnitAccountParticipation represents route unit participation for an account.
type OAuthRouteUnitAccountParticipation struct {
	Kind        string                 `json:"kind"`
	ID          int64                  `json:"id"`
	Name        string                 `json:"name"`
	Strategy    OAuthRouteUnitStrategy `json:"strategy"`
	MemberCount int64                  `json:"memberCount"`
}

// CreateRouteUnitInput holds input for creating a route unit.
type CreateRouteUnitInput struct {
	AccountIDs []int64
	Name       string
	Strategy   OAuthRouteUnitStrategy
}

// CreateRouteUnitResult holds the result of creating a route unit.
type CreateRouteUnitResult struct {
	Success   bool                    `json:"success"`
	RouteUnit *OAuthRouteUnitSummary  `json:"routeUnit"`
}

// ListOauthRouteUnitsByAccountIDs returns route unit participation for a set of accounts.
func ListOauthRouteUnitsByAccountIDs(accountIDs []int64) map[int64]*OAuthRouteUnitAccountParticipation {
	if len(accountIDs) == 0 {
		return nil
	}

	db := store.GetDB()
	if db == nil {
		return nil
	}

	query, args, err := sqlx.In(
		`SELECT m.account_id, u.id as unit_id, u.name, u.strategy,
		        COUNT(*) OVER (PARTITION BY u.id) as member_count
		 FROM oauth_route_unit_members m
		 INNER JOIN oauth_route_units u ON m.unit_id = u.id
		 WHERE m.account_id IN (?)`, accountIDs)
	if err != nil {
		return nil
	}
	query = db.Rebind(query)

	type row struct {
		AccountID   int64  `db:"account_id"`
		UnitID      int64  `db:"unit_id"`
		Name        string `db:"name"`
		Strategy    string `db:"strategy"`
		MemberCount int64  `db:"member_count"`
	}

	var rows []row
	if err := db.Select(&rows, query, args...); err != nil {
		return nil
	}

	result := make(map[int64]*OAuthRouteUnitAccountParticipation)
	for _, r := range rows {
		strategy := normalizeStrategy(r.Strategy)
		result[r.AccountID] = &OAuthRouteUnitAccountParticipation{
			Kind:        "route_unit",
			ID:          r.UnitID,
			Name:        r.Name,
			Strategy:    strategy,
			MemberCount: max64(r.MemberCount, 1),
		}
	}
	return result
}

// ListEnabledOauthRouteUnitsWithMembers returns all enabled route units with their members.
func ListEnabledOauthRouteUnitsWithMembers() []struct {
	Unit    store.OAuthRouteUnit
	Members []store.OAuthRouteUnitMember
} {
	db := store.GetDB()
	if db == nil {
		return nil
	}

	var units []store.OAuthRouteUnit
	if err := db.Select(&units, "SELECT * FROM oauth_route_units WHERE enabled = TRUE"); err != nil {
		return nil
	}
	if len(units) == 0 {
		return nil
	}

	unitIDs := make([]int64, len(units))
	for i, u := range units {
		unitIDs[i] = u.ID
	}

	query, args, _ := sqlx.In(
		"SELECT * FROM oauth_route_unit_members WHERE unit_id IN (?) ORDER BY sort_order, id", unitIDs)
	query = db.Rebind(query)

	var allMembers []store.OAuthRouteUnitMember
	if err := db.Select(&allMembers, query, args...); err != nil {
		return nil
	}

	membersByUnit := make(map[int64][]store.OAuthRouteUnitMember)
	for _, m := range allMembers {
		membersByUnit[m.UnitID] = append(membersByUnit[m.UnitID], m)
	}

	result := make([]struct {
		Unit    store.OAuthRouteUnit
		Members []store.OAuthRouteUnitMember
	}, len(units))
	for i, u := range units {
		result[i].Unit = u
		result[i].Members = membersByUnit[u.ID]
	}
	return result
}

// CreateOauthRouteUnit creates a new OAuth route unit.
func CreateOauthRouteUnit(input CreateRouteUnitInput) (*CreateRouteUnitResult, error) {
	db := store.GetDB()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	accountIDs := uniquePositiveIDs(input.AccountIDs)
	if len(accountIDs) < 2 {
		return nil, fmt.Errorf("oauth route unit requires at least 2 accounts")
	}

	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, fmt.Errorf("oauth route unit name is required")
	}

	strategy := normalizeStrategy(string(input.Strategy))
	if strategy != RouteUnitStrategyRoundRobin && strategy != RouteUnitStrategyStickUntilUnavailable {
		return nil, fmt.Errorf("invalid oauth route unit strategy")
	}

	// Load accounts with site join.
	query, args, _ := sqlx.In(
		`SELECT a.*, s.* FROM accounts a
		 INNER JOIN sites s ON a.site_id = s.id
		 WHERE a.id IN (?)`, accountIDs)
	query = db.Rebind(query)

	type accountWithSite struct {
		Account store.Account `db:"accounts"`
		Site    store.Site    `db:"sites"`
	}
	var rows []accountWithSite
	if err := db.Select(&rows, query, args...); err != nil {
		return nil, fmt.Errorf("oauth route unit accounts not found")
	}
	if len(rows) != len(accountIDs) {
		return nil, fmt.Errorf("oauth route unit accounts not found")
	}

	first := rows[0]
	expectedSiteID := first.Account.SiteID
	expectedProvider := ""
	if first.Account.OAuthProvider != nil {
		expectedProvider = strings.TrimSpace(*first.Account.OAuthProvider)
	}
	if expectedProvider == "" {
		return nil, fmt.Errorf("oauth route unit only supports oauth accounts")
	}

	for _, row := range rows {
		if row.Account.SiteID != expectedSiteID {
			return nil, fmt.Errorf("oauth route unit accounts must belong to the same site")
		}
		p := ""
		if row.Account.OAuthProvider != nil {
			p = strings.TrimSpace(*row.Account.OAuthProvider)
		}
		if p != expectedProvider {
			return nil, fmt.Errorf("oauth route unit accounts must share the same provider")
		}
	}

	// Check uniqueness — accounts already in route units.
	checkQuery, checkArgs, _ := sqlx.In(
		"SELECT account_id FROM oauth_route_unit_members WHERE account_id IN (?)", accountIDs)
	checkQuery = db.Rebind(checkQuery)

	var existingMembers []struct {
		AccountID int64 `db:"account_id"`
	}
	if err := db.Select(&existingMembers, checkQuery, checkArgs...); err == nil && len(existingMembers) > 0 {
		return nil, fmt.Errorf("oauth route unit accounts already grouped")
	}

	// Create in transaction.
	tx, err := db.Beginx()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().Format(time.RFC3339)
	result, err := tx.Exec(
		`INSERT INTO oauth_route_units (site_id, provider, name, strategy, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 1, ?, ?)`,
		expectedSiteID, expectedProvider, name, string(strategy), now, now)
	if err != nil {
		return nil, fmt.Errorf("oauth route unit creation failed: %w", err)
	}

	unitID, err := result.LastInsertId()
	if err != nil {
		// Fallback for Postgres.
		tx.Get(&unitID, "SELECT id FROM oauth_route_units WHERE site_id = ? AND name = ? ORDER BY id DESC LIMIT 1",
			expectedSiteID, name)
	}

	for i, accountID := range accountIDs {
		_, err := tx.Exec(
			`INSERT INTO oauth_route_unit_members (unit_id, account_id, sort_order, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?)`,
			unitID, accountID, i, now, now)
		if err != nil {
			return nil, fmt.Errorf("oauth route unit accounts already grouped")
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// Step 5: Rebuild routes.
	hooks := getWorkflowHooks()
	if hooks != nil {
		if err := hooks.RebuildRoutesOnly(context.Background()); err != nil {
			// Step 6: On rebuild failure, rollback created route unit.
			slog.Warn("route rebuild failed after creating route unit, rolling back", "unitID", unitID, "error", err)
			rollbackErr := rollbackCreateRouteUnit(unitID, accountIDs)
			if rollbackErr != nil {
				slog.Warn("rollback route unit creation failed", "error", rollbackErr)
			}
			// Best-effort retry rebuild after rollback.
			_ = hooks.RebuildRoutesOnly(context.Background())
			return nil, fmt.Errorf("route rebuild failed: %w", err)
		}
		// Invalidate token router cache.
		hooks.InvalidateTokenRouterCache()
	}

	return &CreateRouteUnitResult{
		Success: true,
		RouteUnit: &OAuthRouteUnitSummary{
			ID:          unitID,
			SiteID:      expectedSiteID,
			Provider:    expectedProvider,
			Name:        name,
			Strategy:    strategy,
			Enabled:     true,
			MemberCount: int64(len(accountIDs)),
		},
	}, nil
}

// UpdateOauthRouteUnit updates a route unit's name and/or strategy.
func UpdateOauthRouteUnit(routeUnitID int64, name *string, strategy *OAuthRouteUnitStrategy) (*OAuthRouteUnitSummary, error) {
	db := store.GetDB()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}

	var unit store.OAuthRouteUnit
	if err := db.Get(&unit, "SELECT * FROM oauth_route_units WHERE id = ?", routeUnitID); err != nil {
		return nil, fmt.Errorf("oauth route unit not found")
	}

	now := time.Now().Format(time.RFC3339)
	if name != nil {
		trimmed := strings.TrimSpace(*name)
		if trimmed == "" {
			return nil, fmt.Errorf("oauth route unit name is required")
		}
		unit.Name = trimmed
	}
	if strategy != nil {
		ns := normalizeStrategy(string(*strategy))
		if ns != RouteUnitStrategyRoundRobin && ns != RouteUnitStrategyStickUntilUnavailable {
			return nil, fmt.Errorf("invalid oauth route unit strategy")
		}
		unit.Strategy = string(ns)
	}

	_, err := db.Exec(
		"UPDATE oauth_route_units SET name = ?, strategy = ?, updated_at = ? WHERE id = ?",
		unit.Name, unit.Strategy, now, routeUnitID)
	if err != nil {
		return nil, err
	}

	var memberCount int64
	db.Get(&memberCount, "SELECT COUNT(*) FROM oauth_route_unit_members WHERE unit_id = ?", routeUnitID)

	// Step 3 (spec): Invalidate token router cache.
	hooks := getWorkflowHooks()
	if hooks != nil {
		hooks.InvalidateTokenRouterCache()
	}

	return &OAuthRouteUnitSummary{
		ID:          unit.ID,
		SiteID:      unit.SiteID,
		Provider:    unit.Provider,
		Name:        unit.Name,
		Strategy:    normalizeStrategy(unit.Strategy),
		Enabled:     unit.Enabled,
		MemberCount: memberCount,
	}, nil
}

// DeleteOauthRouteUnit deletes a route unit with snapshot + rebuild + rollback.
func DeleteOauthRouteUnit(routeUnitID int64) error {
	db := store.GetDB()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}

	var unit store.OAuthRouteUnit
	if err := db.Get(&unit, "SELECT * FROM oauth_route_units WHERE id = ?", routeUnitID); err != nil {
		return fmt.Errorf("oauth route unit not found")
	}

	// Step 2: Snapshot current members + channels for rollback.
	var snapshotMembers []store.OAuthRouteUnitMember
	db.Select(&snapshotMembers, "SELECT * FROM oauth_route_unit_members WHERE unit_id = ?", routeUnitID)

	var snapshotChannels []store.RouteChannel
	db.Select(&snapshotChannels, "SELECT * FROM route_channels WHERE oauth_route_unit_id = ?", routeUnitID)

	// Step 3: Delete channels -> members -> unit.
	_, err := db.Exec("DELETE FROM route_channels WHERE oauth_route_unit_id = ?", routeUnitID)
	if err != nil {
		return err
	}
	_, err = db.Exec("DELETE FROM oauth_route_unit_members WHERE unit_id = ?", routeUnitID)
	if err != nil {
		// Rollback: re-insert channels.
		for _, ch := range snapshotChannels {
			rollbackInsertChannel(db, &ch)
		}
		return err
	}
	_, err = db.Exec("DELETE FROM oauth_route_units WHERE id = ?", routeUnitID)
	if err != nil {
		// Rollback: re-insert members + channels.
		for _, m := range snapshotMembers {
			rollbackInsertMember(db, &m)
		}
		for _, ch := range snapshotChannels {
			rollbackInsertChannel(db, &ch)
		}
		return err
	}

	// Step 4: Rebuild routes.
	hooks := getWorkflowHooks()
	if hooks != nil {
		if err := hooks.RebuildRoutesOnly(context.Background()); err != nil {
			// Step 5: On rebuild failure: full rollback (re-insert unit + members + channels).
			slog.Warn("route rebuild failed after deleting route unit, rolling back", "routeUnitID", routeUnitID, "error", err)
			rbErr := rollbackDeleteRouteUnit(db, &unit, snapshotMembers, snapshotChannels)
			if rbErr != nil {
				slog.Warn("rollback route unit deletion failed", "error", rbErr)
			}
			// Best-effort retry rebuild after rollback.
			_ = hooks.RebuildRoutesOnly(context.Background())
			hooks.InvalidateTokenRouterCache()
			return fmt.Errorf("route rebuild failed: %w", err)
		}
		hooks.InvalidateTokenRouterCache()
	}

	return nil
}

// ---- Helpers ----

func uniquePositiveIDs(ids []int64) []int64 {
	seen := make(map[int64]bool)
	result := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id > 0 && !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}
	return result
}

func normalizeStrategy(s string) OAuthRouteUnitStrategy {
	normalized := strings.ToLower(strings.TrimSpace(s))
	switch normalized {
	case "round_robin":
		return RouteUnitStrategyRoundRobin
	case "stick_until_unavailable":
		return RouteUnitStrategyStickUntilUnavailable
	default:
		return RouteUnitStrategyRoundRobin
	}
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// ---- Rollback helpers ----

// rollbackCreateRouteUnit deletes a route unit and its members when route rebuild fails.
func rollbackCreateRouteUnit(unitID int64, accountIDs []int64) error {
	db := store.GetDB()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := db.Exec("DELETE FROM oauth_route_unit_members WHERE unit_id = ?", unitID)
	if err != nil {
		return err
	}
	_, err = db.Exec("DELETE FROM oauth_route_units WHERE id = ?", unitID)
	return err
}

// rollbackDeleteRouteUnit re-inserts a route unit, its members, and channels on rebuild failure.
func rollbackDeleteRouteUnit(db *store.DB, unit *store.OAuthRouteUnit, members []store.OAuthRouteUnitMember, channels []store.RouteChannel) error {
	now := time.Now().Format(time.RFC3339)
	_, err := db.Exec(
		`INSERT INTO oauth_route_units (id, site_id, provider, name, strategy, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		unit.ID, unit.SiteID, unit.Provider, unit.Name, unit.Strategy, unit.Enabled, unit.CreatedAt, now)
	if err != nil {
		return err
	}
	for _, m := range members {
		if err := rollbackInsertMember(db, &m); err != nil {
			return err
		}
	}
	for _, ch := range channels {
		if err := rollbackInsertChannel(db, &ch); err != nil {
			return err
		}
	}
	return nil
}

func rollbackInsertMember(db *store.DB, m *store.OAuthRouteUnitMember) error {
	now := time.Now().Format(time.RFC3339)
	createdAt := now
	updatedAt := now
	if m.CreatedAt != "" {
		createdAt = m.CreatedAt
	}
	if m.UpdatedAt != "" {
		updatedAt = m.UpdatedAt
	}
	_, err := db.Exec(
		`INSERT INTO oauth_route_unit_members (unit_id, account_id, sort_order, success_count, fail_count,
		 total_latency_ms, total_cost, last_used_at, last_selected_at, last_fail_at,
		 consecutive_fail_count, cooldown_level, cooldown_until, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.UnitID, m.AccountID, m.SortOrder, m.SuccessCount, m.FailCount,
		m.TotalLatencyMs, m.TotalCost, m.LastUsedAt, m.LastSelectedAt, m.LastFailAt,
		m.ConsecutiveFailCount, m.CooldownLevel, m.CooldownUntil,
		createdAt, updatedAt)
	return err
}

func rollbackInsertChannel(db *store.DB, ch *store.RouteChannel) error {
	_, err := db.Exec(
		`INSERT INTO route_channels (id, route_id, account_id, token_id, oauth_route_unit_id,
		 source_model, priority, weight, enabled, manual_override,
		 success_count, fail_count, total_latency_ms, total_cost,
		 last_used_at, last_selected_at, last_fail_at,
		 consecutive_fail_count, cooldown_level, cooldown_until)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ch.ID, ch.RouteID, ch.AccountID, ch.TokenID, ch.OAuthRouteUnitID,
		ch.SourceModel, ch.Priority, ch.Weight, ch.Enabled, ch.ManualOverride,
		ch.SuccessCount, ch.FailCount, ch.TotalLatencyMs, ch.TotalCost,
		ch.LastUsedAt, ch.LastSelectedAt, ch.LastFailAt,
		ch.ConsecutiveFailCount, ch.CooldownLevel, ch.CooldownUntil)
	return err
}
