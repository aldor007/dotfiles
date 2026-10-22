package widget

import (
	"reflect"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestRenderHintsJoinsAndCuts(t *testing.T) {
	hints := []Hint{{"j/k", "findings"}, {"a", "approve"}, {":", ""}}
	got := RenderHints(hints, 0)
	want := "j/k findings · a approve · :"
	if got != want {
		t.Fatalf("RenderHints = %q, want %q", got, want)
	}
	cut := RenderHints(hints, 10)
	if lipgloss.Width(cut) > 10 {
		t.Errorf("RenderHints(w=10) width = %d", lipgloss.Width(cut))
	}
}

func TestPrefixHints(t *testing.T) {
	keys := []string{"gg", "gd", "gn", "j", "k", "G", "gg"} // dup gg
	got := PrefixHints("g", keys)
	want := []Hint{{"gg", ""}, {"gd", ""}, {"gn", ""}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("PrefixHints = %v, want %v", got, want)
	}
	if got := PrefixHints("z", keys); len(got) != 0 {
		t.Fatalf("no-match prefix returned %v", got)
	}
	if got := PrefixHints("j", keys); len(got) != 0 {
		t.Fatalf("exact-key prefix (not a prefix of a longer key) returned %v", got)
	}
}

func TestRenderStatusLine(t *testing.T) {
	bold := lipgloss.NewStyle().Bold(true)
	segs := []StatusSegment{
		{Text: "Status: "},
		{Text: "review complete", Style: &bold},
	}
	got := RenderStatusLine("", segs, 0, nil)
	if got == "" {
		t.Fatal("empty render")
	}
	// injected truncation is honored
	cut := RenderStatusLine("", segs, 5, func(s string, w int) string {
		if lipgloss.Width(s) > w {
			return s[:w]
		}
		return s
	})
	if lipgloss.Width(cut) > 5 {
		t.Errorf("truncation not applied: %q", cut)
	}
}

func TestSplitProgress(t *testing.T) {
	cases := map[[2]int]string{{0, 0}: "0/0", {3, 12}: "3/12", {12, 12}: "12/12"}
	for in, want := range cases {
		if got := SplitProgress(in[0], in[1]); got != want {
			t.Errorf("SplitProgress(%d,%d) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

func TestTabBarFitsWidth(t *testing.T) {
	names := []string{"DIFF", "EXPL", "EVID", "COMM", "DISC"}
	s := TabBarStyles{
		Border: lipgloss.NewStyle(), BorderFocus: lipgloss.NewStyle(),
		Dim: lipgloss.NewStyle(), Active: lipgloss.NewStyle(), Cursor: lipgloss.NewStyle(),
	}
	rows := RenderTabBar(names, 0, true, false, 120, s)
	if len(rows) != 3 {
		t.Fatalf("tab bar rows = %d", len(rows))
	}
	for i, r := range rows {
		if lipgloss.Width(r) != 120 {
			t.Errorf("row %d width = %d, want 120", i, lipgloss.Width(r))
		}
	}
	// plain mode marks the active tab with brackets
	plainRows := RenderTabBar(names, 1, false, true, 120, s)
	if !containsStr(plainRows[1], "[EXPL]") {
		t.Errorf("plain active tab missing brackets: %q", plainRows[1])
	}
	// very narrow: last label drops to "…"
	narrow := RenderTabBar(names, 0, true, false, 20, s)
	if !containsStr(narrow[1], "…") {
		t.Errorf("narrow bar should drop last label to …: %q", narrow[1])
	}
}

func TestTabBarWidth(t *testing.T) {
	// 1 + (1+4+1) + (2+4+1) = 14
	if w := TabBarWidth([]string{"A", "BB"}); w != 14 {
		t.Errorf("TabBarWidth = %d, want 14", w)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
