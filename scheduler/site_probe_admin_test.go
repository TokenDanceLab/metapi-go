package scheduler

import "testing"

func TestGlobalModelProbeRegistry(t *testing.T) {
	SetGlobalModelProbeScheduler(nil)
	if GetGlobalModelProbeScheduler() != nil {
		t.Fatal("expected nil")
	}
	s := NewModelProbeScheduler(nil)
	SetGlobalModelProbeScheduler(s)
	if GetGlobalModelProbeScheduler() != s {
		t.Fatal("expected same instance")
	}
	SetGlobalModelProbeScheduler(nil)
}

func TestProbeSite_EmptyWithoutDB(t *testing.T) {
	s := NewModelProbeScheduler(nil)
	res, a, u := s.ProbeSite(1)
	if a != 0 || u != 0 {
		t.Fatalf("a=%d u=%d res=%v", a, u, res)
	}
}
