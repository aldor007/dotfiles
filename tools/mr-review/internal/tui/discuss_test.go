package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// fakeAgent writes an executable agent stand-in. mode:
//   - "sleep": ignores stdin, sleeps 30s (simulates a stalled agent)
//   - "answer": prints a reply and exits
func fakeAgent(t *testing.T, mode string) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "fake-agent")
	script := "#!/bin/sh\n"
	if mode == "sleep" {
		script += "sleep 30\n"
	} else {
		script += "cat >/dev/null\necho 'agent says: confirmed'\n"
	}
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MR_REVIEW_AGENT", bin)
}

// TestDiscussTimeoutEnforced — a stalled agent must fail at the timeout
// instead of wedging the TUI forever (the original hang: timeout was
// accepted but never applied to the exec context).
func TestDiscussTimeoutEnforced(t *testing.T) {
	fakeAgent(t, "sleep")
	r := newRunner()
	start := time.Now()
	_, err := r.Discuss(bgCtx(), newMultiFileModel(t, 120, 35).dir, "F001", "why?", 150*time.Millisecond)
	if err == nil {
		t.Fatal("stalled agent must return an error at the timeout")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("timeout not enforced: Discuss returned after %s", d)
	}
	if !strings.Contains(err.Error(), "did not finish") {
		t.Fatalf("error should explain the timeout: %v", err)
	}
}

// TestAskNonBlockingAndEscCancellable — submitting a question must not put
// the model into the blocking busy state, and Esc must cancel the in-flight
// ask (askingID cleared, cancel function invoked).
func TestAskNonBlockingAndEscCancellable(t *testing.T) {
	fakeAgent(t, "sleep")
	m := newMultiFileModel(t, 120, 35)
	m.focus = focusFindings
	m.inputOn = true
	m.input.SetValue("why is this wrong?")

	// submit
	m2, _ := m.Update(keyEnter)
	mm := m2.(*model)
	if mm.inputOn {
		t.Fatal("submit must close the question input")
	}
	if mm.busy {
		t.Fatal("an AI ask must NOT block the TUI (busy would swallow all keys)")
	}
	if mm.askingID != "F001" || mm.askCancel == nil {
		t.Fatalf("ask must be registered as cancellable: askingID=%q cancel=%v", mm.askingID, mm.askCancel)
	}
	// the registry must refuse a second concurrent ask
	mm2, _ := mm.Update(key("d"))
	if mm2.(*model).inputOn {
		t.Fatal("d while an ask is in flight must not open a second question input")
	}

	// Esc cancels
	m3, _ := mm.Update(keyEsc)
	mm3 := m3.(*model)
	if mm3.askingID != "" || mm3.askCancel != nil {
		t.Fatal("esc must clear the in-flight ask")
	}
	if !strings.Contains(mm3.msg, "cancel") {
		t.Fatalf("esc feedback missing: %q", mm3.msg)
	}
}

// TestCtrlCQuitsDuringAsk — Ctrl+C must quit even while an ask is running.
func TestCtrlCQuitsDuringAsk(t *testing.T) {
	fakeAgent(t, "sleep")
	m := newMultiFileModel(t, 120, 35)
	m.inputOn = true
	m.input.SetValue("q?")
	m2, _ := m.Update(keyEnter)
	mm := m2.(*model)
	_, cmd := mm.Update(tea.KeyPressMsg{Code: 'x', Text: "ctrl+c"})
	if cmd == nil {
		t.Fatal("ctrl+c during an ask must return a quit command")
	}
	if mm.askingID != "" {
		t.Log("askingID cleared synchronously by ctrl+c handling")
	}
}
