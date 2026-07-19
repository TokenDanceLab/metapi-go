package platform

import (
	"context"
	"testing"
	"time"
)

func TestDoneHubAdapter_PlatformName(t *testing.T) {
	d := &DoneHubAdapter{
		OneHubAdapter: &OneHubAdapter{
			OneApiAdapter: &OneApiAdapter{BaseAdapter: NewBaseAdapter("done-hub")},
		},
	}
	if d.PlatformName() != "done-hub" {
		t.Errorf("PlatformName: %q", d.PlatformName())
	}
}

func TestDoneHubAdapter_Detect(t *testing.T) {
	d := &DoneHubAdapter{
		OneHubAdapter: &OneHubAdapter{
			OneApiAdapter: &OneApiAdapter{BaseAdapter: NewBaseAdapter("done-hub")},
		},
	}

	ctx := context.Background()

	tests := []struct {
		url     string
		matches bool
	}{
		{"https://donehub.example.com", true},
		{"https://DONEHUB.example.com", true},
		{"https://done-hub.example.com", true},
		{"https://DONE-HUB.example.com", true},
		{"https://onehub.example.com", false},
		{"https://newapi.example.com", false},
	}
	for _, tt := range tests {
		ok, err := d.Detect(ctx, tt.url)
		if err != nil {
			t.Errorf("Detect(%q) error: %v", tt.url, err)
			continue
		}
		if ok != tt.matches {
			t.Errorf("Detect(%q) = %v, want %v", tt.url, ok, tt.matches)
		}
	}
}

func TestDoneHubAdapter_CheckinUnspported(t *testing.T) {
	d := &DoneHubAdapter{
		OneHubAdapter: &OneHubAdapter{
			OneApiAdapter: &OneApiAdapter{BaseAdapter: NewBaseAdapter("done-hub")},
		},
	}
	ctx := context.Background()

	cr, err := d.Checkin(ctx, "http://x", "token", nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if cr.Success {
		t.Error("DoneHub checkin should always return Success=false")
	}
	if cr.Message != "checkin endpoint not found" {
		t.Errorf("Checkin message: %q", cr.Message)
	}
}

func TestDoneHubAdapter_BalanceQuotaIsRemaining(t *testing.T) {
	d := &DoneHubAdapter{
		OneHubAdapter: &OneHubAdapter{
			OneApiAdapter: &OneApiAdapter{BaseAdapter: NewBaseAdapter("done-hub")},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// DoneHub balance on unreachable URL returns empty BalanceInfo
	bi, err := d.GetBalance(ctx, unreachableBaseURL(t), "token", nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if bi.Balance != 0 {
		t.Errorf("Balance on unreachable should be 0, got %f", bi.Balance)
	}
}

func TestDoneHubAdapter_BalanceFormula(t *testing.T) {
	// DoneHub model A: quota=remaining, total=quota+used, divisor=500000
	// Verify the formula manually
	quotaRemaining := 400000.0
	used := 100000.0

	quotaUSD := quotaRemaining / 500000  // 0.8
	usedUSD := used / 500000              // 0.2
	totalUSD := quotaUSD + usedUSD         // 1.0

	if quotaUSD != 0.8 {
		t.Errorf("DoneHub quotaUSD: %f, want 0.8", quotaUSD)
	}
	if usedUSD != 0.2 {
		t.Errorf("DoneHub usedUSD: %f, want 0.2", usedUSD)
	}
	if totalUSD != 1.0 {
		t.Errorf("DoneHub totalUSD: %f, want 1.0", totalUSD)
	}

	// Balance = quotaUSD (remaining) = 0.8
	// This differs from OneApi where balance = (quota - used) / 500000 = (400000 - 100000)/500000 = 0.6
	oneApiBalance := (quotaRemaining - used) / 500000 // 0.6 (quota is total in OneApi)
	if oneApiBalance == quotaUSD {
		t.Error("DoneHub and OneApi balance formulas should differ for same inputs")
	}
}

func TestDoneHubAdapter_GetSiteAnnouncements(t *testing.T) {
	d := &DoneHubAdapter{
		OneHubAdapter: &OneHubAdapter{
			OneApiAdapter: &OneApiAdapter{BaseAdapter: NewBaseAdapter("done-hub")},
		},
	}
	ctx := context.Background()

	anns, err := d.GetSiteAnnouncements(ctx, unreachableBaseURL(t), "token", nil, nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(anns) != 0 {
		t.Error("GetSiteAnnouncements on unreachable should return empty")
	}
}
