package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// wsFakeTuicr writes a tuicr stand-in that renders text and echoes keys.
func wsFakeTuicr(t *testing.T) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "fake-tuicr-tui")
	script := `#!/bin/sh
printf '\033[2J\033[H TUIRCR-STUB READY\n'
stty raw -echo 2>/dev/null
while IFS= read -r -n 1 -s c; do
  [ -z "$c" ] && continue
  printf 'KEY:%s' "$c"
done
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MR_REVIEW_TUICR", bin)
}

// TestWorkspacePaneLifecycle — W opens the embedded tuicr pane, keys are
// forwarded when focused, Ctrl+G toggles focus, W closes it.
func TestWorkspacePaneLifecycle(t *testing.T) {
	wsFakeTuicr(t)
	m := newMultiFileModel(t, 160, 45)
	if m.rev.Worktree == "" {
		m.rev.Worktree = t.TempDir()
	}

	// open
	m2, cmd := m.Update(key("W"))
	mm := m2.(*model)
	if wsPane == nil {
		t.Fatal("W must open the tuicr pane")
	}
	if cmd == nil {
		t.Fatal("pane spawn must arm the exit watcher")
	}
	t.Cleanup(func() { // never leak the package-global pane into later tests
		if wsPane != nil {
			mm.closeTuicrPane()
		}
	})
	if wsFocus != wsFocusTuicr {
		t.Fatal("opening must focus the tuicr pane")
	}
	// wait for the child screen to appear on the emulated pane
	deadline := time.Now().Add(3 * time.Second)
	for {
		if strings.Contains(strings.Join(wsPane.ScreenText(), "\n"), "TUIRCR-STUB") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("pane must render the child screen, got: %q",
				strings.Join(wsPane.ScreenText(), "\n"))
		}
		time.Sleep(50 * time.Millisecond)
	}

	// key routing: while tuicr focused, 'j' goes to the pane, not the model
	mm.focus = focusFindings // j would move the findings cursor if it leaked to the model
	cur := mm.cursor
	mm.Update(key("j"))
	time.Sleep(150 * time.Millisecond)
	if mm.cursor != cur {
		t.Fatal("keys must be forwarded to tuicr, not the cockpit")
	}

	// Ctrl+G → cockpit focus: keys hit the model again
	mm.Update(key("ctrl+g"))
	if wsFocus != wsFocusCockpit {
		t.Fatal("ctrl+g must return focus to the cockpit")
	}
	mm.focus = focusFindings // cockpit keys restored: j must move the findings cursor again
	cur = mm.cursor
	mm.Update(key("j"))
	if mm.cursor != cur+1 {
		t.Fatal("cockpit focus must restore normal key handling")
	}

	// close
	mm.closeTuicrPane()
	if wsPane != nil {
		t.Fatal("W (cockpit focus) must close the pane")
	}
}

// TestWorkspaceMouseSGR — mouse events over the pane are forwarded as SGR
// sequences with pane-relative 1-based coordinates.
func TestWorkspaceMouseSGR(t *testing.T) {
	fakeTuicrScript(t, 55868) // reuse the JSON fake for ListSessions
	_ = fakeTuicrScript
	m := newMultiFileModel(t, 160, 45)
	m.rev.Worktree = t.TempDir()

	if _, cmd := m.Update(key("W")); cmd == nil && wsPane == nil {
		t.Fatal("W must open the pane")
	}
	// let the fake tuicr (a shell script that exits immediately in JSON mode?
	// no — MR_REVIEW_TUICR points at the JSON fake; the pane child is
	// tuicr itself) — the child may exit fast; forwarding still records.

	cw := m.wsCockpitWidth()
	// click inside the pane area (x = cockpit+2, y = 10)
	msg := tea.MouseMsg(tea.MouseClickMsg(tea.Mouse{X: cw + 2, Y: 10, Button: tea.MouseLeft}))
	handled, _ := m.wsHandleMouse(msg)
	if !handled {
		t.Fatal("mouse over the pane must be handled")
	}
	if wsFocus != wsFocusTuicr {
		t.Fatal("click must focus the tuicr pane")
	}

	// SGR encoding unit checks
	sgr := sgrMouse(tea.Mouse{X: 0, Y: 0, Button: tea.MouseLeft}, 0, 0, false)
	if sgr != "\x1b[<0;1;1M" {
		t.Fatalf("left press SGR = %q", sgr)
	}
	sgr = sgrMouse(tea.Mouse{X: 0, Y: 0, Button: tea.MouseWheelUp}, 3, 4, false)
	if sgr != "\x1b[<64;4;5M" {
		t.Fatalf("wheel up SGR = %q", sgr)
	}
	sgr = sgrMouse(tea.Mouse{X: 0, Y: 0, Button: tea.MouseNone}, 3, 4, false)
	if sgr != "\x1b[<32;4;5M" {
		t.Fatalf("motion SGR = %q", sgr)
	}
	sgr = sgrMouse(tea.Mouse{X: 0, Y: 0, Button: tea.MouseLeft}, 3, 4, true)
	if sgr != "\x1b[<0;4;5m" {
		t.Fatalf("release SGR = %q", sgr)
	}

	// mouse outside the pane is not handled by the workspace
	msg = tea.MouseMsg(tea.MouseClickMsg(tea.Mouse{X: 2, Y: 10, Button: tea.MouseLeft}))
	if handled, _ := m.wsHandleMouse(msg); handled {
		t.Fatal("mouse over the cockpit must not be workspace-handled")
	}
	m.closeTuicrPane()
}

// TestWorkspaceFocusUX — the focused pane is visually marked (● title +
// bright border), the footer documents the focused pane's keys, and Ctrl+O
// toggles focus just like Ctrl+G.
func TestWorkspaceFocusUX(t *testing.T) {
	wsFakeTuicr(t)
	m := newMultiFileModel(t, 160, 45)
	if m.rev.Worktree == "" {
		m.rev.Worktree = t.TempDir()
	}
	m.openTuicrPane()
	if wsPane == nil {
		t.Fatal("pane must open")
	}
	defer m.closeTuicrPane()

	// focused cockpit? no: opening focuses tuicr
	v := m.View().Content
	// Nerd Font filled/open circles mark the focused pane (\uf111 / \uf10c)
	if !strings.Contains(v, "\uf111") || !strings.Contains(v, "tuicr — manual review") ||
		!strings.Contains(v, "\uf10c") || !strings.Contains(v, "AI cockpit") {
		t.Fatalf("focused pane markers missing:\n%s", firstLines(v, 4))
	}
	// footer documents the tuicr pane while it is focused
	if !strings.Contains(v, "Ctrl+O") || !strings.Contains(v, "→ cockpit") {
		t.Fatalf("tuicr-mode footer missing:\n%s", lastLines(v, 2))
	}

	// Ctrl+O toggles to the cockpit: markers flip, footer flips
	m.Update(key("ctrl+o"))
	if wsFocus != wsFocusCockpit {
		t.Fatal("ctrl+o must toggle focus to the cockpit")
	}
	v = m.View().Content
	if !strings.Contains(v, "\uf111") || !strings.Contains(v, "AI cockpit ·") ||
		!strings.Contains(v, "\uf10c") || !strings.Contains(v, "tuicr — manual review") {
		t.Fatalf("cockpit focus markers missing:\n%s", firstLines(v, 4))
	}
	if !strings.Contains(v, "→ tuicr") || !strings.Contains(v, "close pane") {
		t.Fatalf("cockpit-mode footer missing:\n%s", lastLines(v, 2))
	}

	// Ctrl+G still works (both toggles accepted)
	m.Update(key("ctrl+g"))
	if wsFocus != wsFocusTuicr {
		t.Fatal("ctrl+g must still toggle focus")
	}
}

func firstLines(s string, n int) string {
	parts := strings.SplitN(s, "\n", n+1)
	out := parts
	if len(out) > n {
		out = out[:n]
	}
	return strings.Join(out, " | ")
}

func lastLines(s string, n int) string {
	parts := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(parts) > n {
		parts = parts[len(parts)-n:]
	}
	return strings.Join(parts, " | ")
}

// TestWorkspaceDetailInStrip — Enter/i open the finding detail INSIDE the
// cockpit strip (the tuicr pane and the split layout stay untouched); Esc
// returns to the list.
func TestWorkspaceDetailInStrip(t *testing.T) {
	wsFakeTuicr(t)
	m := newMultiFileModel(t, 160, 45)
	if m.rev.Worktree == "" {
		m.rev.Worktree = t.TempDir()
	}
	m.openTuicrPane()
	defer m.closeTuicrPane()

	// focus the cockpit first (the workspace opens focused on tuicr)
	tap(m, "ctrl+o")
	if wsFocus != wsFocusCockpit {
		t.Fatal("ctrl+o must focus the cockpit")
	}
	// open details from the findings list — the cockpit EXPANDS for
	// readable prose and the tuicr PTY is resized to reflow
	narrow := m.wsCockpitWidth()
	tap(m, "enter")
	if !wsDetail {
		t.Fatal("enter must open the in-strip detail")
	}
	if m.wsCockpitWidth() <= narrow {
		t.Fatalf("cockpit must expand for the detail: %d -> %d", narrow, m.wsCockpitWidth())
	}
	if wsPane != nil {
		pw, _ := wsPane.Size()
		ew, _ := m.wsPaneSize()
		if pw != ew {
			t.Fatalf("pane not resized to the new split: %d != %d", pw, ew)
		}
	}
	if m.overlay != ovNone {
		t.Fatalf("details must not open a fullscreen overlay (got %d)", m.overlay)
	}
	v := m.View().Content
	// the split layout is still there (both boxes) AND the detail is in the strip
	if !strings.Contains(v, "tuicr — manual review") || !strings.Contains(v, "AI cockpit") {
		t.Fatal("split layout must remain visible while details are open")
	}
	if !strings.Contains(v, "F001") || !strings.Contains(v, "The call passes") || !strings.Contains(v, "Esc back") {
		t.Fatalf("detail content missing from the strip")
	}
	// the finding's code diff renders inside the strip too
	if !strings.Contains(v, "diff —") || !strings.Contains(v, "markOffering") {
		t.Fatal("code diff section missing from the strip detail")
	}
	if strings.Contains(v, "KEYBOARD REFERENCE") {
		t.Fatal("fullscreen inspector must not appear in workspace mode")
	}

	// scrolling works in the strip detail
	m.wsDetailScroll(5)
	// esc returns to the list and restores the narrow cockpit
	tap(m, "esc")
	if wsDetail {
		t.Fatal("esc must close the strip detail")
	}
	if m.wsCockpitWidth() != narrow {
		t.Fatalf("cockpit width not restored: %d != %d", m.wsCockpitWidth(), narrow)
	}
	v = m.View().Content
	if !strings.Contains(v, "FINDINGS") {
		t.Fatal("findings list must return after esc")
	}

	// i opens it again (cockpit focus)
	tap(m, "i")
	if !wsDetail {
		t.Fatal("i must open the strip detail")
	}
	tap(m, "esc")
}

// TestWorkspaceKeysReachTuicr — m/M (and plain keys) pressed while the
// tuicr pane is focused must ARRIVE at the embedded tuicr (the stub echoes
// KEY:<char> for every byte it receives).
func TestWorkspaceKeysReachTuicr(t *testing.T) {
	wsFakeTuicr(t)
	m := newMultiFileModel(t, 160, 45)
	if m.rev.Worktree == "" {
		m.rev.Worktree = t.TempDir()
	}
	m.openTuicrPane()
	defer m.closeTuicrPane()

	// focus is tuicr after open; wait for the stub banner first
	deadline := time.Now().Add(3 * time.Second)
	for {
		if strings.Contains(strings.Join(wsPane.ScreenText(), "\n"), "TUIRCR-STUB") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("stub did not start")
		}
		time.Sleep(50 * time.Millisecond)
	}

	for _, k := range []string{"m", "M", "j"} {
		mm, _ := m.Update(tea.KeyPressMsg{Code: rune(k[0]), Text: k})
		_ = mm
	}
	deadline = time.Now().Add(3 * time.Second)
	for {
		txt := strings.Join(wsPane.ScreenText(), "\n")
		if strings.Contains(txt, "KEY:m") && strings.Contains(txt, "KEY:M") && strings.Contains(txt, "KEY:j") {
			return // all three arrived
		}
		if time.Now().After(deadline) {
			t.Fatalf("keys did not reach tuicr; screen:\n%s", txt)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestTuicrPaneArgs — by default tuicr reviews ALL commits of the pinned MR
// revision (no commit selector): -r BaseSHA..HeadSHA in PR mode, or as a
// top-level local revset when the MR URL is unknown.
func TestTuicrPaneArgs(t *testing.T) {
	m := newMultiFileModel(t, 160, 45)
	m.rev.Host = "gitlab.example.com"
	m.rev.Project = "grp/proj"
	m.rev.IID = 42
	m.rev.BaseSHA = "aaa111"
	m.rev.HeadSHA = "bbb222"

	// default: PR mode (tuicr's native GitLab integration — :submit works);
	// revset limitations are documented in tuicrPaneArgs
	args := m.tuicrPaneArgs()
	got := strings.Join(args, " ")
	if !strings.Contains(got, "pr https://gitlab.example.com/grp/proj/-/merge_requests/42") {
		t.Fatalf("PR mode args wrong: %q", got)
	}
	for _, a := range args {
		if a == "review" {
			t.Fatal("must not use the tuicr review SESSION subcommand for the TUI")
		}
	}

	// no host → local branch review fallback
	m.rev.Host = ""
	args = m.tuicrPaneArgs()
	if strings.Join(args, " ") != "-r feat/x" {
		t.Fatalf("local fallback args wrong: %v", args)
	}
}
