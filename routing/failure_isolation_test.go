package routing

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/tokendancelab/metapi-go/store"
)

// =============================================================================
// R1 / #25 — Channel failure isolation (no cascade to sibling channels)
//
// Proves RecordFailure scopes channel cooldown/failCount to the failed channel
// (or credential-scoped siblings only for short-window usage-limit), and that
// selection soft-filters prefer healthy siblings instead of cascading poison.
// =============================================================================

// isolationDB is a minimal ChannelSelectorDB for RecordFailure isolation tests.
type isolationDB struct {
	mu sync.Mutex

	channels map[int64]*struct {
		Channel store.RouteChannel
		Account store.Account
		Route   store.TokenRoute
	}
	members map[string]*struct {
		Member  store.OAuthRouteUnitMember
		Account store.Account
		Unit    store.OAuthRouteUnit
	}

	// credentialScope maps channelID -> sibling IDs returned by LoadCredentialScopedChannelIDs
	credentialScope map[int64][]int64

	// lastCooldownUpdate records the most recent UpdateChannelCooldownFields call
	lastCooldownIDs     []int64
	lastCooldownUpdates map[string]interface{}
	cooldownCalls       int

	// lastMemberUpdate records OAuth member cooldown writes
	lastMemberID      int64
	lastMemberUpdates map[string]interface{}
	memberCalls       int
}

func newIsolationDB() *isolationDB {
	return &isolationDB{
		channels: make(map[int64]*struct {
			Channel store.RouteChannel
			Account store.Account
			Route   store.TokenRoute
		}),
		members: make(map[string]*struct {
			Member  store.OAuthRouteUnitMember
			Account store.Account
			Unit    store.OAuthRouteUnit
		}),
		credentialScope: make(map[int64][]int64),
	}
}

func (db *isolationDB) seedChannel(ch store.RouteChannel, account store.Account, route store.TokenRoute) {
	db.mu.Lock()
	defer db.mu.Unlock()
	cp := ch
	db.channels[ch.ID] = &struct {
		Channel store.RouteChannel
		Account store.Account
		Route   store.TokenRoute
	}{Channel: cp, Account: account, Route: route}
}

func (db *isolationDB) getChannel(id int64) *store.RouteChannel {
	db.mu.Lock()
	defer db.mu.Unlock()
	row, ok := db.channels[id]
	if !ok {
		return nil
	}
	cp := row.Channel
	return &cp
}

func memberKey(unitID, accountID int64) string {
	return formatInt(unitID) + ":" + formatInt(accountID)
}

func (db *isolationDB) seedMember(member store.OAuthRouteUnitMember, account store.Account, unit store.OAuthRouteUnit) {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.members[memberKey(member.UnitID, member.AccountID)] = &struct {
		Member  store.OAuthRouteUnitMember
		Account store.Account
		Unit    store.OAuthRouteUnit
	}{Member: member, Account: account, Unit: unit}
}

// ---- ChannelSelectorDB implementation (only methods exercised by RecordFailure) ----

func (db *isolationDB) LoadEnabledRoutes(ctx context.Context) ([]store.TokenRoute, error) {
	return nil, nil
}
func (db *isolationDB) LoadRouteGroupSources(ctx context.Context, groupRouteIDs []int64) (map[int64][]int64, error) {
	return map[int64][]int64{}, nil
}
func (db *isolationDB) LoadRouteChannels(ctx context.Context, routeIDs []int64) ([]struct {
	Channel store.RouteChannel
	Account store.Account
	Site    store.Site
	Token   *store.AccountToken
}, error) {
	return nil, nil
}
func (db *isolationDB) LoadOAuthRouteUnitSummaries(ctx context.Context, unitIDs []int64) (map[int64]OAuthRouteUnitSummary, error) {
	return map[int64]OAuthRouteUnitSummary{}, nil
}
func (db *isolationDB) LoadOAuthRouteUnitMembers(ctx context.Context, unitIDs []int64) (map[int64][]OAuthRouteUnitMemberCandidate, error) {
	return map[int64][]OAuthRouteUnitMemberCandidate{}, nil
}
func (db *isolationDB) UpdateChannelLastSelectedAt(ctx context.Context, channelID int64, lastSelectedAt string) error {
	return nil
}
func (db *isolationDB) UpdateRouteUnitMemberLastSelectedAt(ctx context.Context, unitID, accountID int64, lastSelectedAt string) error {
	return nil
}
func (db *isolationDB) FindRouteIDsByOAuthRouteUnitID(ctx context.Context, unitID int64) ([]int64, error) {
	return nil, nil
}
func (db *isolationDB) LoadCredentialScopedChannelIDs(ctx context.Context, channel store.RouteChannel, accountID int64) ([]int64, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if ids, ok := db.credentialScope[channel.ID]; ok {
		out := make([]int64, len(ids))
		copy(out, ids)
		return out, nil
	}
	return []int64{channel.ID}, nil
}
func (db *isolationDB) LoadChannelWithAccount(ctx context.Context, channelID int64) (*struct {
	Channel store.RouteChannel
	Account store.Account
}, error) {
	row, err := db.LoadChannelWithAccountAndRoute(ctx, channelID)
	if err != nil || row == nil {
		return nil, err
	}
	return &struct {
		Channel store.RouteChannel
		Account store.Account
	}{Channel: row.Channel, Account: row.Account}, nil
}
func (db *isolationDB) LoadChannelWithAccountAndRoute(ctx context.Context, channelID int64) (*struct {
	Channel store.RouteChannel
	Account store.Account
	Route   store.TokenRoute
}, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	row, ok := db.channels[channelID]
	if !ok {
		return nil, nil
	}
	cp := *row
	return &cp, nil
}
func (db *isolationDB) UpdateChannelCooldownFields(ctx context.Context, channelIDs []int64, updates map[string]interface{}) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.cooldownCalls++
	db.lastCooldownIDs = append([]int64(nil), channelIDs...)
	db.lastCooldownUpdates = updates

	for _, id := range channelIDs {
		row, ok := db.channels[id]
		if !ok {
			continue
		}
		if v, ok := updates["failCount"].(int64); ok {
			row.Channel.FailCount = v
		}
		if raw, exists := updates["lastFailAt"]; exists {
			switch v := raw.(type) {
			case nil:
				row.Channel.LastFailAt = nil
			case *string:
				row.Channel.LastFailAt = v
			case string:
				s := v
				row.Channel.LastFailAt = &s
			}
		}
		if v, ok := updates["consecutiveFailCount"].(int64); ok {
			row.Channel.ConsecutiveFailCount = v
		}
		if v, ok := updates["cooldownLevel"].(int64); ok {
			row.Channel.CooldownLevel = v
		}
		if raw, exists := updates["cooldownUntil"]; exists {
			switch v := raw.(type) {
			case nil:
				row.Channel.CooldownUntil = nil
			case *string:
				row.Channel.CooldownUntil = v
			case string:
				s := v
				row.Channel.CooldownUntil = &s
			}
		}
	}
	return nil
}
func (db *isolationDB) UpdateChannelSuccessFields(ctx context.Context, channelID int64, updates map[string]interface{}) error {
	return nil
}
func (db *isolationDB) UpdateRouteUnitMemberCooldownFields(ctx context.Context, memberID int64, updates map[string]interface{}) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.memberCalls++
	db.lastMemberID = memberID
	db.lastMemberUpdates = updates
	for _, row := range db.members {
		if row.Member.ID != memberID {
			continue
		}
		if v, ok := updates["failCount"].(int64); ok {
			row.Member.FailCount = v
		}
		if v, ok := updates["lastFailAt"].(string); ok {
			s := v
			row.Member.LastFailAt = &s
		} else if v, ok := updates["lastFailAt"].(*string); ok {
			row.Member.LastFailAt = v
		}
		if v, ok := updates["consecutiveFailCount"].(int64); ok {
			row.Member.ConsecutiveFailCount = v
		}
		if v, ok := updates["cooldownLevel"].(int64); ok {
			row.Member.CooldownLevel = v
		}
		if v, ok := updates["cooldownUntil"].(*string); ok {
			row.Member.CooldownUntil = v
		}
	}
	return nil
}
func (db *isolationDB) UpdateRouteUnitMemberSuccessFields(ctx context.Context, memberID int64, updates map[string]interface{}) error {
	return nil
}
func (db *isolationDB) LoadRouteUnitMemberWithAccount(ctx context.Context, unitID, accountID int64) (*struct {
	Member  store.OAuthRouteUnitMember
	Account store.Account
	Unit    store.OAuthRouteUnit
}, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	row, ok := db.members[memberKey(unitID, accountID)]
	if !ok {
		return nil, nil
	}
	cp := *row
	return &cp, nil
}
func (db *isolationDB) FindAllEnabledRoutes(ctx context.Context) ([]store.TokenRoute, error) {
	return nil, nil
}
func (db *isolationDB) LoadChannelsByTokenID(ctx context.Context, tokenID int64) ([]store.RouteChannel, error) {
	return nil, nil
}
func (db *isolationDB) LoadChannelsByAccountIDWithoutToken(ctx context.Context, accountID int64) ([]store.RouteChannel, error) {
	return nil, nil
}
func (db *isolationDB) LoadRuntimeHealthChannelRows(ctx context.Context, channelIDs []int64) ([]struct {
	SiteID            int64
	SourceModel       *string
	RouteModelPattern string
}, error) {
	return nil, nil
}
func (db *isolationDB) ClearChannelFailureStates(ctx context.Context, channelIDs []int64) error {
	return nil
}

func newIsolationRouter(db *isolationDB) *TokenRouter {
	cache := NewRouteCache(60_000)
	return &TokenRouter{
		db:               db,
		cache:            cache,
		configuredMaxSec: 3600,
	}
}

func isolationAccount(id, siteID int64) store.Account {
	token := "tok-" + formatInt(id)
	return store.Account{
		ID:          id,
		SiteID:      siteID,
		AccessToken: token,
		APIToken:    &token,
		Status:      "active",
		Balance:     100,
		Quota:       1000,
		ValueScore:  1,
	}
}

func isolationRoute(id int64, strategy string) store.TokenRoute {
	return store.TokenRoute{
		ID:              id,
		ModelPattern:    "gpt-test",
		RouteMode:       "pattern",
		RoutingStrategy: strategy,
		Enabled:         true,
	}
}

func isolationChannel(id, routeID, accountID int64) store.RouteChannel {
	model := "gpt-test"
	return store.RouteChannel{
		ID:          id,
		RouteID:     routeID,
		AccountID:   accountID,
		SourceModel: &model,
		Priority:    0,
		Weight:      10,
		Enabled:     true,
	}
}

// TestRecordFailure_DoesNotCascadeToSiblingChannels proves a single non-usage-limit
// failure only mutates the failed channel's cooldown/failCount fields.
func TestRecordFailure_DoesNotCascadeToSiblingChannels(t *testing.T) {
	ResetSiteRuntimeHealthState()
	// Mark health as loaded so EnsureSiteRuntimeHealthStateLoaded is a no-op
	// without a settings store (persist is also no-op when store is nil).
	siteRuntimeHealthLoaded = true
	t.Cleanup(ResetSiteRuntimeHealthState)

	db := newIsolationDB()
	route := isolationRoute(1, "weighted")

	// Site A: two sibling channels (same site, different accounts)
	// Site B: unrelated healthy channel
	chA1 := isolationChannel(101, 1, 1001)
	chA2 := isolationChannel(102, 1, 1002)
	chB1 := isolationChannel(201, 1, 2001)
	accA1 := isolationAccount(1001, 10)
	accA2 := isolationAccount(1002, 10)
	accB1 := isolationAccount(2001, 20)

	db.seedChannel(chA1, accA1, route)
	db.seedChannel(chA2, accA2, route)
	db.seedChannel(chB1, accB1, route)

	tr := newIsolationRouter(db)
	ctx := context.Background()
	status := 500
	errText := "bad gateway"
	model := "gpt-test"

	if err := tr.RecordFailure(ctx, 101, SiteRuntimeFailureContext{
		Status:    &status,
		ErrorText: &errText,
		ModelName: &model,
	}, nil); err != nil {
		t.Fatalf("RecordFailure: %v", err)
	}

	if db.cooldownCalls != 1 {
		t.Fatalf("expected 1 cooldown update, got %d", db.cooldownCalls)
	}
	if len(db.lastCooldownIDs) != 1 || db.lastCooldownIDs[0] != 101 {
		t.Fatalf("expected cooldown scoped to channel 101 only, got %v", db.lastCooldownIDs)
	}

	failed := db.getChannel(101)
	sibling := db.getChannel(102)
	otherSite := db.getChannel(201)
	if failed == nil || sibling == nil || otherSite == nil {
		t.Fatal("expected all seeded channels present")
	}

	if failed.FailCount != 1 {
		t.Errorf("failed channel failCount=%d, want 1", failed.FailCount)
	}
	if failed.LastFailAt == nil || *failed.LastFailAt == "" {
		t.Error("failed channel lastFailAt should be set")
	}
	if failed.CooldownUntil == nil || *failed.CooldownUntil == "" {
		t.Error("failed channel cooldownUntil should be set (fibonacci path)")
	}

	if sibling.FailCount != 0 {
		t.Errorf("sibling channel failCount cascaded: got %d, want 0", sibling.FailCount)
	}
	if sibling.LastFailAt != nil {
		t.Errorf("sibling lastFailAt cascaded: %v", sibling.LastFailAt)
	}
	if sibling.CooldownUntil != nil {
		t.Errorf("sibling cooldownUntil cascaded: %v", sibling.CooldownUntil)
	}
	if sibling.ConsecutiveFailCount != 0 || sibling.CooldownLevel != 0 {
		t.Errorf("sibling consecutive/cooldown level cascaded: cf=%d level=%d",
			sibling.ConsecutiveFailCount, sibling.CooldownLevel)
	}

	if otherSite.FailCount != 0 || otherSite.LastFailAt != nil || otherSite.CooldownUntil != nil {
		t.Errorf("unrelated site channel poisoned: failCount=%d lastFailAt=%v cooldownUntil=%v",
			otherSite.FailCount, otherSite.LastFailAt, otherSite.CooldownUntil)
	}

	// Site runtime health records only the failed site — not the unrelated site.
	if !IsSiteRuntimeBreakerOpen(10) {
		// One failure is not enough to open breaker; just check state exists for site 10
		// and site 20 remains untouched.
	}
	if IsSiteRuntimeBreakerOpen(20) {
		t.Error("unrelated site breaker should remain closed")
	}
	detailsB := GetSiteRuntimeHealthDetails(20, model)
	if detailsB.GlobalBreakerOpen || detailsB.ModelBreakerOpen {
		t.Error("unrelated site/model breaker should stay closed after site A failure")
	}
	// Site A should have recorded a failure (penalty/recent counts) but not open breaker yet.
	stateA := siteRuntimeHealthStates[10]
	if stateA == nil || stateA.RecentFailureCount < 1 {
		t.Error("expected site A runtime health to record the failure")
	}
	stateB := siteRuntimeHealthStates[20]
	if stateB != nil && stateB.RecentFailureCount > 0 {
		t.Error("expected site B runtime health to stay clean")
	}
}

// TestRecordFailure_UsageLimitScopesCredentialSiblingsOnly proves short-window
// usage-limit cooldowns credential-scoped siblings, not whole-route/site peers.
func TestRecordFailure_UsageLimitScopesCredentialSiblingsOnly(t *testing.T) {
	ResetSiteRuntimeHealthState()
	siteRuntimeHealthLoaded = true
	t.Cleanup(ResetSiteRuntimeHealthState)

	db := newIsolationDB()
	route := isolationRoute(1, "weighted")

	// Same credential: channels 101 + 102 share token/account scope
	// Different credential on same site: 103
	// Other site: 201
	ch101 := isolationChannel(101, 1, 1001)
	ch102 := isolationChannel(102, 1, 1001)
	ch103 := isolationChannel(103, 1, 1002)
	ch201 := isolationChannel(201, 1, 2001)

	accSame := isolationAccount(1001, 10)
	accOther := isolationAccount(1002, 10)
	accSiteB := isolationAccount(2001, 20)

	db.seedChannel(ch101, accSame, route)
	db.seedChannel(ch102, accSame, route)
	db.seedChannel(ch103, accOther, route)
	db.seedChannel(ch201, accSiteB, route)
	db.credentialScope[101] = []int64{101, 102}

	tr := newIsolationRouter(db)
	status := 429
	errText := "usage_limit_reached"
	model := "gpt-test"

	if err := tr.RecordFailure(context.Background(), 101, SiteRuntimeFailureContext{
		Status:    &status,
		ErrorText: &errText,
		ModelName: &model,
	}, nil); err != nil {
		t.Fatalf("RecordFailure: %v", err)
	}

	if len(db.lastCooldownIDs) != 2 {
		t.Fatalf("expected credential-scoped IDs [101,102], got %v", db.lastCooldownIDs)
	}
	seen := map[int64]bool{}
	for _, id := range db.lastCooldownIDs {
		seen[id] = true
	}
	if !seen[101] || !seen[102] {
		t.Fatalf("expected both 101 and 102 in scope, got %v", db.lastCooldownIDs)
	}
	if seen[103] || seen[201] {
		t.Fatalf("usage-limit must not cascade beyond credential scope, got %v", db.lastCooldownIDs)
	}

	if c := db.getChannel(103); c.FailCount != 0 || c.CooldownUntil != nil {
		t.Errorf("same-site different credential poisoned: failCount=%d cooldown=%v", c.FailCount, c.CooldownUntil)
	}
	if c := db.getChannel(201); c.FailCount != 0 || c.CooldownUntil != nil {
		t.Errorf("other-site channel poisoned: failCount=%d cooldown=%v", c.FailCount, c.CooldownUntil)
	}
	// Credential siblings should carry short-window cooldown
	for _, id := range []int64{101, 102} {
		c := db.getChannel(id)
		if c.CooldownUntil == nil {
			t.Errorf("channel %d missing short-window cooldown", id)
		}
		// short-window path resets failCount to 0 by design
		if c.FailCount != 0 {
			t.Errorf("channel %d failCount=%d want 0 under short-window path", id, c.FailCount)
		}
	}
}

// TestRecordFailure_OAuthMemberIsolation proves member failure does not write
// channel-level cooldown on sibling members or regular channels.
func TestRecordFailure_OAuthMemberIsolation(t *testing.T) {
	ResetSiteRuntimeHealthState()
	siteRuntimeHealthLoaded = true
	t.Cleanup(ResetSiteRuntimeHealthState)

	db := newIsolationDB()
	route := isolationRoute(1, "weighted")
	unitID := int64(7)
	ch := isolationChannel(301, 1, 3001)
	ch.OAuthRouteUnitID = &unitID
	acc1 := isolationAccount(3001, 30)
	acc2 := isolationAccount(3002, 30)
	db.seedChannel(ch, acc1, route)

	unit := store.OAuthRouteUnit{
		ID: unitID, SiteID: 30, Provider: "codex", Name: "unit", Strategy: "round_robin", Enabled: true,
	}
	db.seedMember(store.OAuthRouteUnitMember{ID: 1, UnitID: unitID, AccountID: 3001}, acc1, unit)
	db.seedMember(store.OAuthRouteUnitMember{ID: 2, UnitID: unitID, AccountID: 3002}, acc2, unit)

	// Sibling regular channel on same site
	sibling := isolationChannel(302, 1, 3003)
	db.seedChannel(sibling, isolationAccount(3003, 30), route)

	tr := newIsolationRouter(db)
	status := 502
	errText := "bad gateway"

	if err := tr.RecordFailure(context.Background(), 301, SiteRuntimeFailureContext{
		Status:    &status,
		ErrorText: &errText,
	}, nil); err != nil {
		t.Fatalf("RecordFailure: %v", err)
	}

	if db.memberCalls != 1 {
		t.Fatalf("expected 1 member cooldown write, got %d", db.memberCalls)
	}
	if db.lastMemberID != 1 {
		t.Fatalf("expected member 1 updated, got %d", db.lastMemberID)
	}
	if db.cooldownCalls != 0 {
		t.Fatalf("oauth member path must not update channel cooldown fields, calls=%d ids=%v",
			db.cooldownCalls, db.lastCooldownIDs)
	}

	// Sibling regular channel untouched
	if c := db.getChannel(302); c.FailCount != 0 || c.CooldownUntil != nil {
		t.Errorf("sibling regular channel cascaded: failCount=%d cooldown=%v", c.FailCount, c.CooldownUntil)
	}
	// Outer OAuth channel itself not mutated (member path only)
	if c := db.getChannel(301); c.FailCount != 0 || c.CooldownUntil != nil {
		t.Errorf("outer oauth channel fields mutated: failCount=%d cooldown=%v", c.FailCount, c.CooldownUntil)
	}
}

// TestSelectionFilter_PrefersHealthySiblingAfterOneFailure is table-driven coverage
// for weighted / stable_first / round_robin soft-filters after one channel fails.
func TestSelectionFilter_PrefersHealthySiblingAfterOneFailure(t *testing.T) {
	nowMs := time.Now().UnixMilli()
	recentISO := time.UnixMilli(nowMs - 2_000).UTC().Format(time.RFC3339)
	model := "gpt-test"

	type cand struct {
		id        int64
		siteID    int64
		failCount int64
		lastFail  string
	}
	toCandidates := func(rows []cand) []RouteChannelCandidate {
		out := make([]RouteChannelCandidate, 0, len(rows))
		for _, r := range rows {
			c := buildTestCandidate(r.id, r.siteID, r.id*10, 10, 0, 100, r.failCount, 50.0, 1.0, nil, 0, &model)
			if r.lastFail != "" {
				lf := r.lastFail
				c.Channel.LastFailAt = &lf
			}
			out = append(out, c)
		}
		return out
	}
	getInfo := func(c RouteChannelCandidate) (*int64, *string) {
		return &c.Channel.FailCount, c.Channel.LastFailAt
	}

	tests := []struct {
		name            string
		strategy        RouteRoutingStrategy
		rows            []cand
		wantIDs         []int64
		wantFallbackAll bool
	}{
		{
			name:     "weighted prefers healthy sibling",
			strategy: StrategyWeighted,
			rows: []cand{
				{id: 1, siteID: 10, failCount: 1, lastFail: recentISO},
				{id: 2, siteID: 20, failCount: 0},
			},
			wantIDs: []int64{2},
		},
		{
			name:     "round_robin prefers healthy sibling",
			strategy: StrategyRoundRobin,
			rows: []cand{
				{id: 1, siteID: 10, failCount: 1, lastFail: recentISO},
				{id: 2, siteID: 20, failCount: 0},
			},
			wantIDs: []int64{2},
		},
		{
			name:     "stable_first prefers healthy sibling",
			strategy: StrategyStableFirst,
			rows: []cand{
				{id: 1, siteID: 10, failCount: 2, lastFail: recentISO},
				{id: 2, siteID: 20, failCount: 0},
				{id: 3, siteID: 30, failCount: 0},
			},
			wantIDs: []int64{2, 3},
		},
		{
			name:     "same-site sibling still healthy after peer fail",
			strategy: StrategyWeighted,
			rows: []cand{
				{id: 1, siteID: 10, failCount: 1, lastFail: recentISO},
				{id: 2, siteID: 10, failCount: 0}, // same site, not marked failed
			},
			wantIDs: []int64{2},
		},
		{
			name:     "all recently failed falls back to full set",
			strategy: StrategyRoundRobin,
			rows: []cand{
				{id: 1, siteID: 10, failCount: 1, lastFail: recentISO},
				{id: 2, siteID: 20, failCount: 3, lastFail: recentISO},
			},
			wantIDs:         []int64{1, 2},
			wantFallbackAll: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ResetSiteRuntimeHealthState()
			candidates := toCandidates(tt.rows)

			// Mimic strategy selection filter stack: breaker then recent-failure
			// (RR now uses the same recent-failure filter as weighted/stable_first).
			breakerHealthy, _ := GetBreakerFilteredCandidatesByModel(candidates, model)
			filtered := FilterRecentlyFailedCandidates(breakerHealthy, getInfo, nowMs, 0)

			got := map[int64]bool{}
			for _, c := range filtered {
				got[c.Channel.ID] = true
			}
			if len(filtered) != len(tt.wantIDs) {
				t.Fatalf("filtered len=%d want %d (ids=%v)", len(filtered), len(tt.wantIDs), keysOf(got))
			}
			for _, id := range tt.wantIDs {
				if !got[id] {
					t.Errorf("expected channel %d in filtered set, got %v", id, keysOf(got))
				}
			}
			if !tt.wantFallbackAll {
				// Failed channel must not remain when healthy sibling exists
				for _, r := range tt.rows {
					if r.failCount > 0 && r.lastFail != "" && !got[r.id] {
						// expected excluded
						continue
					}
					if r.failCount > 0 && r.lastFail != "" && got[r.id] && len(tt.wantIDs) < len(tt.rows) {
						t.Errorf("recently failed channel %d still present with healthy alternatives", r.id)
					}
				}
			}

			// Round-robin selection must pick a healthy channel when available
			if tt.strategy == StrategyRoundRobin && !tt.wantFallbackAll {
				selected := SelectRoundRobinCandidate(filtered)
				if selected == nil {
					t.Fatal("expected RR selection")
				}
				if selected.Channel.FailCount > 0 {
					t.Errorf("RR selected recently-failed channel %d", selected.Channel.ID)
				}
			}
		})
	}
}

// TestSiteBreaker_DoesNotOpenOnSingleFailureAndDoesNotPoisonOtherSites verifies
// residual cascade boundaries: one failure must not open a site breaker or affect
// other sites; three transient failures open only the failed site.
func TestSiteBreaker_DoesNotOpenOnSingleFailureAndDoesNotPoisonOtherSites(t *testing.T) {
	ResetSiteRuntimeHealthState()
	t.Cleanup(ResetSiteRuntimeHealthState)

	status := 500
	model := "gpt-test"
	ctx := SiteRuntimeFailureContext{Status: &status, ModelName: &model}

	// Single failure: no breaker
	RecordSiteRuntimeFailure(10, ctx)
	if IsSiteRuntimeBreakerOpen(10) {
		t.Error("single transient failure must not open site breaker")
	}
	if IsSiteRuntimeBreakerOpen(20) {
		t.Error("unrelated site must stay closed")
	}

	// Two more on site 10 → streak 3 opens breaker on site 10 only
	RecordSiteRuntimeFailure(10, ctx)
	RecordSiteRuntimeFailure(10, ctx)
	if !IsSiteRuntimeBreakerOpen(10) {
		t.Error("expected site 10 breaker open after 3 transient failures")
	}
	if IsSiteRuntimeBreakerOpen(20) {
		t.Error("site 20 must not cascade-open from site 10 breaker")
	}

	// Model-level: only the named model on the failed site is broken
	detailsSameModel := GetSiteRuntimeHealthDetails(10, model)
	if !detailsSameModel.GlobalBreakerOpen {
		t.Error("expected global breaker open on failed site")
	}
	detailsOtherSite := GetSiteRuntimeHealthDetails(20, model)
	if detailsOtherSite.GlobalBreakerOpen || detailsOtherSite.ModelBreakerOpen {
		t.Error("other site health must remain clean")
	}

	// Filter must keep other-site channels when only site 10 is broken
	candidates := []RouteChannelCandidate{
		buildTestCandidate(1, 10, 101, 10, 0, 100, 0, 50.0, 1.0, nil, 0, &model),
		buildTestCandidate(2, 20, 201, 10, 0, 100, 0, 50.0, 1.0, nil, 0, &model),
	}
	healthy, avoided := GetBreakerFilteredCandidatesByModel(candidates, model)
	if len(healthy) != 1 || healthy[0].Channel.ID != 2 {
		t.Fatalf("expected only site-20 channel healthy, got %v", healthyIDs(healthy))
	}
	if len(avoided) != 1 || avoided[0].Candidate.Channel.ID != 1 {
		t.Fatalf("expected site-10 channel avoided, got avoided=%d", len(avoided))
	}
}

// TestRoundRobinFilterStack_MatchesWeightedRecentFailurePolicy is a regression for
// the R1 fix: RR must apply FilterRecentlyFailedCandidates, not only breaker filter.
func TestRoundRobinFilterStack_MatchesWeightedRecentFailurePolicy(t *testing.T) {
	ResetSiteRuntimeHealthState()
	t.Cleanup(ResetSiteRuntimeHealthState)

	nowMs := time.Now().UnixMilli()
	recentISO := time.UnixMilli(nowMs - 1_000).UTC().Format(time.RFC3339)
	model := "gpt-test"

	failed := buildTestCandidate(1, 10, 101, 10, 0, 100, 1, 50.0, 1.0, nil, 0, &model)
	failed.Channel.LastFailAt = &recentISO
	healthy := buildTestCandidate(2, 20, 201, 10, 0, 100, 0, 50.0, 1.0, nil, 0, &model)
	// Make failed channel "earlier" in RR order so without the filter it would win.
	old := time.UnixMilli(nowMs - 86_400_000).UTC().Format(time.RFC3339)
	failed.Channel.LastSelectedAt = &old
	recentSel := time.UnixMilli(nowMs - 1_000).UTC().Format(time.RFC3339)
	healthy.Channel.LastSelectedAt = &recentSel

	candidates := []RouteChannelCandidate{failed, healthy}
	breakerHealthy, _ := GetBreakerFilteredCandidatesByModel(candidates, model)
	filtered := FilterRecentlyFailedCandidates(breakerHealthy,
		func(c RouteChannelCandidate) (*int64, *string) { return &c.Channel.FailCount, c.Channel.LastFailAt },
		nowMs, 0)

	selected := SelectRoundRobinCandidate(filtered)
	if selected == nil {
		t.Fatal("expected selection")
	}
	if selected.Channel.ID != 2 {
		t.Fatalf("RR with recent-failure filter selected channel %d, want healthy sibling 2", selected.Channel.ID)
	}

	// Without recent-failure filter, RR would prefer the older lastSelectedAt (failed ch)
	unfiltered := SelectRoundRobinCandidate(breakerHealthy)
	if unfiltered == nil || unfiltered.Channel.ID != 1 {
		t.Fatalf("sanity: unfiltered RR should prefer failed channel 1 by lastSelectedAt, got %v",
			channelIDOrNil(unfiltered))
	}
}

func keysOf(m map[int64]bool) []int64 {
	out := make([]int64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func healthyIDs(cs []RouteChannelCandidate) []int64 {
	out := make([]int64, len(cs))
	for i, c := range cs {
		out[i] = c.Channel.ID
	}
	return out
}

func channelIDOrNil(c *RouteChannelCandidate) interface{} {
	if c == nil {
		return nil
	}
	return c.Channel.ID
}

// TestWeightedSoftFilter_EmptyPriorityDemotesToNext covers #358:
// when every priority-0 candidate is soft-unhealthy (recent-fail / breaker),
// weighted selection must try the next priority instead of pinning on the
// unfiltered full priority-0 layer. Healthy priority-1 wins.
func TestWeightedSoftFilter_EmptyPriorityDemotesToNext(t *testing.T) {
	ResetSiteRuntimeHealthState()
	siteRuntimeHealthLoaded = true
	t.Cleanup(ResetSiteRuntimeHealthState)

	nowMs := time.Now().UnixMilli()
	recentISO := time.UnixMilli(nowMs - 2_000).UTC().Format(time.RFC3339)
	model := "gpt-test"
	resolve := staticModel(model)

	// Priority 0: two recently-failed channels (would previously pin via full-set fallback)
	c0a := buildTestCandidate(1, 10, 101, 10, 0, 100, 1, 50.0, 1.0, nil, 50.0, &model)
	c0a.Channel.FailCount = 2
	c0a.Channel.LastFailAt = &recentISO
	c0b := buildTestCandidate(2, 11, 102, 10, 0, 100, 1, 50.0, 1.0, nil, 50.0, &model)
	c0b.Channel.FailCount = 3
	c0b.Channel.LastFailAt = &recentISO
	// Priority 1: healthy channel
	c1 := buildTestCandidate(3, 20, 201, 10, 1, 100, 0, 50.0, 1.0, nil, 50.0, &model)

	available := []RouteChannelCandidate{c0a, c0b, c1}

	// Strict filter on prio 0 alone must be empty (no full-set pin).
	strict0 := softFilterCandidatesStrict([]RouteChannelCandidate{c0a, c0b}, resolve, nowMs, 3600)
	if len(strict0) != 0 {
		t.Fatalf("expected strict soft-filter of failed prio-0 layer to be empty, got %d", len(strict0))
	}
	// Legacy full-set filter would return both failed channels.
	legacy0 := FilterRecentlyFailedCandidates([]RouteChannelCandidate{c0a, c0b},
		func(c RouteChannelCandidate) (*int64, *string) { return &c.Channel.FailCount, c.Channel.LastFailAt },
		nowMs, 3600)
	if len(legacy0) != 2 {
		t.Fatalf("expected legacy filter full-set fallback size 2, got %d", len(legacy0))
	}

	selected := selectWeightedAcrossPriorityLayers(available, resolve, nowMs, 3600,
		func(pool []RouteChannelCandidate) *RouteChannelCandidate {
			if len(pool) == 0 {
				return nil
			}
			// Deterministic pick: lowest channel ID (avoids rand flakiness).
			best := &pool[0]
			for i := range pool {
				if pool[i].Channel.ID < best.Channel.ID {
					best = &pool[i]
				}
			}
			return best
		})
	if selected == nil {
		t.Fatal("expected selection from healthy priority-1 layer")
	}
	if selected.Channel.ID != 3 {
		t.Fatalf("expected priority-1 channel 3, got channel %d priority %d",
			selected.Channel.ID, selected.Channel.Priority)
	}
	if selected.Channel.Priority != 1 {
		t.Fatalf("expected priority 1, got %d", selected.Channel.Priority)
	}
}

// TestWeightedSoftFilter_AllLayersSoftEmptyAllowsGlobalFallback covers #358 AC2:
// when every priority layer is soft-empty, selection still returns a candidate
// via global full-set fallback instead of hard-failing.
func TestWeightedSoftFilter_AllLayersSoftEmptyAllowsGlobalFallback(t *testing.T) {
	ResetSiteRuntimeHealthState()
	siteRuntimeHealthLoaded = true
	t.Cleanup(ResetSiteRuntimeHealthState)

	nowMs := time.Now().UnixMilli()
	recentISO := time.UnixMilli(nowMs - 1_000).UTC().Format(time.RFC3339)
	model := "gpt-test"
	resolve := staticModel(model)

	c0 := buildTestCandidate(1, 10, 101, 10, 0, 100, 1, 50.0, 1.0, nil, 50.0, &model)
	c0.Channel.FailCount = 2
	c0.Channel.LastFailAt = &recentISO
	c1 := buildTestCandidate(2, 20, 201, 10, 1, 100, 1, 50.0, 1.0, nil, 50.0, &model)
	c1.Channel.FailCount = 2
	c1.Channel.LastFailAt = &recentISO

	selected := selectWeightedAcrossPriorityLayers([]RouteChannelCandidate{c0, c1}, resolve, nowMs, 3600,
		func(pool []RouteChannelCandidate) *RouteChannelCandidate {
			if len(pool) == 0 {
				return nil
			}
			return &pool[0]
		})
	if selected == nil {
		t.Fatal("expected global fallback selection when all layers soft-empty")
	}
	if selected.Channel.ID != 1 && selected.Channel.ID != 2 {
		t.Fatalf("unexpected selected channel %d", selected.Channel.ID)
	}
}

// TestRoundRobinSoftFilter_EmptyPriorityDemotesToNext covers #368:
// when every priority-0 candidate is soft-unhealthy, round_robin must try the
// next priority (strict soft-filter demotion) instead of pinning via the
// global FilterRecentlyFailedCandidates full-set fallback. Healthy prio-1 wins.
func TestRoundRobinSoftFilter_EmptyPriorityDemotesToNext(t *testing.T) {
	ResetSiteRuntimeHealthState()
	siteRuntimeHealthLoaded = true
	t.Cleanup(ResetSiteRuntimeHealthState)

	nowMs := time.Now().UnixMilli()
	recentISO := time.UnixMilli(nowMs - 2_000).UTC().Format(time.RFC3339)
	model := "gpt-test"
	resolve := staticModel(model)

	// Priority 0: recently-failed channel (would previously pin via full-set fallback
	// when RR applied FilterRecentlyFailedCandidates to the whole available set).
	c0 := buildTestCandidate(1, 10, 101, 10, 0, 100, 1, 50.0, 1.0, nil, 50.0, &model)
	c0.Channel.FailCount = 2
	c0.Channel.LastFailAt = &recentISO
	// Make prio-0 "earlier" in RR order so without demotion it would win.
	old := time.UnixMilli(nowMs - 86_400_000).UTC().Format(time.RFC3339)
	c0.Channel.LastSelectedAt = &old

	// Priority 1: healthy channel
	c1 := buildTestCandidate(2, 20, 201, 10, 1, 100, 0, 50.0, 1.0, nil, 50.0, &model)
	recentSel := time.UnixMilli(nowMs - 1_000).UTC().Format(time.RFC3339)
	c1.Channel.LastSelectedAt = &recentSel

	available := []RouteChannelCandidate{c0, c1}

	strict0 := softFilterCandidatesStrict([]RouteChannelCandidate{c0}, resolve, nowMs, 3600)
	if len(strict0) != 0 {
		t.Fatalf("expected strict soft-filter of failed prio-0 layer to be empty, got %d", len(strict0))
	}

	// Legacy FilterRecentlyFailedCandidates on prio-0 alone would return c0 via full-set fallback.
	legacy0 := FilterRecentlyFailedCandidates([]RouteChannelCandidate{c0},
		func(c RouteChannelCandidate) (*int64, *string) { return &c.Channel.FailCount, c.Channel.LastFailAt },
		nowMs, 3600)
	if len(legacy0) != 1 || legacy0[0].Channel.ID != 1 {
		t.Fatalf("expected legacy filter full-set fallback to pin prio-0 channel 1, got %v", healthyIDs(legacy0))
	}

	selected := selectAcrossPriorityLayers(available, resolve, nowMs, 3600,
		func(pool []RouteChannelCandidate) *RouteChannelCandidate {
			return SelectRoundRobinCandidate(pool)
		})
	if selected == nil {
		t.Fatal("expected selection from healthy priority-1 layer")
	}
	if selected.Channel.ID != 2 {
		t.Fatalf("expected priority-1 channel 2, got channel %d priority %d",
			selected.Channel.ID, selected.Channel.Priority)
	}
	if selected.Channel.Priority != 1 {
		t.Fatalf("expected priority 1, got %d", selected.Channel.Priority)
	}

	// Alias used by #358 tests must share the same walk.
	selectedAlias := selectWeightedAcrossPriorityLayers(available, resolve, nowMs, 3600,
		func(pool []RouteChannelCandidate) *RouteChannelCandidate {
			return SelectRoundRobinCandidate(pool)
		})
	if selectedAlias == nil || selectedAlias.Channel.ID != 2 {
		t.Fatalf("alias walk expected channel 2, got %v", channelIDOrNil(selectedAlias))
	}
}

// TestStableFirstSoftFilter_EmptyPriorityDemotesToNext covers #368:
// stable_first must demote a soft-failed prio-0 channel to healthy prio-1 via
// priority-layer strict soft-filter walk (parity with weighted #358 / RR above).
func TestStableFirstSoftFilter_EmptyPriorityDemotesToNext(t *testing.T) {
	ResetSiteRuntimeHealthState()
	siteRuntimeHealthLoaded = true
	t.Cleanup(ResetSiteRuntimeHealthState)

	nowMs := time.Now().UnixMilli()
	recentISO := time.UnixMilli(nowMs - 2_000).UTC().Format(time.RFC3339)
	model := "gpt-test"
	resolve := staticModel(model)

	// Priority 0: soft-unhealthy (recent fail). Give it high success history so that
	// if it leaked into the pool plan it would be primary material.
	c0 := buildTestCandidate(1, 10, 101, 10, 0, 100, 1, 50.0, 1.0, nil, 50.0, &model)
	c0.Channel.FailCount = 2
	c0.Channel.LastFailAt = &recentISO
	c0.Channel.SuccessCount = 50

	// Priority 1: healthy channel with enough history for primary pool.
	c1 := buildTestCandidate(2, 20, 201, 10, 1, 100, 0, 50.0, 1.0, nil, 50.0, &model)
	c1.Channel.SuccessCount = 50

	available := []RouteChannelCandidate{c0, c1}

	strict0 := softFilterCandidatesStrict([]RouteChannelCandidate{c0}, resolve, nowMs, 3600)
	if len(strict0) != 0 {
		t.Fatalf("expected strict soft-filter of failed prio-0 layer to be empty, got %d", len(strict0))
	}

	selected := selectAcrossPriorityLayers(available, resolve, nowMs, 3600,
		func(pool []RouteChannelCandidate) *RouteChannelCandidate {
			// Mirror selectFromMatch stable_first branch: plan primary/observation
			// only on the soft-healthy layer, then pick a deterministic candidate.
			poolPlan := BuildStableFirstPoolPlan(pool, resolve)
			selectionPool := poolPlan.PrimaryCandidates
			if len(selectionPool) == 0 {
				selectionPool = poolPlan.ObservationCandidates
			}
			if len(selectionPool) == 0 {
				// Fall back to the soft-healthy layer itself if pool plan is empty
				// (e.g. untrusted sites still need demotion coverage).
				if len(pool) == 0 {
					return nil
				}
				best := &pool[0]
				for i := range pool {
					if pool[i].Channel.ID < best.Channel.ID {
						best = &pool[i]
					}
				}
				return best
			}
			best := &selectionPool[0]
			for i := range selectionPool {
				if selectionPool[i].Channel.ID < best.Channel.ID {
					best = &selectionPool[i]
				}
			}
			return best
		})
	if selected == nil {
		t.Fatal("expected selection from healthy priority-1 layer")
	}
	if selected.Channel.ID != 2 {
		t.Fatalf("expected priority-1 channel 2, got channel %d priority %d",
			selected.Channel.ID, selected.Channel.Priority)
	}
	if selected.Channel.Priority != 1 {
		t.Fatalf("expected priority 1, got %d", selected.Channel.Priority)
	}
}

// TestRoundRobinAndStableFirstSoftFilter_AllLayersSoftEmptyAllowsGlobalFallback
// covers #368 honesty: when every priority layer is soft-empty, RR and
// stable_first still return a candidate via the shared global full-set fallback
// (same starvation guard as weighted #358).
func TestRoundRobinAndStableFirstSoftFilter_AllLayersSoftEmptyAllowsGlobalFallback(t *testing.T) {
	ResetSiteRuntimeHealthState()
	siteRuntimeHealthLoaded = true
	t.Cleanup(ResetSiteRuntimeHealthState)

	nowMs := time.Now().UnixMilli()
	recentISO := time.UnixMilli(nowMs - 1_000).UTC().Format(time.RFC3339)
	model := "gpt-test"
	resolve := staticModel(model)

	c0 := buildTestCandidate(1, 10, 101, 10, 0, 100, 1, 50.0, 1.0, nil, 50.0, &model)
	c0.Channel.FailCount = 2
	c0.Channel.LastFailAt = &recentISO
	c1 := buildTestCandidate(2, 20, 201, 10, 1, 100, 1, 50.0, 1.0, nil, 50.0, &model)
	c1.Channel.FailCount = 2
	c1.Channel.LastFailAt = &recentISO

	available := []RouteChannelCandidate{c0, c1}

	rr := selectAcrossPriorityLayers(available, resolve, nowMs, 3600,
		func(pool []RouteChannelCandidate) *RouteChannelCandidate {
			return SelectRoundRobinCandidate(pool)
		})
	if rr == nil {
		t.Fatal("expected RR global fallback selection when all layers soft-empty")
	}
	if rr.Channel.ID != 1 && rr.Channel.ID != 2 {
		t.Fatalf("unexpected RR selected channel %d", rr.Channel.ID)
	}

	sf := selectAcrossPriorityLayers(available, resolve, nowMs, 3600,
		func(pool []RouteChannelCandidate) *RouteChannelCandidate {
			if len(pool) == 0 {
				return nil
			}
			return &pool[0]
		})
	if sf == nil {
		t.Fatal("expected stable_first-style global fallback selection when all layers soft-empty")
	}
	if sf.Channel.ID != 1 && sf.Channel.ID != 2 {
		t.Fatalf("unexpected stable_first selected channel %d", sf.Channel.ID)
	}
}
