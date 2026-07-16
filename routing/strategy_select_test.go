package routing

import (
	"testing"

	"github.com/tokendancelab/metapi-go/store"
)

func mkStrategyCandidate(channelID, siteID, weight, success, fail int64, unitCost *float64) RouteChannelCandidate {
	return RouteChannelCandidate{
		Channel: store.RouteChannel{
			ID:           channelID,
			Weight:       weight,
			SuccessCount: success,
			FailCount:    fail,
		},
		Account: store.Account{ID: channelID, UnitCost: unitCost},
		Site:    store.Site{ID: siteID, Name: "s"},
	}
}

type fakeLoadProvider map[int64]ChannelLoadSnapshot

func (f fakeLoadProvider) GetChannelLoadSnapshot(p ChannelLoadParams) ChannelLoadSnapshot {
	if s, ok := f[p.ChannelID]; ok {
		return s
	}
	return ChannelLoadSnapshot{}
}

func TestSelectLeastBusyCandidate_PrefersLowerLoad(t *testing.T) {
	c1 := mkStrategyCandidate(1, 10, 1, 0, 0, nil)
	c2 := mkStrategyCandidate(2, 20, 1, 0, 0, nil)
	load := fakeLoadProvider{
		1: {SessionScoped: true, ConcurrencyLimit: 10, ActiveLeaseCount: 8, WaitingCount: 2, Saturated: true},
		2: {SessionScoped: true, ConcurrencyLimit: 10, ActiveLeaseCount: 1, WaitingCount: 0},
	}
	got := SelectLeastBusyCandidate([]RouteChannelCandidate{c1, c2}, load)
	if got == nil || got.Channel.ID != 2 {
		t.Fatalf("expected channel 2, got %#v", got)
	}
}

func TestSelectLowestCostCandidate_PicksCheapest(t *testing.T) {
	cheap := 0.1
	expensive := 5.0
	c1 := mkStrategyCandidate(1, 10, 1, 0, 0, &expensive)
	c2 := mkStrategyCandidate(2, 20, 1, 0, 0, &cheap)
	got := SelectLowestCostCandidate([]RouteChannelCandidate{c1, c2}, func(RouteChannelCandidate) string { return "gpt-4o" }, nil, 1.0)
	if got == nil || got.Channel.ID != 2 {
		t.Fatalf("expected channel 2, got %#v", got)
	}
}

func TestSelectLowestLatencyCandidate_PrefersLowerEMA(t *testing.T) {
	ResetSiteRuntimeHealthState()
	t.Cleanup(ResetSiteRuntimeHealthState)

	c1 := mkStrategyCandidate(1, 11, 1, 0, 0, nil)
	c2 := mkStrategyCandidate(2, 22, 1, 0, 0, nil)

	// Seed site runtime latency EMAs via RecordSiteRuntimeSuccess.
	// Site 11 slow, site 22 fast.
	for i := 0; i < 5; i++ {
		RecordSiteRuntimeSuccess(11, 2000, nil, 1500)
		RecordSiteRuntimeSuccess(22, 200, nil, 80)
	}

	got := SelectLowestLatencyCandidate([]RouteChannelCandidate{c1, c2}, func(RouteChannelCandidate) string { return "gpt-4o" })
	if got == nil || got.Channel.ID != 2 {
		t.Fatalf("expected channel 2 (faster site), got %#v", got)
	}
}

func TestNormalizeRouteRoutingStrategy_NewStrategies(t *testing.T) {
	if NormalizeRouteRoutingStrategy("least_busy") != StrategyLeastBusy {
		t.Fatal("least_busy")
	}
	if NormalizeRouteRoutingStrategy("lowest_latency") != StrategyLowestLatency {
		t.Fatal("lowest_latency")
	}
	if NormalizeRouteRoutingStrategy("lowest_cost") != StrategyLowestCost {
		t.Fatal("lowest_cost")
	}
	if len(KnownRouteRoutingStrategies) < 6 {
		t.Fatalf("expected >=6 strategies, got %d", len(KnownRouteRoutingStrategies))
	}
}
