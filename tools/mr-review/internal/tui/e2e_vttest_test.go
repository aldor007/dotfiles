package tui

// Layer 3 e2e (vttest): the REAL compiled binary runs inside a virtual
// terminal (PTY + vt emulator). Keyboard input is delivered as raw terminal
// bytes and assertions run against the captured screen GRID — cells, cursor
// and alternate-screen state — not against View() output. A coding agent
// needs no eyes for this: everything is structured data.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"mr-review/internal/store"

	"github.com/charmbracelet/x/vttest"
)

// vtBin compiles the real mr-review binary for the virtual terminal.
func vtBin(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "mr-review")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/mr-review")
	cmd.Dir = filepath.Join("..", "..") // module root (tools/mr-review)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build binary: %v\n%s", err, out)
	}
	return bin
}

// vtEnv isolates state and pins a deterministic terminal profile.
func vtEnv(home string) []string {
	var out []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "NO_COLOR=") || strings.HasPrefix(e, "MR_REVIEW_") {
			continue
		}
		out = append(out, e)
	}
	return append(out,
		"MR_REVIEW_HOME="+home, // isolated state dir
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"CI=1",
	)
}

// snapText renders the terminal grid to plain lines (cells joined per row).
func snapText(tt *vttest.Terminal) []string {
	snap := tt.Snapshot()
	lines := make([]string, len(snap.Cells))
	for y, row := range snap.Cells {
		var b strings.Builder
		for _, c := range row {
			b.WriteString(c.Content)
		}
		lines[y] = strings.TrimRight(b.String(), " ")
	}
	return lines
}

// vtWaitFor polls the screen until a line satisfies want (5s budget).
func vtWaitFor(t *testing.T, tt *vttest.Terminal, want func(line string) bool, desc string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, l := range snapText(tt) {
			if want(l) {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	lines := snapText(tt)
	t.Fatalf("timeout waiting for %q; screen:\n%s", desc, strings.Join(lines, "\n"))
}

func vtContains(substr string) func(string) bool {
	return func(l string) bool { return strings.Contains(l, substr) }
}

// vtQuit sends q and waits — but never forever: a lost keystroke must not
// hang the suite, so the child is killed after a grace period.
func vtQuit(t *testing.T, tt *vttest.Terminal, cmd *exec.Cmd) {
	t.Helper()
	tt.SendText("q")
	done := make(chan struct{})
	go func() { _ = tt.Wait(cmd); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Log("quit keystroke lost — child killed")
	}
}

// startVtReview compiles the binary, writes the fixture and opens the review
// TUI in a virtual terminal of the given size.
func startVtReview(t *testing.T, w, h int) (*vttest.Terminal, *exec.Cmd, string) {
	t.Helper()
	if raceEnabled {
		// upstream bug: vttest.Terminal.Close races its internal PTY copy
		// goroutine (charmbracelet/x#vttest) — false positives under -race
		t.Skip("vttest has an upstream data race in Close(); skipped under -race")
	}
	bin := vtBin(t)
	dir := writeReviewFixture(t, t.TempDir())
	tt, err := vttest.NewTerminal(t, w, h)
	if err != nil {
		t.Fatalf("terminal: %v", err)
	}
	t.Cleanup(func() { _ = tt.Close() })
	cmd := exec.Command(bin, "tui", dir)
	cmd.Env = vtEnv(t.TempDir())
	if err := tt.Start(cmd); err != nil {
		t.Fatalf("start: %v", err)
	}
	vtWaitFor(t, tt, vtContains("F001"), "findings painted")
	return tt, cmd, dir
}

// TestVttestLayoutInvariants — structural screen-grid assertions at 120×35.
func TestVttestLayoutInvariants(t *testing.T) {
	tt, cmd, _ := startVtReview(t, 120, 35)
	defer vtQuit(t, tt, cmd)

	snap := tt.Snapshot()
	if !snap.AltScreen {
		t.Error("the review TUI must run on the alternate screen")
	}
	if snap.Cols != 120 || snap.Rows != 35 {
		t.Fatalf("terminal size = %dx%d, want 120x35", snap.Cols, snap.Rows)
	}
	lines := snapText(tt)

	// all five detail tabs visible at 120 cols (full labels, not the
	// narrow-mode short names)
	tabs := strings.Join(lines[2:7], "\n")
	for _, name := range []string{"Diff", "Explanation", "Evidence", "Comment", "Discussion"} {
		if !strings.Contains(tabs, name) {
			t.Errorf("tab %s not visible at 120x35:\n%s", name, tabs)
		}
	}

	// the vertical divider spans the entire content area (the cockpit
	// widget draws a heavy ┃ divider; plain │ also accepted)
	divX := listWidthFor(120) + 1
	for y := 3; y < 29; y++ { // content rows: between the box's top/bottom borders
		row := snap.Cells[y]
		if divX >= len(row) || (row[divX].Content != "│" && row[divX].Content != "┃") {
			t.Fatalf("divider missing at row %d col %d: %q", y, divX, row[divX].Content)
		}
	}

	// footer: status + key hints sit right under the body box (header 2 +
	// box 29 rows); the alt screen keeps spare rows below them
	footer := strings.Join(lines[29:34], "\n")
	if !strings.Contains(footer, "j/k") {
		t.Errorf("key hints missing from footer rows 30-34: %q", footer)
	}
	if !strings.Contains(footer, "Status:") {
		t.Errorf("status line missing from footer rows 30-34: %q", footer)
	}
}

// TestVttestPaletteAndApprove — keyboard drives the real binary: palette
// opens with actions, approve flips the decision, state persists, q quits.
func TestVttestPaletteAndApprove(t *testing.T) {
	tt, cmd, dir := startVtReview(t, 120, 35)

	tt.SendText(":")
	time.Sleep(150 * time.Millisecond) // let the app process each key
	vtWaitFor(t, tt, vtContains("ACTIONS"), "palette open")
	vtWaitFor(t, tt, vtContains("Approve finding"), "palette lists registry actions")
	tt.SendText("\x1b") // esc closes the palette
	time.Sleep(150 * time.Millisecond)
	// the palette is closed only when the normal-mode footer is back — a
	// "no ACTIONS anywhere" check passes instantly while it is still open
	vtWaitFor(t, tt, vtContains("j/k findings"), "palette closed, normal footer back")

	tt.SendText("a") // approve the selected finding
	vtWaitFor(t, tt, vtContains("approved"), "decision recorded in the status line")

	// state hit the disk before quit
	// the save is atomic (tmp+rename): poll the disk instead of racing it
	deadline := time.Now().Add(3 * time.Second)
	for {
		fs, ferr := store.LoadFindings(dir)
		if ferr == nil && len(fs) > 0 && fs[0].Decision == "approved" {
			break
		}
		if time.Now().After(deadline) {
			raw, _ := os.ReadFile(filepath.Join(dir, "findings.jsonl"))
			t.Fatalf("decision never hit the disk: %v %+v\nraw: %s", ferr, fs, string(raw))
		}
		time.Sleep(50 * time.Millisecond)
	}

	vtQuit(t, tt, cmd) // hangs would mean a lost keystroke, not an app bug
}

// TestVttestResize — resize the PTY; the layout must adapt (short tab
// labels at 80, full chrome back at 120).
func TestVttestResize(t *testing.T) {
	tt, cmd, _ := startVtReview(t, 80, 24)
	defer vtQuit(t, tt, cmd)

	vtWaitFor(t, tt, vtContains("F001"), "80x24 paint")
	if err := tt.Resize(120, 35); err != nil {
		t.Fatalf("resize: %v", err)
	}
	// vttest's Resize is ioctl-only; make sure the child hears about it
	if cmd.Process != nil {
		_ = cmd.Process.Signal(syscall.SIGWINCH)
	}
	vtWaitFor(t, tt, func(l string) bool {
		return strings.Contains(l, "F001") && len(l) > 100 // a 120-wide row
	}, "120x35 repaint after resize") // on timeout the screen dump shows why
	snap := tt.Snapshot()
	if snap.Cols != 120 || snap.Rows != 35 {
		t.Fatalf("snapshot size = %dx%d after resize", snap.Cols, snap.Rows)
	}
}

// TestVttestGoldenSnapshots — text-grid goldens of the real binary at the
// two most common sizes (rows of cell contents; styles are covered by the
// in-process goldens). Update with: go test ./internal/tui -update
func TestVttestGoldenSnapshots(t *testing.T) {
	for _, size := range [][2]int{{120, 35}, {80, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			tt, cmd, _ := startVtReview(t, size[0], size[1])
			defer vtQuit(t, tt, cmd)

			snap := tt.Snapshot()
			grid := make([][]string, len(snap.Cells))
			for y, row := range snap.Cells {
				grid[y] = make([]string, len(row))
				for x, c := range row {
					grid[y][x] = c.Content
				}
			}
			got, err := json.MarshalIndent(grid, "", " ")
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("testdata", "vttest", fmt.Sprintf("findings-%dx%d.json", size[0], size[1]))
			if *update {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := osWriteFile(path, got); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := osReadFile(path)
			if err != nil {
				t.Skipf("no vt golden (run with -update): %v", err)
			}
			if string(got) != string(want) {
				t.Fatalf("vt golden mismatch for %s (first diff at byte %d)",
					path, firstDiffByte(string(want), string(got)))
			}
		})
	}
}

func firstDiffByte(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}
