package routing

import (
	"context"
	"testing"

	"github.com/tokendancelab/metapi-go/store"
)

// sourceModelFallbackDB is a minimal ChannelSelectorDB for loadRouteMatch source-model fallback.
type sourceModelFallbackDB struct {
	routes   []store.TokenRoute
	sources  map[int64][]int64
	channels []struct {
		Channel store.RouteChannel
		Account store.Account
		Site    store.Site
		Token   *store.AccountToken
	}
}

func (db *sourceModelFallbackDB) LoadEnabledRoutes(ctx context.Context) ([]store.TokenRoute, error) {
	out := make([]store.TokenRoute, 0, len(db.routes))
	for _, r := range db.routes {
		if r.Enabled {
			out = append(out, r)
		}
	}
	return out, nil
}

func (db *sourceModelFallbackDB) LoadRouteGroupSources(ctx context.Context, groupRouteIDs []int64) (map[int64][]int64, error) {
	result := make(map[int64][]int64)
	for _, id := range groupRouteIDs {
		if ids, ok := db.sources[id]; ok {
			cp := append([]int64(nil), ids...)
			result[id] = cp
		}
	}
	return result, nil
}

func (db *sourceModelFallbackDB) LoadRouteChannels(ctx context.Context, routeIDs []int64) ([]struct {
	Channel store.RouteChannel
	Account store.Account
	Site    store.Site
	Token   *store.AccountToken
}, error) {
	allow := make(map[int64]bool, len(routeIDs))
	for _, id := range routeIDs {
		allow[id] = true
	}
	var out []struct {
		Channel store.RouteChannel
		Account store.Account
		Site    store.Site
		Token   *store.AccountToken
	}
	for _, row := range db.channels {
		if allow[row.Channel.RouteID] {
			out = append(out, row)
		}
	}
	return out, nil
}

func (db *sourceModelFallbackDB) LoadOAuthRouteUnitSummaries(ctx context.Context, unitIDs []int64) (map[int64]OAuthRouteUnitSummary, error) {
	return map[int64]OAuthRouteUnitSummary{}, nil
}
func (db *sourceModelFallbackDB) LoadOAuthRouteUnitMembers(ctx context.Context, unitIDs []int64) (map[int64][]OAuthRouteUnitMemberCandidate, error) {
	return map[int64][]OAuthRouteUnitMemberCandidate{}, nil
}
func (db *sourceModelFallbackDB) UpdateChannelLastSelectedAt(ctx context.Context, channelID int64, lastSelectedAt string) error {
	return nil
}
func (db *sourceModelFallbackDB) UpdateRouteUnitMemberLastSelectedAt(ctx context.Context, unitID, accountID int64, lastSelectedAt string) error {
	return nil
}
func (db *sourceModelFallbackDB) FindRouteIDsByOAuthRouteUnitID(ctx context.Context, unitID int64) ([]int64, error) {
	return nil, nil
}
func (db *sourceModelFallbackDB) LoadCredentialScopedChannelIDs(ctx context.Context, channel store.RouteChannel, accountID int64) ([]int64, error) {
	return []int64{channel.ID}, nil
}
func (db *sourceModelFallbackDB) LoadChannelWithAccount(ctx context.Context, channelID int64) (*struct {
	Channel store.RouteChannel
	Account store.Account
}, error) {
	return nil, nil
}
func (db *sourceModelFallbackDB) LoadChannelWithAccountAndRoute(ctx context.Context, channelID int64) (*struct {
	Channel store.RouteChannel
	Account store.Account
	Route   store.TokenRoute
}, error) {
	return nil, nil
}
func (db *sourceModelFallbackDB) UpdateChannelCooldownFields(ctx context.Context, channelIDs []int64, updates map[string]interface{}) error {
	return nil
}
func (db *sourceModelFallbackDB) UpdateChannelSuccessFields(ctx context.Context, channelID int64, updates map[string]interface{}) error {
	return nil
}
func (db *sourceModelFallbackDB) UpdateRouteUnitMemberCooldownFields(ctx context.Context, memberID int64, updates map[string]interface{}) error {
	return nil
}
func (db *sourceModelFallbackDB) UpdateRouteUnitMemberSuccessFields(ctx context.Context, memberID int64, updates map[string]interface{}) error {
	return nil
}
func (db *sourceModelFallbackDB) LoadRouteUnitMemberWithAccount(ctx context.Context, unitID, accountID int64) (*struct {
	Member  store.OAuthRouteUnitMember
	Account store.Account
	Unit    store.OAuthRouteUnit
}, error) {
	return nil, nil
}
func (db *sourceModelFallbackDB) FindAllEnabledRoutes(ctx context.Context) ([]store.TokenRoute, error) {
	return db.LoadEnabledRoutes(ctx)
}
func (db *sourceModelFallbackDB) LoadChannelsByTokenID(ctx context.Context, tokenID int64) ([]store.RouteChannel, error) {
	return nil, nil
}
func (db *sourceModelFallbackDB) LoadChannelsByAccountIDWithoutToken(ctx context.Context, accountID int64) ([]store.RouteChannel, error) {
	return nil, nil
}
func (db *sourceModelFallbackDB) LoadRuntimeHealthChannelRows(ctx context.Context, channelIDs []int64) ([]struct {
	SiteID            int64
	SourceModel       *string
	RouteModelPattern string
}, error) {
	return nil, nil
}
func (db *sourceModelFallbackDB) ClearChannelFailureStates(ctx context.Context, channelIDs []int64) error {
	return nil
}

func TestLoadRouteMatch_GroupRouteNilSourceModelGetsSourceRoutePattern(t *testing.T) {
	displayName := "group-claude"
	emptySource := ""
	explicitSource := "claude-3-haiku"

	groupRoute := store.TokenRoute{
		ID:              100,
		ModelPattern:    "group-claude",
		DisplayName:     &displayName,
		RouteMode:       "explicit_group",
		RoutingStrategy: "weighted",
		Enabled:         true,
	}
	sourceRouteA := store.TokenRoute{
		ID:              10,
		ModelPattern:    "claude-3-opus",
		RouteMode:       "pattern",
		RoutingStrategy: "weighted",
		Enabled:         true,
	}
	sourceRouteB := store.TokenRoute{
		ID:              20,
		ModelPattern:    "claude-3-sonnet",
		RouteMode:       "pattern",
		RoutingStrategy: "weighted",
		Enabled:         true,
	}

	token := store.AccountToken{ID: 1, AccountID: 1, Name: "default", Token: "tok-a", Enabled: true, IsDefault: true}
	apiToken := "api-token-a"

	db := &sourceModelFallbackDB{
		routes: []store.TokenRoute{groupRoute, sourceRouteA, sourceRouteB},
		sources: map[int64][]int64{
			100: {10, 20},
		},
		channels: []struct {
			Channel store.RouteChannel
			Account store.Account
			Site    store.Site
			Token   *store.AccountToken
		}{
			{
				// nil SourceModel → should fall back to source route pattern
				Channel: store.RouteChannel{
					ID: 1, RouteID: 10, AccountID: 1, TokenID: &token.ID,
					SourceModel: nil, Priority: 0, Weight: 10, Enabled: true,
				},
				Account: store.Account{ID: 1, SiteID: 1, Status: "active", APIToken: &apiToken},
				Site:    store.Site{ID: 1, Name: "site-a", Status: "active", GlobalWeight: 1},
				Token:   &token,
			},
			{
				// empty SourceModel → same fallback
				Channel: store.RouteChannel{
					ID: 2, RouteID: 20, AccountID: 2, TokenID: &token.ID,
					SourceModel: &emptySource, Priority: 0, Weight: 10, Enabled: true,
				},
				Account: store.Account{ID: 2, SiteID: 1, Status: "active", APIToken: &apiToken},
				Site:    store.Site{ID: 1, Name: "site-a", Status: "active", GlobalWeight: 1},
				Token:   &token,
			},
			{
				// explicit SourceModel must be preserved
				Channel: store.RouteChannel{
					ID: 3, RouteID: 10, AccountID: 3, TokenID: &token.ID,
					SourceModel: &explicitSource, Priority: 0, Weight: 10, Enabled: true,
				},
				Account: store.Account{ID: 3, SiteID: 1, Status: "active", APIToken: &apiToken},
				Site:    store.Site{ID: 1, Name: "site-a", Status: "active", GlobalWeight: 1},
				Token:   &token,
			},
		},
	}

	selector := NewChannelSelector(db, NewRouteCache(60_000), 3600, defaultRoutingWeights(), nil, 1, nil)
	match, err := selector.loadRouteMatch(context.Background(), groupRoute)
	if err != nil {
		t.Fatalf("loadRouteMatch: %v", err)
	}
	if match == nil {
		t.Fatal("expected non-nil RouteMatch")
	}
	if len(match.Channels) != 3 {
		t.Fatalf("expected 3 channels, got %d", len(match.Channels))
	}

	byID := make(map[int64]RouteChannelCandidate, len(match.Channels))
	for _, c := range match.Channels {
		byID[c.Channel.ID] = c
	}

	gotA := NormalizeChannelSourceModel(byID[1].Channel.SourceModel)
	if gotA != "claude-3-opus" {
		t.Fatalf("nil SourceModel channel: got %q, want source route pattern claude-3-opus", gotA)
	}
	gotB := NormalizeChannelSourceModel(byID[2].Channel.SourceModel)
	if gotB != "claude-3-sonnet" {
		t.Fatalf("empty SourceModel channel: got %q, want source route pattern claude-3-sonnet", gotB)
	}
	gotC := NormalizeChannelSourceModel(byID[3].Channel.SourceModel)
	if gotC != "claude-3-haiku" {
		t.Fatalf("explicit SourceModel channel: got %q, want claude-3-haiku (no overwrite)", gotC)
	}
}

func TestLoadFallbackSourceModelByRouteID_NonGroupUsesRoutePattern(t *testing.T) {
	route := store.TokenRoute{
		ID:           7,
		ModelPattern: "gpt-4o",
		RouteMode:    "pattern",
		Enabled:      true,
	}
	db := &sourceModelFallbackDB{routes: []store.TokenRoute{route}}
	selector := NewChannelSelector(db, NewRouteCache(60_000), 3600, defaultRoutingWeights(), nil, 1, nil)

	fallback, err := selector.loadFallbackSourceModelByRouteID(context.Background(), route, []int64{7})
	if err != nil {
		t.Fatalf("loadFallbackSourceModelByRouteID: %v", err)
	}
	if got := fallback[7]; got != "gpt-4o" {
		t.Fatalf("fallback[7]=%q, want gpt-4o", got)
	}
}
