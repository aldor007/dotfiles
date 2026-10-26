// Package plex embeds full-screen child programs (tuicr) into panes of the
// mr-review TUI — a small Go terminal multiplexer built on charmbracelet/x/vt
// (terminal emulator) and creack/pty. No tmux, no herdr: mr-review owns the
// split.
package plex

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

// Pane is a child process running in a PTY with its screen emulated locally.
// It is safe for concurrent use.
type Pane struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	tty     *os.File
	emu     *vt.SafeEmulator
	cols    int
	rows    int
	exited  chan struct{}
	changed chan struct{} // signalled (coalesced) when the screen changed
	exitErr error
}

// Start launches name/args in a new PTY of the given size, feeding its output
// through the emulator.
func Start(ctx context.Context, dir string, cols, rows int, env []string, name string, args ...string) (*Pane, error) {
	if cols < 10 {
		cols = 40
	}
	if rows < 4 {
		rows = 12
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	} else {
		cmd.Env = os.Environ()
	}
	cmd.Env = append(cmd.Env,
		fmt.Sprintf("TERM=xterm-256color"),
		fmt.Sprintf("COLUMNS=%d", cols),
		fmt.Sprintf("LINES=%d", rows),
	)
	tty, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("pty start %s: %w", name, err)
	}
	if err := pty.Setsize(tty, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}); err != nil {
		tty.Close()
		return nil, fmt.Errorf("pty setsize: %w", err)
	}
	p := &Pane{
		cmd:     cmd,
		tty:     tty,
		emu:     vt.NewSafeEmulator(cols, rows),
		cols:    cols,
		rows:    rows,
		exited:  make(chan struct{}),
		changed: make(chan struct{}, 1),
	}
	// child output → emulator; emulator responses (query replies) → child.
	// The response pump runs separately: when the child exits, the output
	// pump ends, we close the emulator to unblock the response reader, and
	// only then mark the pane exited.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := p.emu.Read(buf)
			if n > 0 {
				_, _ = p.tty.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		_, _ = copyTo(p.emu, p.tty, p)
		// NOTE: the emulator is intentionally not Closed here — Close is
		// not synchronised with the blocking response pump (x/vt
		// SafeEmulator), and racing it trips the detector. The pump
		// goroutine stays parked on the emulator's response pipe until
		// process exit; Kill() closes the PTY so nothing user-visible
		// lingers.
		_ = cmd.Wait()
		close(p.exited)
	}()
	return p, nil
}

// copyTo copies r into w until EOF, notifying the change channel after each
// batch (coalesced: buffered cap 1 — a dropped signal is harmless because
// the consumer re-renders the latest screen state anyway).
func copyTo(w interface{ Write([]byte) (int, error) }, r *os.File, p *Pane) (int64, error) {
	var total int64
	buf := make([]byte, 32*1024)
	for {
		n, rerr := r.Read(buf)
		if n > 0 {
			wn, werr := w.Write(buf[:n])
			total += int64(wn)
			p.signal()
			if werr != nil {
				return total, werr
			}
		}
		if rerr != nil {
			return total, rerr
		}
	}
}

// signal marks the screen changed without ever blocking the pump.
func (p *Pane) signal() {
	select {
	case p.changed <- struct{}{}:
	default:
	}
}

// Changed returns a channel that receives a (coalesced) tick whenever the
// emulated screen changed — the host TUI re-renders the pane on receipt.
func (p *Pane) Changed() <-chan struct{} { return p.changed }

// Resize resizes the PTY and the emulator.
func (p *Pane) Resize(cols, rows int) {
	if cols < 10 || rows < 4 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if cols == p.cols && rows == p.rows {
		return
	}
	p.cols, p.rows = cols, rows
	p.emu.Resize(cols, rows)
	_ = pty.Setsize(p.tty, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	// refresh the child's view of its size
	_, _ = p.tty.Write([]byte(fmt.Sprintf("\x1b[8;%d;%dt", rows, cols)))
}

// Write sends raw key bytes to the child (input forwarding).
func (p *Pane) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.tty.Write(b)
}

// WriteString sends a key string to the child.
func (p *Pane) WriteString(s string) {
	_, _ = p.Write([]byte(s))
}

// Render returns the emulated screen as ANSI text (lines joined with \n).
func (p *Pane) Render() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.emu.Render()
}

// Exited returns a channel closed when the child process ends.
func (p *Pane) Exited() <-chan struct{} { return p.exited }

// Alive reports whether the child is still running.
func (p *Pane) Alive() bool {
	select {
	case <-p.exited:
		return false
	default:
		return true
	}
}

// Kill terminates the child (workspace shutdown).
func (p *Pane) Kill() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd.Process != nil {
		_ = syscallKillGroup(p.cmd.Process.Pid)
	}
	_ = p.tty.Close()
}

// ScreenText returns the plain-text rows of the emulated screen (for tests
// and assertions).
func (p *Pane) ScreenText() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	rendered := p.emu.Render()
	lines := strings.Split(strings.TrimRight(rendered, "\n"), "\n")
	return lines
}

// InAltScreen reports whether the child switched to the alternate screen
// buffer (full-screen TUIs do; tests assert the TUI took over).
func (p *Pane) InAltScreen() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.emu.IsAltScreen()
}

// CellContent returns the character content of one emulated screen cell
// (empty outside the grid) — for cell-level golden tests.
func (p *Pane) CellContent(x, y int) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	c := p.emu.CellAt(x, y)
	if c == nil {
		return ""
	}
	return c.Content
}

// syscallKillGroup is defined per-platform (kill.go / kill_windows.go).

// Size returns the current pane size in cells (for tests and diagnostics).
func (p *Pane) Size() (int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cols, p.rows
}
