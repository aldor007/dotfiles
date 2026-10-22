package agent

import (
	"context"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Kimchi -p prints nothing on stdout until it finishes, so live "what is the
// agent doing" progress comes from its session transcript (JSONL, appended
// during the run under ~/.config/kimchi/harness/sessions/<slug>/<ts>.jsonl).

// SessionRoot returns the kimchi transcript root (overridable via
// MR_REVIEW_SESSION_DIR for tests).
func SessionRoot() string {
	if s := os.Getenv("MR_REVIEW_SESSION_DIR"); s != "" {
		return s
	}
	if u, err := user.Current(); err == nil {
		return filepath.Join(u.HomeDir, ".config/kimchi/harness/sessions")
	}
	return ".config/kimchi/harness/sessions"
}

// WatchSession polls the newest transcript written after `since` whose
// directory slug contains `hint` (e.g. "mr55868" — kimchi names session dirs
// after the working directory, which embeds the review worktree). Emits a
// rich one-line summary for every new entry until ctx is cancelled. Best
// effort: any error just means no progress lines.
func WatchSession(ctx context.Context, since time.Time, hint string, onLine func(string)) {
	if onLine == nil {
		return
	}
	root := SessionRoot()
	var path string
	var seen int
	var st activityState
	ticker := time.NewTicker(800 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if path == "" {
			path = newestTranscript(root, since, hint)
			if path == "" {
				continue
			}
			onLine("transcript: " + filepath.Base(path))
		}
		seen = pollTranscript(path, seen, onLine, &st)
	}
}

// newestTranscript finds the most recently modified *.jsonl transcript whose
// parent dir contains hint (when hint != "").
func newestTranscript(root string, since time.Time, hint string) string {
	var best string
	var bestMod time.Time
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.ModTime().Before(since.Add(-time.Second)) {
			return nil
		}
		if hint != "" && !strings.Contains(path, hint) {
			return nil
		}
		if info.ModTime().After(bestMod) {
			bestMod = info.ModTime()
			best = path
		}
		return nil
	})
	return best
}

// pollTranscript processes newly completed lines; returns the new offset.
func pollTranscript(path string, seen int, onLine func(string), st *activityState) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return seen
	}
	text := string(b)
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n" // don't process a partially written line
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(lines) <= seen {
		return seen
	}
	for _, l := range lines[seen:] {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if s := st.summarize(l); s != "" {
			onLine(s)
			if len(lines)-seen > 10 {
				time.Sleep(30 * time.Millisecond) // replay pacing
			}
		}
	}
	return len(lines)
}

// activityState accumulates cross-entry context (turn number, token burn)
// so every activity line can carry it.
type activityState struct {
	turn int
	inK  string
	outK string
}

var (
	reToolCallName = regexp.MustCompile(`"type":"toolCall","id":"[^"]*","name":"([a-zA-Z_-]+)"`)
	reCommand      = regexp.MustCompile(`"command":"((?:[^"\\]|\\.){1,220})`)
	reFilePath     = regexp.MustCompile(`"file_path":"((?:[^"\\]|\\.){1,80})`)
	reURL          = regexp.MustCompile(`"url":"((?:[^"\\]|\\.){1,60})`)
	reDescription  = regexp.MustCompile(`"description":"((?:[^"\\]|\\.){1,80})`)
	rePattern      = regexp.MustCompile(`"pattern":"((?:[^"\\]|\\.){1,60})`)
	reAssistant    = regexp.MustCompile(`"role":"assistant"`)
	reTextBlock    = regexp.MustCompile(`"type":"text","text":"((?:[^"\\]|\\.){1,90})`)
	reThinking     = regexp.MustCompile(`"type":"thinking","thinking":"Thinking: ((?:[^"\\]|\\.){1,80})`)
	reToolResult   = regexp.MustCompile(`"role":"toolResult","toolCallId":"[^"]*","toolName":"([a-zA-Z_-]+)"`)
	reIsError      = regexp.MustCompile(`"isError":true`)
	reTurnIdx      = regexp.MustCompile(`"turnIndex":(\d+)`)
	reInTokens     = regexp.MustCompile(`"inputTokens":(\d+)`)
	reOutTokens    = regexp.MustCompile(`"outputTokens":(\d+)`)
)

// summarize turns one transcript JSONL entry into a rich activity line like
//
//	T5·58k→1k ⚙ bash: wc -l diff.patch && grep -n "^diff --git" …
//	T7·62k→3k ⚙ read: services/review/handler.go
//	T8·64k→5k note: I'm checking the reconciliation loop for idempotency
//	T9·66k→6k ✗ bash failed
//
// Stats-only entries (cache summaries) emit the stats + "working…". Others
// yield "" (no line).
func (st *activityState) summarize(line string) string {
	statsBefore := strconv.Itoa(st.turn) + "|" + st.inK + "|" + st.outK
	// token/turn bookkeeping first (present on cache_summary entries)
	if m := reTurnIdx.FindStringSubmatch(line); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			st.turn = n
		}
	}
	if m := reInTokens.FindStringSubmatch(line); m != nil {
		st.inK = fmtK(m[1])
	}
	if m := reOutTokens.FindStringSubmatch(line); m != nil {
		st.outK = fmtK(m[1])
	}
	statsChanged := statsBefore != strconv.Itoa(st.turn)+"|"+st.inK+"|"+st.outK

	var event string
	switch {
	case reToolCallName.MatchString(line):
		name := reToolCallName.FindStringSubmatch(line)[1]
		detail := ""
		for _, re := range []*regexp.Regexp{reCommand, reFilePath, reURL, rePattern, reDescription} {
			if mm := re.FindStringSubmatch(line); mm != nil {
				detail = unescape(mm[1])
				break
			}
		}
		if detail != "" {
			event = "⚙ " + name + ": " + shortenDetail(detail)
		} else {
			event = "⚙ " + name
		}
	case reAssistant.MatchString(line) && reTextBlock.MatchString(line):
		// the agent narrating its own review
		event = "note: " + unescape(reTextBlock.FindStringSubmatch(line)[1])
	case reToolResult.MatchString(line):
		name := reToolResult.FindStringSubmatch(line)[1]
		if reIsError.MatchString(line) {
			event = "✗ " + name + " failed"
		} else {
			return "" // successful results are noise; the next action follows
		}
	case reThinking.MatchString(line):
		event = "… " + unescape(reThinking.FindStringSubmatch(line)[1])
	default:
		if statsChanged {
			return st.compose("working…")
		}
		return ""
	}
	return st.compose(event)
}

// compose prefixes the event with the compact stats context.
func (st *activityState) compose(event string) string {
	var parts []string
	if st.turn > 0 {
		parts = append(parts, "T"+strconv.Itoa(st.turn))
	}
	if st.inK != "" {
		parts = append(parts, "in "+st.inK)
	}
	if st.outK != "" {
		parts = append(parts, "out "+st.outK)
	}
	if len(parts) == 0 {
		return event
	}
	return strings.Join(parts, " · ") + " · " + event
}

// shortenDetail makes long commands readable: the home dir becomes ~, a
// leading `cd <review worktree> && ` collapses to `wt:` (the agent already
// runs inside that worktree), and overlong tails are ellipsized.
func shortenDetail(detail string) string {
	d := unescape(detail)
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		d = strings.ReplaceAll(d, home, "~")
	}
	if i := strings.Index(d, "/mr-review/worktrees/"); i >= 0 {
		rest := d[i:]
		// cut the cd operand: ends at the next " && "
		if j := strings.Index(rest, " && "); j >= 0 {
			d = "wt: " + d[i+j+4:]
		}
	}
	const max = 160
	if len(d) > max {
		d = d[:max-1] + "…"
	}
	return d
}

func unescape(s string) string {
	s = strings.ReplaceAll(s, "\\n", " ")
	s = strings.ReplaceAll(s, "\\t", " ")
	s = strings.ReplaceAll(s, "\\\"", "\"")
	return strings.TrimSpace(s)
}

func fmtK(digits string) string {
	n, _ := strconv.Atoi(digits)
	switch {
	case n >= 1_000_000:
		return strconv.Itoa(n/1_000_000) + "M"
	case n >= 1000:
		return strconv.Itoa(n/1000) + "k"
	default:
		return digits
	}
}
