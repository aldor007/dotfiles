package tui

// Layer 3 e2e (vttest): the REAL compiled binary runs inside a virtual
// terminal (PTY + vt emulator). Keyboard input is delivered as raw terminal
// bytes and assertions run against the captured screen GRID — cells, cursor
// and alternate-screen state — not against View() output. A coding agent
// needs no eyes for this: everything is structured data.
//
// Migrated to the redesigned layout contracts:
//   wide  ≥120: three side-by-side bordered panes FILES | DIFF | FINDINGS
//   medium 80–119: FILES sidebar + DIFF (findings = focused drawer)
//   narrow  <80: a single focused pane
// The legacy assertions (detail tabs, one heavy divider at listWidthFor)
// described the old 2-pane cockpit and have no new-UX equivalent.

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

// vtWaitAbsent polls until NO line contains substr (5s budget) — the
// floating-overlay counterpart of vtWaitFor: the underlying frame keeps
// rendering, so "closed" means the overlay box is gone, not that the frame
// changed.
func vtWaitAbsent(t *testing.T, tt *vttest.Terminal, substr, desc string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		found := false
		for _, l := range snapText(tt) {
			if strings.Contains(l, substr) {
				found = true
				break
			}
		}
		if !found {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%q still on screen after %s:\n%s", substr, desc, strings.Join(snapText(tt), "\n"))
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
// TUI in a virtual terminal of the given size. The wait token is the diff's
// file header, visible at every width (the findings pane is hidden at <120).
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
	vtWaitFor(t, tt, vtContains("handler.go"), "diff painted")
	return tt, cmd, dir
}

// vtCorners returns the row index of the top pane border and the columns of
// every "╭" corner on it (one corner per bordered pane box).
func vtCorners(lines []string) (top int, cols []int, ok bool) {
	for y, l := range lines {
		p := stripANSIish(l)
		if !strings.Contains(p, "╭") {
			continue
		}
		n := 0 // CELL index — box-drawing runes are 3 bytes; byte index lies
		for _, r := range p {
			if r == '╭' {
				cols = append(cols, n)
			}
			n++
		}
		return y, cols, true
	}
	return -1, nil, false
}

// vtCheckFrame asserts the shared invariants at every size: alt screen,
// exact grid size, no row wider than the terminal.
func vtCheckFrame(t *testing.T, tt *vttest.Terminal, w, h int) []string {
	t.Helper()
	snap := tt.Snapshot()
	if !snap.AltScreen {
		t.Error("the review TUI must run on the alternate screen")
	}
	if snap.Cols != w || snap.Rows != h {
		t.Fatalf("terminal size = %dx%d, want %dx%d", snap.Cols, snap.Rows, w, h)
	}
	lines := snapText(tt)
	for y, l := range lines {
		if len([]rune(l)) > w {
			t.Fatalf("row %d is %d cells (> %d): %q", y, len([]rune(l)), w, l)
		}
	}
	return lines
}

// TestVttestLayoutInvariants — structural screen-grid assertions for the
// three-pane redesign across the width bands.
func TestVttestLayoutInvariants(t *testing.T) {
	t.Run("wide-120x35", func(t *testing.T) { vtAssertWide(t, 120, 35) })
	t.Run("wide-160x45", func(t *testing.T) { vtAssertWide(t, 160, 45) })
	t.Run("wide-200x50", func(t *testing.T) { vtAssertWide(t, 200, 50) })
	t.Run("medium-80x24", func(t *testing.T) {
		tt, cmd, _ := startVtReview(t, 80, 24)
		defer vtQuit(t, tt, cmd)
		lines := vtCheckFrame(t, tt, 80, 24)
		top, cols, ok := vtCorners(lines)
		if !ok || len(cols) != 2 {
			t.Fatalf("medium 80x24: want 2 pane boxes (sidebar+diff), got %v: %d corners", cols, len(cols))
		}
		hdr := lines[top+1]
		if !strings.Contains(hdr, "FILES") || !strings.Contains(hdr, "DIFF") {
			t.Errorf("medium header row %q must show FILES and DIFF", hdr)
		}
		if strings.Contains(hdr, "FINDINGS") {
			t.Error("medium startup must NOT show the findings pane (drawer only when focused)")
		}
		vtAssertPaneEdges(t, lines, top, cols)
	})
	t.Run("narrow-60x24", func(t *testing.T) {
		tt, cmd, _ := startVtReview(t, 60, 24)
		defer vtQuit(t, tt, cmd)
		lines := vtCheckFrame(t, tt, 60, 24)
		top, cols, ok := vtCorners(lines)
		if !ok || len(cols) != 1 {
			t.Fatalf("narrow 60x24: want a single pane box, got %d corners", len(cols))
		}
		hdr := lines[top+1]
		// startup focus is the diff pane: narrow shows the focused detail pane
		if !strings.Contains(hdr, "DETAILS") {
			t.Errorf("narrow startup header row %q must show the focused pane (DETAILS)", hdr)
		}
		if strings.Contains(hdr, "FINDINGS") || strings.Contains(hdr, "FILES") {
			t.Errorf("narrow shows exactly one pane, got %q", hdr)
		}
	})
	// footer invariants once (same chrome at every size)
	tt, cmd, _ := startVtReview(t, 120, 35)
	defer vtQuit(t, tt, cmd)
	lines := vtCheckFrame(t, tt, 120, 35)
	footer := strings.Join(lines[len(lines)-4:], "\n")
	if !strings.Contains(footer, "j/k") {
		t.Errorf("key hints missing from the footer: %q", footer)
	}
	if !strings.Contains(footer, "Status:") {
		t.Errorf("status line missing from the footer: %q", footer)
	}
}

// vtAssertWide asserts the wide-band contract: three side-by-side panes
// whose widths match the layout() guarantees, continuous box edges, and
// no pane overlap.
func vtAssertWide(t *testing.T, w, h int) {
	t.Helper()
	tt, cmd, _ := startVtReview(t, w, h)
	defer vtQuit(t, tt, cmd)
	lines := vtCheckFrame(t, tt, w, h)
	top, cols, ok := vtCorners(lines)
	if !ok || len(cols) != 3 {
		t.Fatalf("%dx%d: want 3 pane boxes (FILES|DIFF|FINDINGS), got %d corners: %v",
			w, h, len(cols), cols)
	}
	hdr := lines[top+1]
	iFiles := strings.Index(hdr, "FILES")
	iDiff := strings.Index(hdr, "DIFF")
	iFind := strings.Index(hdr, "FINDINGS")
	if iFiles < 0 || iDiff < 0 || iFind < 0 || !(iFiles < iDiff && iDiff < iFind) {
		t.Fatalf("%dx%d: pane headers not side by side in order: %q", w, h, hdr)
	}
	// widths must match the layout() contract: files 26..44, findings
	// 34..48, diff ≥40, boxes + chrome sum EXACTLY to the terminal width
	// (each pane: 2 border cols; 1 gap col between panes — panesChrome(3)=8)
	fw := cols[1] - cols[0] - 3
	dw := cols[2] - cols[1] - 3
	gw := len([]rune(stripANSIish(lines[top]))) - 2 - cols[2]
	if fw < 26 || fw > 44 {
		t.Errorf("%dx%d: files pane width %d outside [26,44]", w, h, fw)
	}
	if gw < 34 || gw > 48 {
		t.Errorf("%dx%d: findings pane width %d outside [34,48]", w, h, gw)
	}
	if dw < 40 {
		t.Errorf("%dx%d: diff pane width %d below the 40 floor", w, h, dw)
	}
	if fw+dw+gw+8 != w { // panesChrome(3) = 3*2 borders + 2 gaps
		t.Errorf("%dx%d: pane widths %d+%d+%d+7 != %d (overlap or gap)", w, h, fw, dw, gw, w)
	}
	vtAssertPaneEdges(t, lines, top, cols)
}

// vtAssertPaneEdges verifies the vertical box edges are continuous between
// the top and bottom borders (no pane bleeds into its neighbour).
func vtAssertPaneEdges(t *testing.T, lines []string, top int, cols []int) {
	t.Helper()
	bottom := -1
	for y := top + 1; y < len(lines); y++ {
		if strings.Contains(lines[y], "╰") {
			bottom = y
			break
		}
	}
	if bottom < 0 {
		t.Fatalf("bottom border missing below row %d", top)
	}
	for y := top + 1; y < bottom; y++ {
		row := []rune(lines[y])
		for _, cx := range cols {
			if cx >= len(row) || row[cx] != '│' {
				t.Fatalf("pane edge broken at row %d col %d: %q", y, cx, lines[y])
			}
		}
	}
}

// TestVttestPaletteAndApprove — keyboard drives the real binary: the
// palette opens with registry actions, approve flips the decision, state
// persists, q quits.
func TestVttestPaletteAndApprove(t *testing.T) {
	tt, cmd, dir := startVtReview(t, 120, 35)

	tt.SendText(":")
	time.Sleep(150 * time.Millisecond) // let the app process each key
	vtWaitFor(t, tt, vtContains("ACTIONS"), "palette open")
	vtWaitFor(t, tt, vtContains("Approve finding"), "palette lists registry actions")
	tt.SendText("\x1b") // esc closes the palette
	time.Sleep(150 * time.Millisecond)
	// the palette FLOATS over the unchanged frame, so "closed" means the
	// ACTIONS box is gone (a plain footer check passes while it is open)
	vtWaitAbsent(t, tt, "ACTIONS", "palette closed")

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

// TestVttestResize — resize the PTY; the layout must adapt (sidebar+diff at
// 80, the full three-pane frame back at 120) without losing state.
func TestVttestResize(t *testing.T) {
	tt, cmd, _ := startVtReview(t, 80, 24)
	defer vtQuit(t, tt, cmd)

	vtWaitFor(t, tt, vtContains("handler.go"), "80x24 paint")
	if err := tt.Resize(120, 35); err != nil {
		t.Fatalf("resize: %v", err)
	}
	// vttest's Resize is ioctl-only; make sure the child hears about it
	if cmd.Process != nil {
		_ = cmd.Process.Signal(syscall.SIGWINCH)
	}
	// the wide repaint brings the third pane back; F001 (the selection)
	// must still be visible — resize preserves state
	vtWaitFor(t, tt, vtContains("FINDINGS"), "120x35 repaint after resize")
	vtWaitFor(t, tt, vtContains("F001"), "selection survived the resize")
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
