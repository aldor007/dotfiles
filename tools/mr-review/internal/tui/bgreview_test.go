package tui

import (
	"os"
	"testing"

	"mr-review/internal/store"
)

// bgWriteState writes an agent.state heartbeat for the test review dir.
func bgWriteState(t *testing.T, dir string, s store.AgentState) {
	t.Helper()
	store.WriteAgentState(dir, s)
}

// TestBGRunningThenDone — the TUI tracks the heartbeat: running shows in the
// status, the done transition reloads findings into the list.
func TestBGRunningThenDone(t *testing.T) {
	m := newMultiFileModel(t, 120, 35) // no findings yet
	bgWriteState(t, m.dir, store.AgentState{
		Status: store.AgentRunning, PID: os.Getpid(), // our own pid: alive
		Raw: 3,
	})
	if reload := m.refreshBGState(); reload {
		t.Fatal("running state must not trigger a reload")
	}
	if !m.bgRunning() {
		t.Fatal("bg review must be tracked as running")
	}
	if line := m.bgStatusLine(); line == "" || !contains(line, "running in background") {
		t.Fatalf("status line wrong: %q", line)
	}

	// the agent finishes and findings are ingested
	fs := mockFindings(2)
	if err := store.SaveFindings(m.dir, fs); err != nil {
		t.Fatal(err)
	}
	bgWriteState(t, m.dir, store.AgentState{
		Status: store.AgentDone, PID: os.Getpid(), Findings: 2, Raw: 2,
	})
	if reload := m.refreshBGState(); !reload {
		t.Fatal("running → done must trigger a reload")
	}
	if err := m.reload(); err != nil {
		t.Fatal(err)
	}
	if len(m.findings) != 2 {
		t.Fatalf("findings not loaded: %d", len(m.findings))
	}
}

// TestBGStalePID — a running heartbeat whose process died is interrupted.
func TestBGStalePID(t *testing.T) {
	m := newMultiFileModel(t, 120, 35)
	bgWriteState(t, m.dir, store.AgentState{Status: store.AgentRunning, PID: 999_999_999})
	m.refreshBGState()
	if m.bgRunning() {
		t.Fatal("dead pid must not read as running")
	}
	if got := m.bgReview.Status; got != store.AgentInterrupted {
		t.Fatalf("status = %q, want interrupted", got)
	}
}

// TestBGReloadDeferredWhileEditing — a finished review must NOT swap the
// findings slice out from under an open editor; the reload applies on close.
func TestBGReloadDeferredWhileEditing(t *testing.T) {
	m := newTestModel(t, 120, 35, nil) // empty review: nothing loaded yet
	m.editingComment = true            // editor open (holds a pointer into m.findings)

	bgWriteState(t, m.dir, store.AgentState{Status: store.AgentDone, PID: os.Getpid(), Findings: 2})
	if err := store.SaveFindings(m.dir, mockFindings(2)); err != nil {
		t.Fatal(err)
	}
	if reload := m.refreshBGState(); !reload {
		t.Fatal("done transition must request a reload")
	}
	// simulate the Update handler's deferral
	if m.editingComment {
		m.pendingReload = true
	}
	if err := m.reload(); err != nil {
		t.Fatal(err)
	}
	if m.pendingReload == false {
		t.Fatal("reload must be deferred while editing")
	}
	if len(m.findings) != 0 {
		t.Fatalf("findings must NOT be swapped under the editor: %d loaded", len(m.findings))
	}
	// close the editor → deferred reload applies
	m.editingComment = false
	m.bgPendingReload()
	if len(m.findings) != 2 {
		t.Fatalf("deferred findings not loaded: %d", len(m.findings))
	}
	if m.pendingReload {
		t.Fatal("pending flag must clear after the reload")
	}
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
