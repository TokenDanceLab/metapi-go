package proxy

import (
	"testing"

	"github.com/tokendancelab/metapi-go/platform"
)

func TestApplyKeyProxyOverride_PreferKeyOverSite(t *testing.T) {
	site := "http://site-proxy:8080"
	key := "http://key-proxy:9090"
	base := &platform.ProxyConfig{
		ProxyURL: site,
		CustomHeaders: map[string]string{
			"X-Metapi-Site": "site-header",
		},
	}

	got := ApplyKeyProxyOverride(base, &key)
	if got == nil {
		t.Fatal("expected non-nil proxy config")
	}
	if got.ProxyURL != key {
		t.Fatalf("ProxyURL = %q, want key proxy %q", got.ProxyURL, key)
	}
	if got.UseSystemProxy {
		t.Fatal("UseSystemProxy should be false after key override")
	}
	if got.CustomHeaders["X-Metapi-Site"] != "site-header" {
		t.Fatalf("custom headers lost: %#v", got.CustomHeaders)
	}
	// base must not be mutated
	if base.ProxyURL != site {
		t.Fatalf("base mutated: ProxyURL = %q", base.ProxyURL)
	}
}

func TestApplyKeyProxyOverride_NilKeyFallsBackToSite(t *testing.T) {
	site := "http://site-proxy:8080"
	base := &platform.ProxyConfig{ProxyURL: site, UseSystemProxy: false}

	got := ApplyKeyProxyOverride(base, nil)
	if got != base {
		t.Fatalf("expected same base pointer on nil key, got %#v", got)
	}
	if got.ProxyURL != site {
		t.Fatalf("ProxyURL = %q, want site", got.ProxyURL)
	}
}

func TestApplyKeyProxyOverride_EmptyKeyFallsBackToSite(t *testing.T) {
	site := "http://site-proxy:8080"
	empty := "   "
	base := &platform.ProxyConfig{ProxyURL: site}

	got := ApplyKeyProxyOverride(base, &empty)
	if got != base {
		t.Fatalf("expected base on empty key, got %#v", got)
	}
}

func TestApplyKeyProxyOverride_KeyOnlyWhenNoSite(t *testing.T) {
	key := "socks5://key-proxy:1080"
	got := ApplyKeyProxyOverride(nil, &key)
	if got == nil {
		t.Fatal("expected non-nil config")
	}
	if got.ProxyURL != key {
		t.Fatalf("ProxyURL = %q, want %q", got.ProxyURL, key)
	}
}

func TestApplyKeyProxyOverride_KeyOverridesSystemProxyFlag(t *testing.T) {
	key := "http://key-proxy:1"
	base := &platform.ProxyConfig{
		ProxyURL:       "http://system-proxy:8080",
		UseSystemProxy: true,
	}
	got := ApplyKeyProxyOverride(base, &key)
	if got.ProxyURL != key {
		t.Fatalf("ProxyURL = %q, want key", got.ProxyURL)
	}
	if got.UseSystemProxy {
		t.Fatal("UseSystemProxy should be cleared by key override")
	}
}

func TestResolveKeyProxyURL(t *testing.T) {
	if ResolveKeyProxyURL(nil) != nil {
		t.Fatal("nil in → nil out")
	}
	empty := "  "
	if ResolveKeyProxyURL(&empty) != nil {
		t.Fatal("whitespace → nil")
	}
	v := " http://p:1 "
	got := ResolveKeyProxyURL(&v)
	if got == nil || *got != "http://p:1" {
		t.Fatalf("got %#v", got)
	}
}
