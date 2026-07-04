package app

import (
	"encoding/json"
	"net/http"
	"runtime"
)

// MetricsHandler serves a /debug/vars style endpoint returning JSON with
// goroutine count and memory statistics.
func MetricsHandler(w http.ResponseWriter, r *http.Request) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	payload := map[string]any{
		"goroutines": runtime.NumGoroutine(),
		"mem": map[string]any{
			"alloc":         mem.Alloc,
			"totalAlloc":    mem.TotalAlloc,
			"sys":           mem.Sys,
			"heapAlloc":     mem.HeapAlloc,
			"heapSys":       mem.HeapSys,
			"heapIdle":      mem.HeapIdle,
			"heapInuse":     mem.HeapInuse,
			"heapReleased":  mem.HeapReleased,
			"heapObjects":   mem.HeapObjects,
			"stackInuse":    mem.StackInuse,
			"stackSys":      mem.StackSys,
			"numGC":         mem.NumGC,
			"numForcedGC":   mem.NumForcedGC,
			"gcPauseTotalNs": mem.PauseTotalNs,
			"lastGCUnixNs":  mem.LastGC,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(payload)
}
