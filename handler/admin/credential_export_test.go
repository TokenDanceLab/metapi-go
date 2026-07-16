package admin

import (
	"strings"
	"testing"
)

func TestBuildCredentialExportProfiles_Content(t *testing.T) {
	ps := buildCredentialExportProfiles("n", "sk-abc", "https://gw.test")
	if len(ps) != 3 {
		t.Fatalf("len=%d", len(ps))
	}
	if ps[0]["id"] != "openai" {
		t.Fatalf("%v", ps[0])
	}
	content, _ := ps[0]["content"].(string)
	if !strings.Contains(content, "OPENAI_BASE_URL") || !strings.Contains(content, "sk-abc") {
		t.Fatalf("content=%q", content)
	}
	cherry, _ := ps[1]["content"].(map[string]any)
	if cherry["apiKey"] != "sk-abc" {
		t.Fatalf("cherry=%v", cherry)
	}
}
