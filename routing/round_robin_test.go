package routing

import (
	"testing"

	"github.com/tokendancelab/metapi-go/store"
)

// =============================================================================
// Round-robin candidate ordering
// =============================================================================

func makeRRCandidate(channelID int64, lastSelectedAt *string, lastUsedAt *string) RouteChannelCandidate {
	return RouteChannelCandidate{
		Channel: store.RouteChannel{
			ID:             channelID,
			LastSelectedAt: lastSelectedAt,
			LastUsedAt:    lastUsedAt,
		},
		Account: store.Account{ID: channelID * 10, Status: "active"},
		Site:    store.Site{ID: channelID * 100, Status: "active"},
	}
}

func TestGetRoundRobinCandidates_Ordering(t *testing.T) {
	earliest := "2024-01-01T00:00:00Z"
	middle := "2024-03-01T00:00:00Z"
	latest := "2024-06-01T00:00:00Z"

	candidates := []RouteChannelCandidate{
		makeRRCandidate(1, &latest, &latest),
		makeRRCandidate(2, &earliest, &earliest),
		makeRRCandidate(3, &middle, &middle),
	}

	ordered := GetRoundRobinCandidates(candidates)
	if len(ordered) != 3 {
		t.Fatalf("expected 3 candidates, got %d", len(ordered))
	}
	// Earliest first
	if ordered[0].Channel.ID != 2 {
		t.Errorf("expected channel 2 (earliest) first, got %d", ordered[0].Channel.ID)
	}
	if ordered[1].Channel.ID != 3 {
		t.Errorf("expected channel 3 (middle) second, got %d", ordered[1].Channel.ID)
	}
	if ordered[2].Channel.ID != 1 {
		t.Errorf("expected channel 1 (latest) last, got %d", ordered[2].Channel.ID)
	}
}

func TestGetRoundRobinCandidates_FallbackToLastUsedAt(t *testing.T) {
	usedEarly := "2024-01-01T00:00:00Z"
	usedLate := "2024-06-01T00:00:00Z"

	// When lastSelectedAt is nil, fall back to lastUsedAt
	candidates := []RouteChannelCandidate{
		makeRRCandidate(1, nil, &usedLate),  // no selected time, but used late
		makeRRCandidate(2, nil, &usedEarly), // no selected time, used early
	}

	ordered := GetRoundRobinCandidates(candidates)
	if ordered[0].Channel.ID != 2 {
		t.Errorf("expected channel 2 (earlier lastUsedAt) first, got %d", ordered[0].Channel.ID)
	}
}

func TestGetRoundRobinCandidates_TiebreakByChannelID(t *testing.T) {
	same := "2024-01-01T00:00:00Z"
	candidates := []RouteChannelCandidate{
		makeRRCandidate(5, &same, &same),
		makeRRCandidate(1, &same, &same),
		makeRRCandidate(3, &same, &same),
	}

	ordered := GetRoundRobinCandidates(candidates)
	// Same times → sorted by channel ID ascending
	if ordered[0].Channel.ID != 1 {
		t.Errorf("expected channel 1 (lowest ID) first, got %d", ordered[0].Channel.ID)
	}
	if ordered[1].Channel.ID != 3 {
		t.Errorf("expected channel 3 second, got %d", ordered[1].Channel.ID)
	}
	if ordered[2].Channel.ID != 5 {
		t.Errorf("expected channel 5 third, got %d", ordered[2].Channel.ID)
	}
}

func TestGetRoundRobinCandidates_Empty(t *testing.T) {
	ordered := GetRoundRobinCandidates(nil)
	if len(ordered) != 0 {
		t.Errorf("expected empty, got %d", len(ordered))
	}

	ordered = GetRoundRobinCandidates([]RouteChannelCandidate{})
	if len(ordered) != 0 {
		t.Errorf("expected empty, got %d", len(ordered))
	}
}

func TestSelectRoundRobinCandidate(t *testing.T) {
	// Normal selection
	candidates := []RouteChannelCandidate{
		makeRRCandidate(1, ptrStr("2024-06-01T00:00:00Z"), nil),
		makeRRCandidate(2, ptrStr("2024-01-01T00:00:00Z"), nil),
	}

	selected := SelectRoundRobinCandidate(candidates)
	if selected == nil {
		t.Fatal("expected a selected candidate")
	}
	if selected.Channel.ID != 2 {
		t.Errorf("expected channel 2 (earliest), got %d", selected.Channel.ID)
	}

	// Empty
	selected = SelectRoundRobinCandidate(nil)
	if selected != nil {
		t.Error("expected nil for empty")
	}
}

// =============================================================================
// Round-robin candidate with nil times
// =============================================================================

func TestGetRoundRobinCandidates_NilTimes(t *testing.T) {
	hasTime := "2024-03-01T00:00:00Z"
	candidates := []RouteChannelCandidate{
		makeRRCandidate(1, nil, nil),          // nil, nil → treated as "earliest"
		makeRRCandidate(2, &hasTime, &hasTime), // has explicit time
	}

	ordered := GetRoundRobinCandidates(candidates)
	if len(ordered) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(ordered))
	}
	// nil should be "earlier" than any real time
	if ordered[0].Channel.ID != 1 {
		t.Errorf("expected channel 1 (nil times) first, got %d", ordered[0].Channel.ID)
	}
}

// =============================================================================
// Round-robin candidate immutability
// =============================================================================

func TestGetRoundRobinCandidates_DoesNotMutateInput(t *testing.T) {
	ts := "2024-01-01T00:00:00Z"
	candidates := []RouteChannelCandidate{
		makeRRCandidate(2, &ts, &ts),
		makeRRCandidate(1, &ts, &ts),
	}

	// Verify copy is independent
	ordered := GetRoundRobinCandidates(candidates)
	if len(ordered) != 2 {
		t.Fatal("expected 2")
	}

	// Modify ordered, verify original unaffected
	ordered[0] = RouteChannelCandidate{}
	if candidates[0].Channel.ID != 2 {
		t.Error("original slice should not be modified")
	}
}
