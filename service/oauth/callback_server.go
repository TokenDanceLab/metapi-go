package oauth

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
)

// ---- Callback Server State ----

// LoopbackCallbackServerState represents the state of a loopback callback server.
type LoopbackCallbackServerState struct {
	Provider    string `json:"provider"`
	Attempted   bool   `json:"attempted"`
	Ready       bool   `json:"ready"`
	Host        string `json:"host,omitempty"`
	Port        int    `json:"port"`
	Path        string `json:"path"`
	Origin      string `json:"origin"`
	RedirectURI string `json:"redirectUri"`
	Error       string `json:"error,omitempty"`
}

var (
	callbackServers     = make(map[string]*http.Server)
	callbackStates      = make(map[string]*LoopbackCallbackServerState)
	callbackStartPromises = make(map[string]chan struct{})
	callbackMu          sync.Mutex
)

// ---- HTML helpers ----

func escapeHTML(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&#39;",
	)
	return replacer.Replace(value)
}

func renderCompletionPage(message string) string {
	safeMessage := escapeHTML(message)
	return `<!doctype html>
<html lang="zh-CN">
  <head>
    <meta charset="utf-8" />
    <title>OAuth Callback</title>
  </head>
  <body>
    <script>window.close();</script>
    ` + safeMessage + `
  </body>
</html>`
}

func respondHTML(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(statusCode)
	w.Write([]byte(renderCompletionPage(message)))
}

func normalizeOrigin(host string, port int) string {
	if host == "" || host == "::" || host == "0.0.0.0" {
		return fmt.Sprintf("http://localhost:%d", port)
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return fmt.Sprintf("http://[%s]:%d", host, port)
	}
	return fmt.Sprintf("http://%s:%d", host, port)
}

func createDefaultState(provider string) *LoopbackCallbackServerState {
	def := GetProviderDefinition(provider)
	if def == nil {
		return nil
	}
	return &LoopbackCallbackServerState{
		Provider:    provider,
		Attempted:   false,
		Ready:       false,
		Host:        def.Loopback.Host,
		Port:        def.Loopback.Port,
		Path:        def.Loopback.Path,
		Origin:      normalizeOrigin(def.Loopback.Host, def.Loopback.Port),
		RedirectURI: def.Loopback.RedirectURI,
	}
}

// ---- Get State ----

// GetLoopbackCallbackServerState returns a copy of the stored state for a provider.
func GetLoopbackCallbackServerState(provider string) *LoopbackCallbackServerState {
	callbackMu.Lock()
	defer callbackMu.Unlock()

	state, ok := callbackStates[provider]
	if ok {
		copy := *state
		return &copy
	}
	return createDefaultState(provider)
}

// GetLoopbackCallbackServerStates returns states for all registered providers.
func GetLoopbackCallbackServerStates() []*LoopbackCallbackServerState {
	defs := ListProviderDefinitions()
	result := make([]*LoopbackCallbackServerState, len(defs))
	for i, def := range defs {
		result[i] = GetLoopbackCallbackServerState(string(def.Metadata.Provider))
	}
	return result
}

// ---- Start Server ----

// StartLoopbackCallbackServer starts the loopback HTTP server for a provider.
// It is idempotent: if already running, returns current state.
// If a start is in-flight, waits for it.
func StartLoopbackCallbackServer(provider string) (*LoopbackCallbackServerState, error) {
	def := GetProviderDefinition(provider)
	if def == nil {
		return nil, fmt.Errorf("unsupported oauth provider: %s", provider)
	}

	callbackMu.Lock()
	if _, hasServer := callbackServers[provider]; hasServer {
		callbackMu.Unlock()
		return GetLoopbackCallbackServerState(provider), nil
	}

	if ch, inFlight := callbackStartPromises[provider]; inFlight {
		callbackMu.Unlock()
		<-ch
		return GetLoopbackCallbackServerState(provider), nil
	}

	ch := make(chan struct{})
	callbackStartPromises[provider] = ch
	callbackMu.Unlock()

	defer func() {
		callbackMu.Lock()
		delete(callbackStartPromises, provider)
		callbackMu.Unlock()
		close(ch)
	}()

	host := def.Loopback.Host
	port := def.Loopback.Port
	path := def.Loopback.Path
	redirectURI := def.Loopback.RedirectURI

	addr := fmt.Sprintf("%s:%d", host, port)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		handleCallbackRequest(provider, w, r)
	})

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		failedState := &LoopbackCallbackServerState{
			Provider:    provider,
			Attempted:   true,
			Ready:       false,
			Host:        host,
			Port:        port,
			Path:        path,
			Origin:      normalizeOrigin(host, port),
			RedirectURI: redirectURI,
			Error:       err.Error(),
		}
		callbackMu.Lock()
		callbackStates[provider] = failedState
		callbackMu.Unlock()
		return GetLoopbackCallbackServerState(provider), fmt.Errorf("failed to start %s oauth callback server: %w", provider, err)
	}

	callbackMu.Lock()
	callbackServers[provider] = server
	nextState := &LoopbackCallbackServerState{
		Provider:    provider,
		Attempted:   true,
		Ready:       true,
		Host:        host,
		Port:        port,
		Path:        path,
		Origin:      normalizeOrigin(host, port),
		RedirectURI: redirectURI,
	}
	callbackStates[provider] = nextState
	callbackMu.Unlock()

	// Serve in background.
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			callbackMu.Lock()
			if state, ok := callbackStates[provider]; ok {
				state.Ready = false
				state.Error = err.Error()
			}
			callbackMu.Unlock()
		}
	}()

	return GetLoopbackCallbackServerState(provider), nil
}

// StartLoopbackCallbackServers starts callback servers for all registered providers.
func StartLoopbackCallbackServers() []*LoopbackCallbackServerState {
	defs := ListProviderDefinitions()
	results := make([]*LoopbackCallbackServerState, len(defs))
	for i, def := range defs {
		state, err := StartLoopbackCallbackServer(string(def.Metadata.Provider))
		if err != nil {
			results[i] = GetLoopbackCallbackServerState(string(def.Metadata.Provider))
		} else {
			results[i] = state
		}
	}
	return results
}

// StopLoopbackCallbackServers shuts down all callback servers.
func StopLoopbackCallbackServers() {
	callbackMu.Lock()
	serversCopy := make(map[string]*http.Server)
	for k, v := range callbackServers {
		serversCopy[k] = v
	}
	callbackServers = make(map[string]*http.Server)
	callbackStates = make(map[string]*LoopbackCallbackServerState)
	callbackStartPromises = make(map[string]chan struct{})
	callbackMu.Unlock()

	for provider, server := range serversCopy {
		server.Close()
		def := GetProviderDefinition(provider)
		if def != nil {
			callbackMu.Lock()
			callbackStates[provider] = createDefaultState(provider)
			callbackMu.Unlock()
		}
	}
}

// ---- Handle Callback Request ----

func handleCallbackRequest(provider string, w http.ResponseWriter, r *http.Request) {
	def := GetProviderDefinition(provider)
	if def == nil {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Not found"))
		return
	}

	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Write([]byte("Method not allowed"))
		return
	}

	if r.URL.Path != def.Loopback.Path {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Not found"))
		return
	}

	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	errorParam := r.URL.Query().Get("error")

	_, err := HandleCallback(CallbackInput{
		Provider: provider,
		State:    state,
		Code:     code,
		Error:    errorParam,
	})

	if err != nil {
		respondHTML(w, http.StatusInternalServerError, "OAuth authorization failed. Return to metapi and review the server logs.")
		return
	}
	respondHTML(w, http.StatusOK, "OAuth authorization succeeded. You can close this window.")
}
