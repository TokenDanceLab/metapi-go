package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProxyAwareHTTPClientWiresCheckRedirect(t *testing.T) {
	client := ProxyAwareHTTPClient("", 5*time.Second)
	if client.CheckRedirect == nil {
		t.Fatal("ProxyAwareHTTPClient must set CheckRedirect")
	}
}

func TestProxyAwareHTTPClientRejectsCrossOriginRedirect(t *testing.T) {
	targetCalled := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/landing", http.StatusFound)
	}))
	t.Cleanup(source.Close)

	client := ProxyAwareHTTPClient("", 5*time.Second)
	resp, err := client.Get(source.URL + "/start")
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil {
		t.Fatal("cross-origin redirect was allowed")
	}
	if !strings.Contains(err.Error(), "cross-origin") {
		t.Fatalf("error = %v, want cross-origin", err)
	}
	if targetCalled {
		t.Fatal("cross-origin redirect target was called")
	}
}

func TestHTTPGetRejectsCrossOriginRedirect(t *testing.T) {
	targetCalled := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/landing", http.StatusFound)
	}))
	t.Cleanup(source.Close)

	resp, err := HTTPGet(context.Background(), "", source.URL+"/start", nil)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil {
		t.Fatal("cross-origin redirect was allowed")
	}
	if !strings.Contains(err.Error(), "cross-origin") {
		t.Fatalf("error = %v, want cross-origin", err)
	}
	if targetCalled {
		t.Fatal("cross-origin redirect target was called")
	}
}
