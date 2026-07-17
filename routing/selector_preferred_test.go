package routing

import (
	"context"
	"sync"
	"testing"

	"github.com/tokendancelab/metapi-go/store"
)

// preferredDB is a minimal ChannelSelectorDB for SelectPreferredChannel tests.
type preferredDB struct {
	mu       sync.Mutex
	routes   []store.TokenRoute
	joined   []struct {
		Channel store.RouteChannel
		Account store.Account
		Site    store.Site
		Token   *store.AccountToken
	}
}

func (db *preferredDB) LoadEnabledRoutes(ctx context.Context) ([]store.TokenRoute, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	out := make([]store.TokenRoute, len(db.routes))
	copy(out, db.routes)
	return out, nil
}
func (db *preferredDB) LoadRouteGroupSources(ctx context.Context, groupRouteIDs []int64) (map[int64][]int64, error) {
	return map[int64][]int64{}, nil
}
func (db *preferredDB) LoadRouteChannels(ctx context.Context, routeIDs []int64) ([]struct {
	Channel store.RouteChannel
	Account store.Account
	Site    store.Site
	Token   *store.AccountToken
}, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	out := make([]struct {
		Channel store.RouteChannel
		Account store.Account
		Site    store.Site
		Token   *store.AccountToken
	}, len(db.joined))
	copy(out, db.joined)
	return out, nil
}
func (db *preferredDB) LoadOAuthRouteUnitSummaries(ctx context.Context, unitIDs []int64) (map[int64]OAuthRouteUnitSummary, error) {
	return map[int64]OAuthRouteUnitSummary{}, nil
}
func (db *preferredDB) LoadOAuthRouteUnitMembers(ctx context.Context, unitIDs []int64) (map[int64][]OAuthRouteUnitMemberCandidate, error) {
	return map[int64][]OAuthRouteUnitMemberCandidate{}, nil
}
func (db *preferredDB) UpdateChannelLastSelectedAt(ctx context.Context, channelID int64, lastSelectedAt string) error {
	return nil
}
func (db *preferredDB) UpdateRouteUnitMemberLastSelectedAt(ctx context.Context, unitID, accountID int64, lastSelectedAt string) error {
	return nil
}
func (db *preferredDB) FindRouteIDsByOAuthRouteUnitID(ctx context.Context, unitID int64) ([]int64, error) {
	return nil, nil
}
func (db *preferredDB) LoadCredentialScopedChannelIDs(ctx context.Context, channel store.RouteChannel, accountID int64) ([]int64, error) {
	return []int64{channel.ID}, nil
}
func (db *preferredDB) LoadChannelWithAccount(ctx context.Context, channelID int64) (*struct {
	Channel store.RouteChannel
	Account store.Account
}, error) {
	return nil, nil
}
func (db *preferredDB) LoadChannelWithAccountAndRoute(ctx context.Context, channelID int64) (*struct {
	Channel store.RouteChannel
	Account store.Account
	Route   store.TokenRoute
}, error) {
	return nil, nil
}
func (db *preferredDB) UpdateChannelCooldownFields(ctx context.Context, channelIDs []int64, updates map[string]interface{}) error {
	return nil
}
func (db *preferredDB) UpdateChannelSuccessFields(ctx context.Context, channelID int64, updates map[string]interface{}) error {
	return nil
}
func (db *preferredDB) UpdateRouteUnitMemberCooldownFields(ctx context.Context, memberID int64, updates map[string]interface{}) error {
	return nil
}
func (db *preferredDB) UpdateRouteUnitMemberSuccessFields(ctx context.Context, memberID int64, updates map[string]interface{}) error {
	return nil
}
func (db *preferredDB) LoadRouteUnitMemberWithAccount(ctx context.Context, unitID, accountID int64) (*struct {
	Member  store.OAuthRouteUnitMember
	Account store.Account
	Unit    store.OAuthRouteUnit
}, error) {
	return nil, nil
}
func (db *preferredDB) FindAllEnabledRoutes(ctx context.Context) ([]store.TokenRoute, error) {
	return db.LoadEnabledRoutes(ctx)
}
func (db *preferredDB) LoadChannelsByTokenID(ctx context.Context, tokenID int64) ([]store.RouteChannel, error) {
	return nil, nil
}
func (db *preferredDB) LoadChannelsByAccountIDWithoutToken(ctx context.Context, accountID int64) ([]store.RouteChannel, error) {
	return nil, nil
}
func (db *preferredDB) LoadRuntimeHealthChannelRows(ctx context.Context, channelIDs []int64) ([]struct {
	SiteID            int64
	SourceModel       *string
	RouteModelPattern string
}, error) {
	return nil, nil
}
func (db *preferredDB) ClearChannelFailureStates(ctx context.Context, channelIDs []int64) error {
	return nil
}

func preferredEligibleJoined(channelID, siteID, accountID int64, model string) struct {
	Channel store.RouteChannel
	Account store.Account
	Site    store.Site
	Token   *store.AccountToken
} {
	m := model
	token := "tok-" + formatInt(accountID)
	return struct {
		Channel store.RouteChannel
		Account store.Account
		Site    store.Site
		Token   *store.AccountToken
	}{
		Channel: store.RouteChannel{
			ID:          channelID,
			RouteID:     1,
			AccountID:   accountID,
			SourceModel: &m,
			Priority:    0,
			Weight:      10,
			Enabled:     true,
		},
		Account: store.Account{
			ID:       accountID,
			SiteID:   siteID,
			Status:   "active",
			APIToken: &token,
			Balance:  100,
		},
		Site: store.Site{
			ID:     siteID,
			Status: "active",
		},
	}
}

func newPreferredSelector(db *preferredDB) *ChannelSelector {
	return NewChannelSelector(db, NewRouteCache(60_000), 3600, defaultRoutingWeights(), nil, 1.0, nil)
}

// TestSelectPreferredChannel_OpenBreakerReturnsNil proves preferred/sticky does not
// stick to a channel whose site/model breaker is open (#423).
// FilterSiteRuntimeBrokenCandidatesByModel short-circuits on len<=1; preferred must
// not reuse that path.
func TestSelectPreferredChannel_OpenBreakerReturnsNil(t *testing.T) {
	ResetSiteRuntimeHealthState()
	siteRuntimeHealthLoaded = true
	t.Cleanup(ResetSiteRuntimeHealthState)

	model := "gpt-preferred"
	db := &preferredDB{
		routes: []store.TokenRoute{{
			ID:              1,
			ModelPattern:    model,
			RouteMode:       "pattern",
			RoutingStrategy: "weighted",
			Enabled:         true,
		}},
		joined: []struct {
			Channel store.RouteChannel
			Account store.Account
			Site    store.Site
			Token   *store.AccountToken
		}{
			// Preferred on broken site 10 + healthy sibling on site 20.
			preferredEligibleJoined(101, 10, 1001, model),
			preferredEligibleJoined(201, 20, 2001, model),
		},
	}

	// Open global breaker on preferred site only.
	breakerMs := nowMs() + 60_000
	healthStateMu.Lock()
	state := getOrCreateSiteRuntimeHealthState(10)
	state.BreakerLevel = 1
	state.BreakerUntilMs = &breakerMs
	healthStateMu.Unlock()

	selector := newPreferredSelector(db)
	selected, err := selector.SelectPreferredChannel(
		context.Background(),
		model,
		101, // preferred channel on broken site
		EmptyDownstreamRoutingPolicy,
		nil,
	)
	if err != nil {
		t.Fatalf("SelectPreferredChannel error: %v", err)
	}
	if selected != nil {
		t.Fatalf("expected nil preferred selection when site breaker open, got channel %d", selected.Channel.ID)
	}

	// Multi-candidate filter still keeps the intentional empty-filter full-set fallback
	// for normal selection pools (not changed by this fix).
	cands := []RouteChannelCandidate{
		{
			Channel: db.joined[0].Channel,
			Account: db.joined[0].Account,
			Site:    db.joined[0].Site,
		},
		{
			Channel: db.joined[1].Channel,
			Account: db.joined[1].Account,
			Site:    db.joined[1].Site,
		},
	}
	healthy, avoided := FilterSiteRuntimeBrokenCandidatesByModel(cands, model)
	if len(healthy) != 1 || healthy[0].Channel.ID != 201 {
		t.Fatalf("normal multi-candidate filter: want healthy sibling 201, got %+v", healthy)
	}
	if len(avoided) != 1 || avoided[0].Candidate.Channel.ID != 101 {
		t.Fatalf("normal multi-candidate filter: want avoided preferred 101, got %+v", avoided)
	}
}

// TestSelectPreferredChannel_OpenBreakerSingleCandidateStillNil covers the AC unit case:
// single preferred candidate with open breaker → nil (caller falls through).
func TestSelectPreferredChannel_OpenBreakerSingleCandidateStillNil(t *testing.T) {
	ResetSiteRuntimeHealthState()
	siteRuntimeHealthLoaded = true
	t.Cleanup(ResetSiteRuntimeHealthState)

	model := "gpt-preferred-solo"
	db := &preferredDB{
		routes: []store.TokenRoute{{
			ID:              1,
			ModelPattern:    model,
			RouteMode:       "pattern",
			RoutingStrategy: "weighted",
			Enabled:         true,
		}},
		joined: []struct {
			Channel store.RouteChannel
			Account store.Account
			Site    store.Site
			Token   *store.AccountToken
		}{
			preferredEligibleJoined(101, 10, 1001, model),
		},
	}

	breakerMs := nowMs() + 60_000
	healthStateMu.Lock()
	state := getOrCreateSiteRuntimeHealthState(10)
	state.BreakerLevel = 1
	state.BreakerUntilMs = &breakerMs
	healthStateMu.Unlock()

	// Reproduce the pre-fix short-circuit: single-candidate filter keeps broken channel.
	solo := []RouteChannelCandidate{{
		Channel: db.joined[0].Channel,
		Account: db.joined[0].Account,
		Site:    db.joined[0].Site,
	}}
	kept, _ := FilterSiteRuntimeBrokenCandidatesByModel(solo, model)
	if len(kept) != 1 {
		t.Fatalf("precondition: len<=1 filter should keep candidate, got %d", len(kept))
	}

	selector := newPreferredSelector(db)
	selected, err := selector.SelectPreferredChannel(
		context.Background(),
		model,
		101,
		EmptyDownstreamRoutingPolicy,
		nil,
	)
	if err != nil {
		t.Fatalf("SelectPreferredChannel error: %v", err)
	}
	if selected != nil {
		t.Fatalf("expected nil when preferred site breaker open (even alone), got channel %d", selected.Channel.ID)
	}
}

// TestSelectPreferredChannel_HealthyPreferredStillSelected ensures closed breakers
// still allow sticky/preferred selection.
func TestSelectPreferredChannel_HealthyPreferredStillSelected(t *testing.T) {
	ResetSiteRuntimeHealthState()
	siteRuntimeHealthLoaded = true
	t.Cleanup(ResetSiteRuntimeHealthState)

	model := "gpt-preferred-ok"
	db := &preferredDB{
		routes: []store.TokenRoute{{
			ID:              1,
			ModelPattern:    model,
			RouteMode:       "pattern",
			RoutingStrategy: "weighted",
			Enabled:         true,
		}},
		joined: []struct {
			Channel store.RouteChannel
			Account store.Account
			Site    store.Site
			Token   *store.AccountToken
		}{
			preferredEligibleJoined(101, 10, 1001, model),
			preferredEligibleJoined(201, 20, 2001, model),
		},
	}

	selector := newPreferredSelector(db)
	selected, err := selector.SelectPreferredChannel(
		context.Background(),
		model,
		101,
		EmptyDownstreamRoutingPolicy,
		nil,
	)
	if err != nil {
		t.Fatalf("SelectPreferredChannel error: %v", err)
	}
	if selected == nil {
		t.Fatal("expected preferred channel when breaker closed")
	}
	if selected.Channel.ID != 101 {
		t.Fatalf("expected channel 101, got %d", selected.Channel.ID)
	}
}
