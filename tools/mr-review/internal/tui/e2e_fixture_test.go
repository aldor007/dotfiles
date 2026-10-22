package tui

// e2e fixture: a deterministic, fully on-disk review (review.json + findings
// + diff + comments) that both the in-process action tests and the vttest
// binary tests open as a real session. No GitLab, no agent — nothing here
// ever touches the network.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mr-review/internal/store"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

const fixtureDiff = `diff --git a/services/kent/handler.go b/services/kent/handler.go
index 1111111..2222222 100644
--- a/services/kent/handler.go
+++ b/services/kent/handler.go
@@ -10,6 +10,7 @@ func handle(ctx context.Context) error {
 	req := parse(r)
-	call(ctx, req)
+	ctx = withTimeout(ctx, 0)
+	call(context.WithoutCancel(ctx), req)
 	return nil
 }
diff --git a/services/kent/worker.go b/services/kent/worker.go
index 3333333..4444444 100644
--- a/services/kent/worker.go
+++ b/services/kent/worker.go
@@ -28,5 +28,6 @@ func (w *Worker) run() {
 	for {
 		select {
 		case <-w.done:
-			return
+			w.cleanup(ctx)
+			return
 		}
 	}
 }
`

// writeReviewFixture creates a complete review dir under parent/mr7.
func writeReviewFixture(t *testing.T, parent string) string {
	t.Helper()
	dir := filepath.Join(parent, "grp_proj", "mr7")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "diff.patch"), []byte(fixtureDiff), 0o600); err != nil {
		t.Fatal(err)
	}
	rev := &store.Review{
		Project: "grp/proj", Host: "gitlab.example.com", IID: 7,
		Title: "Add ctx propagation", Author: "matas",
		SourceBranch: "feat/ctx", TargetBranch: "main",
		HeadSHA: "fixed0sha123456", BaseSHA: "base0sha000000",
		State: "reviewing", CreatedAt: "2026-10-21T10:00:00Z",
	}
	if err := rev.Save(dir); err != nil {
		t.Fatal(err)
	}
	findings := []store.Finding{
		{
			ID: "F001", File: "services/kent/handler.go", Side: "new",
			Line: 12, LineEnd: 13, Severity: "P1", Kind: "bug",
			Title:       "ctx propagation dropped",
			Explanation: "The handler rebuilds the context without propagating\nthe deadline. Callers with a timeout get an unbounded call.",
			Evidence:    "services/kent/handler.go:12 — context.WithoutCancel discards the deadline set by the caller.",
			Comment:     "**Bug:** the deadline is dropped here — propagate `ctx` instead of `context.WithoutCancel(ctx)`.",
			Confidence:  "high", Decision: "pending",
		},
		{
			ID: "F002", File: "services/kent/worker.go", Side: "new",
			Line: 31, Severity: "P2", Kind: "likely",
			Title:       "cleanup runs after done close",
			Explanation: "cleanup is invoked on the done path; verify it is idempotent\nsince done may be closed twice.",
			Evidence:    "services/kent/worker.go:31 — w.cleanup(ctx) on the done branch.",
			Comment:     "Is `cleanup` idempotent? `done` can be closed more than once.",
			Confidence:  "medium", Decision: "pending",
		},
	}
	if err := store.SaveFindings(dir, findings); err != nil {
		t.Fatal(err)
	}
	return dir
}

// newSessionModel builds the review model exactly like Run() does before
// starting the program — the in-process seam for e2e-style tests.
func newSessionModel(t *testing.T, dir string) *model {
	t.Helper()
	rev, err := store.LoadReview(dir)
	if err != nil {
		t.Fatalf("LoadReview: %v", err)
	}
	findings, err := store.LoadFindings(dir)
	if err != nil {
		t.Fatalf("LoadFindings: %v", err)
	}
	ti := textinput.New()
	ti.Placeholder = "ask the agent about this finding…"
	ti.CharLimit = 500
	ti.Prompt = "ask> "

	ta := textarea.New()
	ta.Placeholder = "The GitLab comment text…"
	ta.MaxHeight = 12

	search := textinput.New()
	search.Placeholder = "search…  (enter keep · esc clear)"
	search.Prompt = "/ "

	pal := textinput.New()
	pal.Placeholder = "type a command…"
	pal.Prompt = ": "
	pal.CharLimit = 100

	comments, _ := store.LoadComments(dir)
	sp := spinner.New(spinner.WithSpinner(spinner.Dot))

	m := &model{
		dir: dir, rev: rev, findings: findings, comments: comments,
		filter: Filter{Kind: "all"}, input: ti, commentTA: ta, spin: sp,
		ctxLines: 6, searchInput: search, paletteInput: pal,
		visualStart: -1, width: 120, height: 35, narrow: false,
	}
	m.rebuild()
	return m
}

// teaKeyPress maps a registry key string to a Bubble Tea v2 key event,
// exactly the way the terminal would deliver it.
func teaKeyPress(k string) tea.KeyPressMsg {
	ctrl := func(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl} }
	switch k {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "ctrl+c":
		return ctrl('c')
	case "ctrl+d":
		return ctrl('d')
	case "ctrl+u":
		return ctrl('u')
	case "ctrl+h":
		return ctrl('h')
	case "ctrl+l":
		return ctrl('l')
	case "ctrl+s":
		return ctrl('s')
	case "ctrl+n":
		return ctrl('n')
	case "ctrl+p":
		return ctrl('p')
	}
	r := []rune(k)
	if len(r) == 1 {
		return tea.KeyPressMsg{Code: r[0], Text: k}
	}
	return tea.KeyPressMsg{Text: k}
}

// namedKeys are multi-letter strings delivered as ONE key event — they
// must never be split into per-rune presses.
var namedKeys = map[string]bool{
	"esc": true, "enter": true, "tab": true, "shift+tab": true,
	"up": true, "down": true, "left": true, "right": true,
	"home": true, "end": true, "pgup": true, "pgdown": true,
	"delete": true, "backspace": true, "space": true,
}

// splitKeys breaks a binding into individual presses ("gg" → g, g);
// named keys and modifier combos stay single ("tab", "ctrl+d").
func splitKeys(k string) []string {
	if namedKeys[k] || strings.HasPrefix(k, "ctrl+") || strings.HasPrefix(k, "shift+") || len([]rune(k)) == 1 {
		return []string{k}
	}
	out := make([]string, 0, len(k))
	for _, r := range k {
		out = append(out, string(r))
	}
	return out
}

// press sends keys through the real Update path (the registry dispatch),
// splitting multi-key sequences the way a user types them ("gg" → g, g).
func press(m *model, keys ...string) *model {
	for _, k := range keys {
		for _, part := range splitKeys(k) {
			out, _ := m.Update(teaKeyPress(part))
			if mm, ok := out.(*model); ok {
				m = mm
			}
		}
	}
	return m
}
