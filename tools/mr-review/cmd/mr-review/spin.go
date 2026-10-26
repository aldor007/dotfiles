package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// spinner shows live progress for async tasks (agent runs, git fetches).
// Animated on a TTY; a single status line when stdout is piped (tests/CI).
// While active it carries the latest activity line (streamed agent output,
// executed commands) via Update.
type spinner struct {
	stop     chan struct{}
	done     chan struct{}
	msg      string
	expected time.Duration // progress-bar denominator (0 = no bar)

	mu     sync.Mutex
	last   string // latest activity line
	fcount int    // findings written so far (live)
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// activeSpinner tracks the current CLI spinner so executed-command echoes
// can route into it instead of clobbering the frame.
var activeSpinner *spinner

func startSpinner(msg string) *spinner {
	return startSpinnerExpected(msg, 0)
}

// startSpinnerExpected adds a time-based progress bar: elapsed vs `expected`.
// Beyond 100% the bar stays full and marks overtime (the hard timeout still
// applies separately).
func startSpinnerExpected(msg string, expected time.Duration) *spinner {
	s := &spinner{stop: make(chan struct{}), done: make(chan struct{}), msg: msg, expected: expected}
	activeSpinner = s
	if !isTTY(os.Stdout) {
		fmt.Println("…", msg)
		close(s.done)
		return s
	}
	start := time.Now()
	go func() {
		defer close(s.done)
		for i := 0; ; i++ {
			select {
			case <-s.stop:
				return
			default:
			}
			s.mu.Lock()
			last := s.last
			s.mu.Unlock()
			elapsed := time.Since(start)
			line := msg
			if s.expected > 0 {
				line = fmt.Sprintf("%s %s", bar(elapsed, s.expected, 20), msg)
			}
			if last != "" {
				line = fmt.Sprintf("%s · %s", line, truncRunes(last, 70))
			}
			fmt.Printf("\r\033[2K%s %s (%ds)",
				spinnerFrames[i%len(spinnerFrames)], line, int(elapsed.Seconds()))
			time.Sleep(120 * time.Millisecond)
		}
	}()
	return s
}

// bar renders an elapsed/expected progress bar: [████████░░░░░░░░░░░░] 7m/10m.
func bar(elapsed, expected time.Duration, width int) string {
	ratio := 0.0
	if expected > 0 {
		ratio = float64(elapsed) / float64(expected)
	}
	if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio * float64(width))
	b := "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"
	if elapsed > expected && expected > 0 {
		b += " overtime"
	}
	return b
}

// SetCount reports the number of findings written so far.
func (s *spinner) SetCount(n int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.fcount = n
	s.mu.Unlock()
}

// Update feeds the latest activity line into the spinner (rendered live).
func (s *spinner) Update(line string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.last = strings.TrimSpace(line)
	s.mu.Unlock()
}

func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// Stop ends the spinner and prints the final line. ok selects ✓/✗.
func (s *spinner) Stop(ok bool, final string) {
	if s == nil {
		return
	}
	close(s.stop)
	<-s.done // let the goroutine finish its last write first
	if activeSpinner == s {
		activeSpinner = nil
	}
	if !isTTY(os.Stdout) {
		fmt.Println(final)
		return
	}
	mark := "\033[32m✓\033[0m"
	if !ok {
		mark = "\033[31m✗\033[0m"
	}
	fmt.Printf("\r\033[2K%s %s\n", mark, final)
}

// steps prints a ✓ line per completed workflow step with its duration.
type steps struct{ last time.Time }

func newSteps() *steps { return &steps{last: time.Now()} }

func (s *steps) done(format string, args ...any) {
	now := time.Now()
	dur := fmt.Sprintf("(%0.1fs)", now.Sub(s.last).Seconds())
	if isTTY(os.Stdout) {
		fmt.Printf("\033[32m✓\033[0m %s \033[2m%s\033[0m\n", fmt.Sprintf(format, args...), dur)
	} else {
		fmt.Printf("✓ %s %s\n", fmt.Sprintf(format, args...), dur)
	}
	s.last = now
}

// diffStat summarizes a unified diff: file/add/delete counts.
func diffStat(patch []byte) (files, add, del int) {
	for _, line := range strings.Split(string(patch), "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git"):
			files++
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			add++
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			del++
		}
	}
	return
}

// StopQuiet ends the spinner without printing a final line.
func (s *spinner) StopQuiet() {
	if s == nil {
		return
	}
	close(s.stop)
	<-s.done
	if activeSpinner == s {
		activeSpinner = nil
	}
	if isTTY(os.Stdout) {
		fmt.Print("\r\033[2K")
	}
}
