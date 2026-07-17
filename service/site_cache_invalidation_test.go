package service

import (
	"sync/atomic"
	"testing"

	"github.com/tokendancelab/metapi-go/routing"
	"github.com/tokendancelab/metapi-go/store"
)

func TestInvalidateSiteProxyCache_RunsHooksAndRouting(t *testing.T) {
	var calls atomic.Int32
	RegisterSiteProxyCacheInvalidator(func() {
		calls.Add(1)
	})

	cache := routing.NewRouteCache(1000)
	routing.SetGlobalCache(cache)
	cache.SetRoutes([]store.TokenRoute{{ID: 1}})
	if cache.GetRoutes() == nil {
		t.Fatal("expected routes before invalidate")
	}

	InvalidateSiteProxyCache()
	if calls.Load() < 1 {
		t.Fatalf("hook calls=%d want >=1", calls.Load())
	}
	if cache.GetRoutes() != nil {
		t.Fatal("expected routes cleared after InvalidateSiteProxyCache")
	}
}

func TestInvalidateSiteCaches_Delegates(t *testing.T) {
	var calls atomic.Int32
	RegisterSiteProxyCacheInvalidator(func() { calls.Add(1) })
	cache := routing.NewRouteCache(1000)
	routing.SetGlobalCache(cache)
	cache.SetRoutes([]store.TokenRoute{{ID: 2}})
	InvalidateSiteCaches()
	if calls.Load() < 1 {
		t.Fatalf("hook calls=%d", calls.Load())
	}
	if cache.GetRoutes() != nil {
		t.Fatal("routes not cleared")
	}
}

func TestRegisterSiteProxyCacheInvalidator_NilNoop(t *testing.T) {
	RegisterSiteProxyCacheInvalidator(nil) // must not panic
}
