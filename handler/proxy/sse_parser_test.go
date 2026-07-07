package proxyhandler

import (
	"strings"
	"testing"
)

var (
	benchmarkIncrementalSseResult incrementalSseAnalysisResult
	benchmarkFullSseResult        SseParseResult
)

func TestIncrementalSseAnalyzerParsesAcrossChunks(t *testing.T) {
	analyzer := newIncrementalSseAnalyzer()

	analyzer.Push([]byte("data: {\"error\":"))
	analyzer.Push([]byte("\"bad upstream\"}\n\n"))
	analyzer.Push([]byte("data: [DONE]\n\n"))

	result := analyzer.Result()
	if result.EventCount != 2 {
		t.Fatalf("EventCount = %d, want 2", result.EventCount)
	}
	if !result.HasErrorEvent || len(result.ErrorEvents) != 1 {
		t.Fatalf("error events = %#v, want one error event", result.ErrorEvents)
	}
	if !result.HasDoneMarker {
		t.Fatal("HasDoneMarker = false, want true")
	}
	if result.PendingBytes != 0 {
		t.Fatalf("PendingBytes = %d, want 0", result.PendingBytes)
	}
}

func TestIncrementalSseAnalyzerParsesCRLFEvents(t *testing.T) {
	analyzer := newIncrementalSseAnalyzer()

	analyzer.Push([]byte("data: {\"type\":\"response.failed\"}\r\n\r\n"))
	analyzer.Push([]byte("data: [DONE]\r\n\r\n"))

	result := analyzer.Result()
	if result.EventCount != 2 {
		t.Fatalf("EventCount = %d, want 2", result.EventCount)
	}
	if !result.HasErrorEvent {
		t.Fatal("HasErrorEvent = false, want true")
	}
	if !result.HasDoneMarker {
		t.Fatal("HasDoneMarker = false, want true")
	}
	if result.PendingBytes != 0 {
		t.Fatalf("PendingBytes = %d, want 0", result.PendingBytes)
	}
}

func TestIncrementalSseAnalyzerDoesNotRetainCompleteStream(t *testing.T) {
	analyzer := newIncrementalSseAnalyzer()
	chunk := "data: " + strings.Repeat("x", 128) + "\n\n"
	for i := 0; i < 20000; i++ {
		analyzer.Push([]byte(chunk))
	}

	result := analyzer.Result()
	if result.EventCount != 20000 {
		t.Fatalf("EventCount = %d, want 20000", result.EventCount)
	}
	if !result.HasDataEvent {
		t.Fatal("HasDataEvent = false, want true")
	}
	if result.PendingBytes != 0 {
		t.Fatalf("PendingBytes = %d, want 0", result.PendingBytes)
	}
	if len(result.ErrorEvents) != 0 {
		t.Fatalf("ErrorEvents = %#v, want empty", result.ErrorEvents)
	}
}

func TestIncrementalSseAnalyzerDropsOversizedSingleEventAndRecovers(t *testing.T) {
	analyzer := newIncrementalSseAnalyzer()

	analyzer.Push([]byte("data: " + strings.Repeat("x", maxIncrementalSsePendingBytes+128)))
	result := analyzer.Result()
	if !result.DroppedOversizedEvent {
		t.Fatal("DroppedOversizedEvent = false, want true")
	}
	if result.PendingBytes != 0 {
		t.Fatalf("PendingBytes = %d, want 0 after oversized event", result.PendingBytes)
	}

	analyzer.Push([]byte("\n\ndata: ok\n\n"))
	result = analyzer.Result()
	if result.EventCount != 1 {
		t.Fatalf("EventCount = %d, want recovered event count 1", result.EventCount)
	}
	if !result.HasDataEvent {
		t.Fatal("HasDataEvent = false, want true after recovery")
	}
}

func BenchmarkIncrementalSseAnalyzerLargeStream(b *testing.B) {
	raw := buildBenchmarkSseStream(20000, 128)
	chunks := splitBenchmarkChunks(raw, 4096)

	b.ReportAllocs()
	b.SetBytes(int64(len(raw)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		analyzer := newIncrementalSseAnalyzer()
		for _, chunk := range chunks {
			analyzer.Push(chunk)
		}
		benchmarkIncrementalSseResult = analyzer.Result()
	}
}

func BenchmarkParseAndAnalyzeSseStreamLargeStream(b *testing.B) {
	raw := buildBenchmarkSseStream(20000, 128)

	b.ReportAllocs()
	b.SetBytes(int64(len(raw)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkFullSseResult = ParseAndAnalyzeSseStream(raw)
	}
}

func buildBenchmarkSseStream(events int, dataBytes int) string {
	event := "data: " + strings.Repeat("x", dataBytes) + "\n\n"
	var raw strings.Builder
	raw.Grow(events * len(event))
	for i := 0; i < events; i++ {
		raw.WriteString(event)
	}
	raw.WriteString("data: [DONE]\n\n")
	return raw.String()
}

func splitBenchmarkChunks(raw string, chunkSize int) [][]byte {
	chunks := make([][]byte, 0, len(raw)/chunkSize+1)
	for len(raw) > 0 {
		n := chunkSize
		if len(raw) < n {
			n = len(raw)
		}
		chunks = append(chunks, []byte(raw[:n]))
		raw = raw[n:]
	}
	return chunks
}
