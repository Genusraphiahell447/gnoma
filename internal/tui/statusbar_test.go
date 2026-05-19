package tui

import (
	"fmt"
	"strings"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/message"
	"somegit.dev/Owlibou/gnoma/internal/session"
)

func TestRenderContextBar_Zero(t *testing.T) {
	s := session.Status{TokensUsed: 0, TokenPercent: 0}
	got := renderContextBar(s)
	if !strings.Contains(got, "—") {
		t.Errorf("zero usage should show dash, got %q", got)
	}
}

func TestRenderContextBar_Half(t *testing.T) {
	s := session.Status{TokensUsed: 50000, TokenPercent: 50, TokenState: "ok"}
	got := renderContextBar(s)
	if !strings.Contains(got, "50%") {
		t.Errorf("should contain 50%%, got %q", got)
	}
	// 8-wide bar at 50% → 4 filled + 4 empty
	if strings.Count(got, "█") != 4 {
		t.Errorf("expected 4 filled blocks, got %d in %q", strings.Count(got, "█"), got)
	}
	if strings.Count(got, "░") != 4 {
		t.Errorf("expected 4 empty blocks, got %d in %q", strings.Count(got, "░"), got)
	}
}

func TestRenderContextBar_Full(t *testing.T) {
	s := session.Status{TokensUsed: 100000, TokenPercent: 100, TokenState: "critical"}
	got := renderContextBar(s)
	if !strings.Contains(got, "100%") {
		t.Errorf("should contain 100%%, got %q", got)
	}
	if strings.Count(got, "█") != 8 {
		t.Errorf("expected 8 filled blocks at 100%%, got %d", strings.Count(got, "█"))
	}
}

func TestRenderContextBar_Warning(t *testing.T) {
	s := session.Status{TokensUsed: 70000, TokenPercent: 75, TokenState: "warning"}
	got := renderContextBar(s)
	if !strings.Contains(got, "75%") {
		t.Errorf("should contain 75%%, got %q", got)
	}
}

func TestRenderProfileBadge_Legacy(t *testing.T) {
	got := renderProfileBadge(ProfileInfo{})
	if got != "" {
		t.Errorf("inactive profile should render nothing, got %q", got)
	}
}

func TestRenderProfileBadge_Active(t *testing.T) {
	got := renderProfileBadge(ProfileInfo{Active: true, Name: "work"})
	if !strings.Contains(got, "profile: work") {
		t.Errorf("active badge should show 'profile: work', got %q", got)
	}
}

func TestRenderProfileBadge_ActiveButNameEmpty(t *testing.T) {
	// Defensive: never render a badge when Name happens to be empty
	// even if Active was somehow set true.
	got := renderProfileBadge(ProfileInfo{Active: true})
	if got != "" {
		t.Errorf("active+empty name should render nothing, got %q", got)
	}
}

func TestFormatTurnUsage_Basic(t *testing.T) {
	u := message.Usage{InputTokens: 1500, OutputTokens: 200}
	got := formatTurnUsage(u)
	if !strings.Contains(got, "in: 1500") {
		t.Errorf("should contain input tokens, got %q", got)
	}
	if !strings.Contains(got, "out: 200") {
		t.Errorf("should contain output tokens, got %q", got)
	}
	if strings.Contains(got, "cache") {
		t.Errorf("should not mention cache when zero, got %q", got)
	}
}

func TestFormatTurnUsage_WithCache(t *testing.T) {
	u := message.Usage{InputTokens: 5000, OutputTokens: 800, CacheReadTokens: 3000}
	got := formatTurnUsage(u)
	if !strings.Contains(got, "cache: 3000") {
		t.Errorf("should contain cache tokens, got %q", got)
	}
}

func TestDiffPreviewEdit(t *testing.T) {
	got := diffPreviewEdit("old line", "new line")
	if !strings.Contains(got, "- old line") {
		t.Errorf("should show removed line, got %q", got)
	}
	if !strings.Contains(got, "+ new line") {
		t.Errorf("should show added line, got %q", got)
	}
}

func TestDiffPreviewWrite(t *testing.T) {
	content := "line1\nline2\nline3"
	got := diffPreviewWrite(content)
	if !strings.Contains(got, "+ line1") {
		t.Errorf("should show first line, got %q", got)
	}
}

func TestDiffPreviewWrite_TruncatesLong(t *testing.T) {
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i+1)
	}
	got := diffPreviewWrite(strings.Join(lines, "\n"))
	if !strings.Contains(got, "more lines") {
		t.Errorf("should truncate with '...more lines', got %q", got)
	}
}
