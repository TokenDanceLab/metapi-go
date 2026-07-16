package routing

import (
	"testing"
	"time"

	"github.com/tokendancelab/metapi-go/internal/redisx"
)

func TestSoftCooldown_NoopDefault(t *testing.T) {
	ResetSoftCooldownForTest()
	if SoftCooldownEnabled() {
		t.Fatal("expected disabled")
	}
	if IsSoftChannelCooldownActive(1) {
		t.Fatal("noop should not be active")
	}
	MarkSoftChannelCooldown(1, time.Minute) // no-op
	if IsSoftChannelCooldownActive(1) {
		t.Fatal("still inactive")
	}
}

func TestSoftCooldown_MemoryMarker(t *testing.T) {
	ResetSoftCooldownForTest()
	t.Cleanup(ResetSoftCooldownForTest)

	mem := redisx.NewMemoryCooldown()
	base := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	now := base
	mem.SetNowFunc(func() time.Time { return now })
	ConfigureSoftCooldown(mem)

	if !SoftCooldownEnabled() {
		t.Fatal("expected enabled")
	}
	MarkSoftChannelCooldown(7, 30*time.Second)
	if !IsSoftChannelCooldownActive(7) {
		t.Fatal("expected active")
	}
	now = base.Add(31 * time.Second)
	if IsSoftChannelCooldownActive(7) {
		t.Fatal("expected expired")
	}
}

func TestSoftCooldown_FailOpenOnError(t *testing.T) {
	ResetSoftCooldownForTest()
	t.Cleanup(ResetSoftCooldownForTest)

	fake := redisx.NewFakeCooldown()
	ConfigureSoftCooldown(fake)
	fake.FailNext = true
	// Mark error is fail-open (logged, counted)
	MarkSoftChannelCooldown(3, time.Minute)
	if SoftCooldownFailOpenCount() != 1 {
		t.Fatalf("failopen=%d", SoftCooldownFailOpenCount())
	}
	// Active error → false
	fake.FailNext = true
	if IsSoftChannelCooldownActive(3) {
		t.Fatal("fail-open Active must return false")
	}
	if SoftCooldownFailOpenCount() != 2 {
		t.Fatalf("failopen=%d", SoftCooldownFailOpenCount())
	}
}

func TestIsChannelCoolingDown(t *testing.T) {
	ResetSoftCooldownForTest()
	t.Cleanup(ResetSoftCooldownForTest)

	nowISO := "2026-07-17T12:00:00Z"
	future := "2026-07-17T12:05:00Z"
	if !isChannelCoolingDown(1, &future, nowISO) {
		t.Fatal("DB future cooldown should cool")
	}
	past := "2026-07-17T11:00:00Z"
	if isChannelCoolingDown(1, &past, nowISO) {
		t.Fatal("past DB cooldown should not cool without soft marker")
	}

	mem := redisx.NewMemoryCooldown()
	ConfigureSoftCooldown(mem)
	_ = mem.Mark(2, time.Minute)
	if !isChannelCoolingDown(2, nil, nowISO) {
		t.Fatal("soft marker alone should cool")
	}
}
