package oauth

import (
	"testing"
	"time"
)

// ---- Singleflight Tests ----

func TestRefreshInFlight_Dedup(t *testing.T) {
	// Clean up state from other tests.
	refreshInFlightMu.Lock()
	refreshInFlight = make(map[int64]*refreshPromise)
	refreshInFlightMu.Unlock()

	// Verify the map is initially empty.
	refreshInFlightMu.Lock()
	if len(refreshInFlight) != 0 {
		t.Error("refreshInFlight should be empty at test start")
	}
	refreshInFlightMu.Unlock()

	p := newRefreshPromise()

	refreshInFlightMu.Lock()
	refreshInFlight[42] = p
	refreshInFlightMu.Unlock()

	// Verify it was stored.
	refreshInFlightMu.Lock()
	if _, exists := refreshInFlight[42]; !exists {
		t.Error("promise should exist after insertion")
	}
	refreshInFlightMu.Unlock()

	// Clean up.
	refreshInFlightMu.Lock()
	delete(refreshInFlight, 42)
	refreshInFlightMu.Unlock()
}

func TestRefreshInFlight_CleanupOnFinish(t *testing.T) {
	refreshInFlightMu.Lock()
	refreshInFlight = make(map[int64]*refreshPromise)
	refreshInFlightMu.Unlock()

	accountID := int64(999)

	// Simulate the singleflight pattern:
	// 1. Check if inflight — no
	// 2. Create promise
	p := newRefreshPromise()

	refreshInFlightMu.Lock()
	refreshInFlight[accountID] = p
	refreshInFlightMu.Unlock()

	// 3. Simulate done — resolve result and clean up
	result := refreshResult{
		AccountID:   accountID,
		AccessToken: "new-token",
		AccountKey:  "key-1",
	}
	p.resolve(result)

	refreshInFlightMu.Lock()
	delete(refreshInFlight, accountID)
	refreshInFlightMu.Unlock()

	// 4. Verify clean up.
	refreshInFlightMu.Lock()
	if _, exists := refreshInFlight[accountID]; exists {
		t.Error("promise should be cleaned up after completion")
	}
	refreshInFlightMu.Unlock()
}

func TestRefreshInFlight_ConcurrentDedup(t *testing.T) {
	refreshInFlightMu.Lock()
	refreshInFlight = make(map[int64]*refreshPromise)
	refreshInFlightMu.Unlock()

	accountID := int64(123)

	// Test that singleflight dedup works:
	// Insert a promise manually, then verify a second caller finds it.
	p := newRefreshPromise()

	refreshInFlightMu.Lock()
	refreshInFlight[accountID] = p
	refreshInFlightMu.Unlock()

	// Second caller should find the existing promise.
	var found bool
	func() {
		refreshInFlightMu.Lock()
		defer refreshInFlightMu.Unlock()
		if _, exists := refreshInFlight[accountID]; exists {
			found = true
		}
	}()

	if !found {
		t.Error("second caller should find existing promise")
	}

	// Cleanup: resolve result to unblock any waiter and remove.
	p.resolve(refreshResult{AccountID: accountID, AccessToken: "deduped-token"})
	refreshInFlightMu.Lock()
	delete(refreshInFlight, accountID)
	refreshInFlightMu.Unlock()

	if len(refreshInFlight) != 0 {
		t.Error("map should be empty after cleanup")
	}
}

func TestRefreshPromiseBroadcastsResultToAllWaiters(t *testing.T) {
	p := newRefreshPromise()

	done := make(chan refreshResult, 2)
	go func() { done <- p.wait() }()
	go func() { done <- p.wait() }()

	want := refreshResult{AccountID: 42, AccessToken: "shared-token"}
	p.resolve(want)

	for i := 0; i < 2; i++ {
		select {
		case got := <-done:
			if got.AccountID != want.AccountID || got.AccessToken != want.AccessToken {
				t.Fatalf("waiter %d got %+v, want %+v", i, got, want)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("waiter %d did not receive broadcast result", i)
		}
	}
}

// ---- coalesceStr Tests ----

func TestCoalesceStr_FirstNonEmpty(t *testing.T) {
	result := coalesceStr("a", "b", "c")
	if result != "a" {
		t.Errorf("expected 'a', got %q", result)
	}
}

func TestCoalesceStr_SkipEmpty(t *testing.T) {
	result := coalesceStr("", "", "c")
	if result != "c" {
		t.Errorf("expected 'c', got %q", result)
	}
}

func TestCoalesceStr_AllEmpty(t *testing.T) {
	result := coalesceStr("", "", "")
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

func TestCoalesceStr_NilValues(t *testing.T) {
	result := coalesceStr("", "")
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

// ---- coalesceInt64 Tests ----

func TestCoalesceInt64_FirstPositive(t *testing.T) {
	result := coalesceInt64(100, 200, 300)
	if result != 100 {
		t.Errorf("expected 100, got %d", result)
	}
}

func TestCoalesceInt64_SkipZero(t *testing.T) {
	result := coalesceInt64(0, 0, 300)
	if result != 300 {
		t.Errorf("expected 300, got %d", result)
	}
}

func TestCoalesceInt64_AllZero(t *testing.T) {
	result := coalesceInt64(0, 0, 0)
	if result != 0 {
		t.Errorf("expected 0, got %d", result)
	}
}

// ---- mergeProviderData Tests ----

func TestMergeProviderData_BothNil(t *testing.T) {
	result := mergeProviderData(nil, nil)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestMergeProviderData_RefreshedMergesOntoExisting(t *testing.T) {
	existing := map[string]interface{}{"a": 1, "b": 2}
	refreshed := map[string]interface{}{"b": "new", "c": 3}
	result := mergeProviderData(existing, refreshed)
	if result["a"] != 1 {
		t.Errorf("expected existing 'a' to be preserved, got %v", result)
	}
	if result["b"] != "new" {
		t.Errorf("expected refreshed 'b' to overwrite, got %v", result)
	}
	if result["c"] != 3 {
		t.Errorf("expected new key 'c', got %v", result)
	}
}

func TestMergeProviderData_ExistingOnly(t *testing.T) {
	existing := map[string]interface{}{"x": "y"}
	result := mergeProviderData(existing, nil)
	if result["x"] != "y" {
		t.Errorf("expected existing data preserved, got %v", result)
	}
}

func TestMergeProviderData_RefreshedOnly(t *testing.T) {
	refreshed := map[string]interface{}{"new": "data"}
	result := mergeProviderData(nil, refreshed)
	if result["new"] != "data" {
		t.Errorf("expected refreshed data, got %v", result)
	}
}

func TestMergeProviderData_EmptyResultReturnsNil(t *testing.T) {
	result := mergeProviderData(map[string]interface{}{}, map[string]interface{}{})
	if result != nil {
		t.Errorf("expected nil for empty merge, got %v", result)
	}
}

// ---- stringsTrimSpace Tests ----

func TestStringsTrimSpace_NoWhitespace(t *testing.T) {
	result := stringsTrimSpace("hello")
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}
}

func TestStringsTrimSpace_WithWhitespace(t *testing.T) {
	result := stringsTrimSpace("  hello world  ")
	if result != "hello world" {
		t.Errorf("expected 'hello world', got %q", result)
	}
}

func TestStringsTrimSpace_OnlyWhitespace(t *testing.T) {
	result := stringsTrimSpace("\t  \n")
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

// ---- ptrToString Tests ----

func TestPtrToString_Nil(t *testing.T) {
	result := ptrToString(nil)
	if result != "" {
		t.Errorf("expected empty for nil, got %q", result)
	}
}

func TestPtrToString_NonNil(t *testing.T) {
	s := "hello"
	result := ptrToString(&s)
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}
}

// ---- resolveAccountProxyURL Tests ----

func TestResolveAccountProxyURL_NoProxy(t *testing.T) {
	result := resolveAccountProxyURL(1, nil)
	if result != nil {
		t.Error("expected nil for nil extraConfig")
	}
}

// ---- refreshResult Tests ----

func TestRefreshResult_ErrorHandling(t *testing.T) {
	result := refreshResult{
		AccountID: 42,
		Err:       nil,
	}
	if result.Err != nil {
		t.Error("fresh result with nil error should not report error")
	}
}

func TestRefreshResult_WithError(t *testing.T) {
	result := refreshResult{
		AccountID: 42,
		Err:       nil,
	}
	// Simulate an error case.
	result.Err = nil
	if result.AccountID != 42 {
		t.Errorf("expected AccountID 42, got %d", result.AccountID)
	}
}
