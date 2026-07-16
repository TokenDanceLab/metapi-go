package shared

import "testing"

// Streaming semantics (MiniMax-friendly):
// - confirmed content is emitted immediately (only partial tag suffixes buffer)
// - open-but-unclosed reasoning is emitted progressively
// - content after a close tag is emitted when confirmed
// - orphan </think> treats prefix as reasoning (MiniMax often omits open tag)

func TestConsumeThinkTaggedText_NoTags_Immediate(t *testing.T) {
	state := CreateThinkTagParserState()
	content, reasoning := ConsumeThinkTaggedText(state, "plain text")
	if reasoning != "" {
		t.Errorf("expected no reasoning, got %q", reasoning)
	}
	if content != "plain text" {
		t.Errorf("expected immediate content 'plain text', got %q", content)
	}
	if state.Pending != "" {
		t.Errorf("expected empty pending, got %q", state.Pending)
	}
}

func TestConsumeThinkTaggedText_CompleteTag_EmitsContent(t *testing.T) {
	state := CreateThinkTagParserState()
	text := thinkOpen + "thinking step by step..." + thinkClose + "Here is the answer."

	content, reasoning := ConsumeThinkTaggedText(state, text)
	if reasoning != "thinking step by step..." {
		t.Errorf("expected 'thinking step by step...', got %q", reasoning)
	}
	if content != "Here is the answer." {
		t.Errorf("expected content 'Here is the answer.', got %q", content)
	}
}

func TestConsumeThinkTaggedText_OpenedTag_ProgressiveReasoning(t *testing.T) {
	state := CreateThinkTagParserState()
	text := "before " + thinkOpen + "thinking..."

	content, reasoning := ConsumeThinkTaggedText(state, text)
	if content != "before " {
		t.Errorf("expected 'before ', got %q", content)
	}
	if reasoning != "thinking..." {
		t.Errorf("expected progressive reasoning 'thinking...', got %q", reasoning)
	}
	if state.Mode != "reasoning" {
		t.Errorf("expected reasoning mode, got %q", state.Mode)
	}
}

func TestConsumeThinkTaggedText_ContinuedReasoning_Progressive(t *testing.T) {
	state := CreateThinkTagParserState()

	content1, reasoning1 := ConsumeThinkTaggedText(state, thinkOpen+"part 1")
	if content1 != "" {
		t.Errorf("expected empty content, got %q", content1)
	}
	if reasoning1 != "part 1" {
		t.Errorf("expected progressive reasoning 'part 1', got %q", reasoning1)
	}
	if state.Mode != "reasoning" {
		t.Errorf("expected reasoning mode, got %q", state.Mode)
	}

	content2, reasoning2 := ConsumeThinkTaggedText(state, "more thinking"+thinkClose+"final answer")
	if reasoning2 != "more thinking" {
		t.Errorf("expected 'more thinking', got %q", reasoning2)
	}
	if content2 != "final answer" {
		t.Errorf("expected content 'final answer', got %q", content2)
	}
	if state.Mode != "content" {
		t.Errorf("expected back to content mode, got %q", state.Mode)
	}
}

func TestConsumeThinkTaggedText_MultipleTags_EmitsAllContent(t *testing.T) {
	state := CreateThinkTagParserState()
	text := thinkOpen + "think1" + thinkClose + " content1 " +
		thinkOpen + "think2" + thinkClose + " content2"

	content, reasoning := ConsumeThinkTaggedText(state, text)
	if content != " content1  content2" {
		t.Errorf("expected ' content1  content2', got %q", content)
	}
	if reasoning != "think1think2" {
		t.Errorf("expected 'think1think2', got %q", reasoning)
	}
}

func TestConsumeThinkTaggedText_MissingCloseTag_Progressive(t *testing.T) {
	state := CreateThinkTagParserState()
	content, reasoning := ConsumeThinkTaggedText(state, thinkOpen+"never closes")
	if content != "" {
		t.Errorf("expected empty content, got %q", content)
	}
	if reasoning != "never closes" {
		t.Errorf("expected progressive reasoning 'never closes', got %q", reasoning)
	}
	if state.Mode != "reasoning" {
		t.Errorf("expected reasoning mode, got %q", state.Mode)
	}
}

func TestConsumeThinkTaggedText_MinimaxOrphanClose(t *testing.T) {
	state := CreateThinkTagParserState()
	content, reasoning := ConsumeThinkTaggedText(state, "internal chain"+thinkClose+"visible answer")
	if reasoning != "internal chain" {
		t.Errorf("expected orphan reasoning, got %q", reasoning)
	}
	if content != "visible answer" {
		t.Errorf("expected content after orphan close, got %q", content)
	}
}

func TestExtractInlineThinkTags_MinimaxOrphanClose(t *testing.T) {
	got := ExtractInlineThinkTags("internal chain" + thinkClose + "visible answer")
	if got.Reasoning != "internal chain" {
		t.Errorf("reasoning=%q", got.Reasoning)
	}
	if got.Content != "visible answer" {
		t.Errorf("content=%q", got.Content)
	}
}

func TestExtractReasoningDetailsText_MinimaxShape(t *testing.T) {
	raw := []any{
		map[string]any{"type": "reasoning.text", "text": "step one"},
		map[string]any{"text": "step two"},
	}
	got := ExtractReasoningDetailsText(raw)
	if got == "" {
		t.Fatal("expected non-empty reasoning details text")
	}
	if !containsStr(got, "step one") || !containsStr(got, "step two") {
		t.Fatalf("got %q", got)
	}
}

func containsStr(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOfStr(s, sub) >= 0)
}

func indexOfStr(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestFlushThinkTaggedText_ReasoningMode_SeededPending(t *testing.T) {
	state := CreateThinkTagParserState()
	ConsumeThinkTaggedText(state, thinkOpen+"pending text")
	if state.Mode != "reasoning" {
		t.Fatal("expected reasoning mode")
	}
	// Progressive parser may have already emitted text; seed pending for flush path.
	state.Pending = "pending text"

	content, reasoning := FlushThinkTaggedText(state)
	if content != "" {
		t.Errorf("expected empty content, got %q", content)
	}
	if reasoning != "pending text" {
		t.Errorf("expected 'pending text', got %q", reasoning)
	}
	if state.Mode != "content" {
		t.Errorf("expected content mode after flush, got %q", state.Mode)
	}
}
