// agentstate.go — heartbeat of the background AI review (agent.state): the
// review is spawned detached, the TUI polls this file for progress and the
// final ingest result. Purely local; nothing here talks to GitLab.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// AgentState statuses.
const (
	AgentRunning     = "running"
	AgentDone        = "done"
	AgentFailed      = "failed"
	AgentInterrupted = "interrupted"
)

// AgentState is the on-disk heartbeat of a background AI review.
type AgentState struct {
	Status     string `json:"status"` // running | done | failed | interrupted
	PID        int    `json:"pid"`
	JobID      string `json:"job_id"`             // execution id (revision + start time)
	HeadSHA    string `json:"head_sha,omitempty"` // pinned MR revision the job analyzes
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at,omitempty"`
	Findings   int    `json:"findings"` // validated findings after ingest
	Raw        int    `json:"raw"`      // raw findings appended so far
	Error      string `json:"error,omitempty"`
}

// AgentStatePath returns the agent.state path for a review dir.
func AgentStatePath(dir string) string { return filepath.Join(dir, "agent.state") }

// WriteAgentState writes the heartbeat atomically.
func WriteAgentState(dir string, s AgentState) {
	b, err := json.Marshal(s)
	if err != nil {
		return
	}
	tmp := AgentStatePath(dir) + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, AgentStatePath(dir))
	}
}

// ReadAgentState loads the heartbeat (nil when absent/corrupt).
func ReadAgentState(dir string) *AgentState {
	b, err := os.ReadFile(AgentStatePath(dir))
	if err != nil {
		return nil
	}
	var s AgentState
	if json.Unmarshal(b, &s) != nil {
		return nil
	}
	return &s
}

// ProcessAlive reports whether a PID is still running.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

// RunningOrStale resolves a heartbeat: a "running" state whose PID died is
// reported as interrupted (the detached process died uncleanly).
func (s AgentState) Resolved(dir string) AgentState {
	if s.Status == AgentRunning && !ProcessAlive(s.PID) {
		s.Status = AgentInterrupted
		s.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return s
}

// SpawnReviewBG launches `review-bg <dir>` from the current binary in its own
// session (survives the TUI/CLI exiting) and writes the running heartbeat.
// headSHA pins the MR revision the job must analyze; review-bg refuses to
// run against a different revision.
func SpawnReviewBG(dir, headSHA string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	logFH, err := os.OpenFile(filepath.Join(dir, "agent-bg.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logFH.Close()
	cmd := exec.Command(self, "review-bg", dir)
	cmd.Stdout = logFH
	cmd.Stderr = logFH
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }() // reap the detached child
	WriteAgentState(dir, AgentState{
		Status: AgentRunning, PID: cmd.Process.Pid,
		JobID:     JobID(headSHA),
		HeadSHA:   headSHA,
		StartedAt: Now(),
	})
	return nil
}

// JobID derives a stable execution id: revision prefix + start timestamp.
func JobID(headSHA string) string {
	prefix := headSHA
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	return fmt.Sprintf("%s-%d", prefix, time.Now().Unix())
}

// String renders a compact status for logs/tests.
func (s AgentState) String() string {
	return fmt.Sprintf("%s pid=%d findings=%d raw=%d", s.Status, s.PID, s.Findings, s.Raw)
}
