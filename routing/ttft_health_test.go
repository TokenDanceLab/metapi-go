package routing

import "testing"

func TestGetRuntimeHealthMultiplier_PrefersFirstByteEMA(t *testing.T) {
	slowTotal := 5000.0
	fastTTFT := 100.0
	slow := &SiteRuntimeHealthState{LatencyEMAMs: &slowTotal, FirstByteEMAMs: &fastTTFT}
	onlySlow := &SiteRuntimeHealthState{LatencyEMAMs: &slowTotal}
	mFast := GetRuntimeHealthMultiplier(slow)
	mSlow := GetRuntimeHealthMultiplier(onlySlow)
	if mFast <= mSlow {
		t.Fatalf("expected first-byte-aware state to score higher: fast=%v slow=%v", mFast, mSlow)
	}
}
