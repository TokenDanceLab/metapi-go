package routing

import (
	"context"
	"testing"

	"github.com/tokendancelab/metapi-go/store"
)

func int64p(v int64) *int64 { return &v }

func TestEstimateRequestContextTokens_Messages(t *testing.T) {
	t.Parallel()
	// 40 runes → 10 tokens; max_tokens 100 → 110
	body := map[string]any{
		"model":      "gpt-4o",
		"max_tokens": 100,
		"messages": []any{
			map[string]any{"role": "user", "content": "0123456789012345678901234567890123456789"},
		},
	}
	got := EstimateRequestContextTokens(body)
	if got < 100 {
		t.Fatalf("expected estimate include max_tokens, got %d", got)
	}
}

func TestEstimateRequestContextTokens_Empty(t *testing.T) {
	t.Parallel()
	if got := EstimateRequestContextTokens(nil); got != 0 {
		t.Fatalf("nil → 0, got %d", got)
	}
	if got := EstimateRequestContextTokens(map[string]any{"model": "x"}); got != 0 {
		t.Fatalf("no messages → 0, got %d", got)
	}
}

func TestPickContextTierRoute_TightestFit(t *testing.T) {
	t.Parallel()
	routes := []store.TokenRoute{
		{ID: 1, ModelPattern: "gpt-4o", ContextLength: int64p(32_000)},
		{ID: 2, ModelPattern: "gpt-4o", ContextLength: int64p(128_000)},
		{ID: 3, ModelPattern: "gpt-4o", ContextLength: int64p(1_000_000)},
	}
	// 20k → 32k tier
	got := PickContextTierRoute(routes, 20_000)
	if got == nil || got.ID != 1 {
		t.Fatalf("20k → id1, got %#v", got)
	}
	// 80k → 128k
	got = PickContextTierRoute(routes, 80_000)
	if got == nil || got.ID != 2 {
		t.Fatalf("80k → id2, got %#v", got)
	}
	// 500k → 1M
	got = PickContextTierRoute(routes, 500_000)
	if got == nil || got.ID != 3 {
		t.Fatalf("500k → id3, got %#v", got)
	}
}

func TestPickContextTierRoute_OversizedUsesLargest(t *testing.T) {
	t.Parallel()
	routes := []store.TokenRoute{
		{ID: 1, ModelPattern: "gpt-4o", ContextLength: int64p(32_000)},
		{ID: 2, ModelPattern: "gpt-4o", ContextLength: int64p(64_000)},
	}
	got := PickContextTierRoute(routes, 200_000)
	if got == nil || got.ID != 2 {
		t.Fatalf("oversized → largest, got %#v", got)
	}
}

func TestPickContextTierRoute_UnsizedCatchAll(t *testing.T) {
	t.Parallel()
	routes := []store.TokenRoute{
		{ID: 1, ModelPattern: "gpt-4o", ContextLength: int64p(32_000)},
		{ID: 9, ModelPattern: "gpt-4o", ContextLength: nil},
	}
	// Fits sized
	got := PickContextTierRoute(routes, 10_000)
	if got == nil || got.ID != 1 {
		t.Fatalf("fit sized, got %#v", got)
	}
	// Only unsized available when all sized gone? oversized still picks largest sized
	got = PickContextTierRoute(routes, 200_000)
	if got == nil || got.ID != 1 {
		t.Fatalf("oversized with one sized → largest sized, got %#v", got)
	}
	// Only unsized routes
	onlyNil := []store.TokenRoute{
		{ID: 5, ModelPattern: "gpt-4o", ContextLength: nil},
		{ID: 6, ModelPattern: "gpt-4o", ContextLength: int64p(0)},
	}
	got = PickContextTierRoute(onlyNil, 50_000)
	if got == nil || got.ID != 5 {
		t.Fatalf("unsized only → first, got %#v", got)
	}
}

func TestPickContextTierRoute_UnknownEstimateFirstMatch(t *testing.T) {
	t.Parallel()
	routes := []store.TokenRoute{
		{ID: 2, ModelPattern: "gpt-4o", ContextLength: int64p(128_000)},
		{ID: 1, ModelPattern: "gpt-4o", ContextLength: int64p(32_000)},
	}
	got := PickContextTierRoute(routes, 0)
	if got == nil || got.ID != 2 {
		t.Fatalf("estimate=0 → first-match id2, got %#v", got)
	}
}

func TestPickContextTierRoute_Empty(t *testing.T) {
	t.Parallel()
	if PickContextTierRoute(nil, 100) != nil {
		t.Fatal("nil routes")
	}
}

func TestSelectChannel_MultiTierContext(t *testing.T) {
	// Integration: two exact model routes with different context_length ceilings;
	// estimate drives which route (and thus channel) is selected.
	ResetSiteRuntimeHealthState()
	siteRuntimeHealthLoaded = true
	t.Cleanup(ResetSiteRuntimeHealthState)

	token := "tok"
	sm := "gpt-4o"
	routeSmall := store.TokenRoute{
		ID: 10, ModelPattern: "gpt-4o", RouteMode: "pattern",
		RoutingStrategy: "weighted", Enabled: true, ContextLength: int64p(32_000),
	}
	routeLarge := store.TokenRoute{
		ID: 20, ModelPattern: "gpt-4o", RouteMode: "pattern",
		RoutingStrategy: "weighted", Enabled: true, ContextLength: int64p(128_000),
	}
	// Channels on different routes
	chSmall := store.RouteChannel{ID: 100, RouteID: 10, AccountID: 1, SourceModel: &sm, Priority: 0, Weight: 10, Enabled: true}
	chLarge := store.RouteChannel{ID: 200, RouteID: 20, AccountID: 2, SourceModel: &sm, Priority: 0, Weight: 10, Enabled: true}
	site := store.Site{ID: 1, Name: "s", Platform: "openai", URL: "https://example.com", Status: "active"}
	acc1 := store.Account{ID: 1, SiteID: 1, Status: "active", AccessToken: token, APIToken: &token}
	acc2 := store.Account{ID: 2, SiteID: 1, Status: "active", AccessToken: token, APIToken: &token}

	db := &preferredDB{
		routes: []store.TokenRoute{routeSmall, routeLarge},
		joined: []struct {
			Channel store.RouteChannel
			Account store.Account
			Site    store.Site
			Token   *store.AccountToken
		}{
			// loadRouteChannels is filtered by routeIDs — return all and filter in mock?
		},
	}
	// preferredDB LoadRouteChannels returns ALL joined regardless of routeIDs.
	// Override by setting only matching channels after each select is hard.
	// Use a filtering wrapper:
	fdb := &tierDB{preferredDB: *db}
	fdb.routes = []store.TokenRoute{routeSmall, routeLarge}
	fdb.joined = []struct {
		Channel store.RouteChannel
		Account store.Account
		Site    store.Site
		Token   *store.AccountToken
	}{
		{Channel: chSmall, Account: acc1, Site: site, Token: nil},
		{Channel: chLarge, Account: acc2, Site: site, Token: nil},
	}

	selector := NewChannelSelector(fdb, NewRouteCache(0), 0, RoutingWeightsConfig{}, nil, 0, nil)
	ctx := context.Background()

	// Small estimate → 32k route → channel 100
	policy := EmptyDownstreamRoutingPolicy
	policy.RequestedContextTokens = 10_000
	sel, err := selector.SelectChannel(ctx, "gpt-4o", policy)
	if err != nil {
		t.Fatal(err)
	}
	if sel == nil || sel.Channel.ID != 100 {
		t.Fatalf("small ctx → ch100, got %#v", sel)
	}
	if sel.ContextLength == nil || *sel.ContextLength != 32_000 {
		t.Fatalf("context_length 32k, got %#v", sel.ContextLength)
	}

	// Large estimate → 128k route → channel 200
	policy.RequestedContextTokens = 80_000
	// Clear cache so second match reloads
	selector.cache = NewRouteCache(0)
	sel, err = selector.SelectChannel(ctx, "gpt-4o", policy)
	if err != nil {
		t.Fatal(err)
	}
	if sel == nil || sel.Channel.ID != 200 {
		t.Fatalf("large ctx → ch200, got %#v", sel)
	}

	// Unknown estimate → first match (route id 10 first in list)
	policy.RequestedContextTokens = 0
	selector.cache = NewRouteCache(0)
	sel, err = selector.SelectChannel(ctx, "gpt-4o", policy)
	if err != nil {
		t.Fatal(err)
	}
	if sel == nil || sel.Channel.ID != 100 {
		t.Fatalf("unknown → first match ch100, got %#v", sel)
	}
}

// tierDB filters LoadRouteChannels by requested route IDs.
type tierDB struct {
	preferredDB
}

func (db *tierDB) LoadRouteChannels(ctx context.Context, routeIDs []int64) ([]struct {
	Channel store.RouteChannel
	Account store.Account
	Site    store.Site
	Token   *store.AccountToken
}, error) {
	all, err := db.preferredDB.LoadRouteChannels(ctx, routeIDs)
	if err != nil {
		return nil, err
	}
	allow := map[int64]bool{}
	for _, id := range routeIDs {
		allow[id] = true
	}
	out := make([]struct {
		Channel store.RouteChannel
		Account store.Account
		Site    store.Site
		Token   *store.AccountToken
	}, 0)
	for _, j := range all {
		if allow[j.Channel.RouteID] {
			out = append(out, j)
		}
	}
	return out, nil
}
