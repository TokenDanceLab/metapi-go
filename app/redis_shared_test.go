package app

import (
	"testing"

	"github.com/tokendancelab/metapi-go/auth"
	"github.com/tokendancelab/metapi-go/config"
	"github.com/tokendancelab/metapi-go/routing"
)

func TestConfigureSharedState_EmptyDisables(t *testing.T) {
	auth.ResetKeyAdmissionForTest()
	routing.ResetSoftCooldownForTest()
	t.Cleanup(func() {
		auth.ResetKeyAdmissionForTest()
		routing.ResetSoftCooldownForTest()
	})

	// Install something first, then clear via empty URL.
	auth.ConfigureKeyAdmissionCounter(nil)
	ConfigureSharedState(&config.Config{RedisURL: ""})
	if auth.GlobalKeyAdmission.SharedCounterEnabled() {
		t.Fatal("expected admission shared disabled")
	}
	if routing.SoftCooldownEnabled() {
		t.Fatal("expected soft cooldown disabled")
	}
}

func TestConfigureSharedState_InvalidURLKeepsLocal(t *testing.T) {
	auth.ResetKeyAdmissionForTest()
	routing.ResetSoftCooldownForTest()
	t.Cleanup(func() {
		auth.ResetKeyAdmissionForTest()
		routing.ResetSoftCooldownForTest()
	})

	ConfigureSharedState(&config.Config{RedisURL: "rediss://example.com:6379"})
	if auth.GlobalKeyAdmission.SharedCounterEnabled() {
		t.Fatal("TLS redis should not enable shared admission")
	}
	if routing.SoftCooldownEnabled() {
		t.Fatal("TLS redis should not enable soft cooldown")
	}
}
