package plex

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestPaneEchoAndScreen — the pane runs a child in a PTY, its output is
// emulated, input is forwarded, and resize works.
func TestPaneEchoAndScreen(t *testing.T) {
	script := "#!/bin/sh\nwhile IFS= read -r line; do echo \"GOT:$line\"; done\n"
	dir := t.TempDir()
	writeFile(t, dir+"/child.sh", script, 0o755)

	p, err := Start(context.Background(), dir, 60, 10, nil, dir+"/child.sh")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Kill()

	p.WriteString("hello tuicr\n")
	waitFor(t, 8*time.Second, func() bool {
		return strings.Contains(strings.Join(p.ScreenText(), "\n"), "GOT:hello tuicr")
	}, "child echo must appear on the emulated screen")

	// resize keeps the pane alive
	p.Resize(50, 8)
	if !p.Alive() {
		t.Fatal("pane must survive resize")
	}
}

// TestPaneExit — a child that exits closes the Exited channel and Alive
// flips false.
func TestPaneExit(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir+"/short.sh", "#!/bin/sh\necho bye\n", 0o755)
	p, err := Start(context.Background(), dir, 40, 8, nil, dir+"/short.sh")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Kill()
	select {
	case <-p.Exited():
		// ok
	case <-time.After(5 * time.Second):
		t.Fatal("child exit not detected")
	}
	if p.Alive() {
		t.Fatal("Alive must be false after exit")
	}
}

func waitFor(t *testing.T, d time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal(msg)
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

// TestPaneChangeSignal — the Changed channel fires when the child writes
// output (drives the host TUI's event-driven repaint).
func TestPaneChangeSignal(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir+"/talker.sh", "#!/bin/sh\necho hello\nsleep 5\n", 0o755)
	p, err := Start(context.Background(), dir, 40, 8, nil, dir+"/talker.sh")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Kill()
	select {
	case <-p.Changed():
		// ok — screen changed
	case <-p.Exited():
		t.Fatal("child exited before a change signal")
	case <-time.After(5 * time.Second):
		t.Fatal("no change signal within 5s")
	}
}
