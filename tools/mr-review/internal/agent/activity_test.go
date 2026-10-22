package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSummarizeTranscriptLine(t *testing.T) {
	st := &activityState{} // NOTE: shared — later lines carry accumulated stats
	cases := []struct {
		line string
		want string
	}{
		{
			`{"type":"message","message":{"role":"assistant","content":[{"type":"toolCall","id":"c1","name":"bash","arguments":{"command":"wc -l diff.patch && grep -n \"^diff --git\" diff.patch","description":"List files changed"}}]}}`,
			"⚙ bash: wc -l diff.patch && grep -n \"^diff --git\" diff.patch",
		},
		{
			`{"type":"message","message":{"role":"assistant","content":[{"type":"toolCall","id":"c2","name":"read","arguments":{"file_path":"services/review/handler.go"}}]}}`,
			"⚙ read: services/review/handler.go",
		},
		{
			`{"type":"custom","customType":"cache_summary","data":{"turnIndex":12,"cumulative":{"inputTokens":145880,"outputTokens":10639}}}`,
			"T12 · in 145k · out 10k · working…",
		},
		{
			`{"type":"message","message":{"role":"assistant","content":[{"type":"thinking","thinking":"Thinking: Let me start by understanding the task."}]}}`,
			"T12 · in 145k · out 10k · … Let me start by understanding the task.",
		},
		{
			`{"type":"message","message":{"role":"assistant","content":[{"type":"text","text":"I'm checking the reconciliation loop for idempotency."}]}}`,
			"T12 · in 145k · out 10k · note: I'm checking the reconciliation loop for idempotency.",
		},
		{
			`{"type":"message","message":{"role":"toolResult","toolCallId":"c9","toolName":"bash","isError":true,"content":[{"type":"text","text":"boom"}]}}`,
			"T12 · in 145k · out 10k · ✗ bash failed",
		},
		{`{"type":"message","message":{"role":"toolResult","toolCallId":"c8","toolName":"read","content":[{"type":"text","text":"ok"}]}}`, ""},
		{`{"type":"model_change"}`, ""},
	}
	for _, c := range cases {
		if got := st.summarize(c.line); got != c.want {
			t.Errorf("summarize:\n  %s\ngot  %q\nwant %q", c.line, got, c.want)
		}
	}
}

func TestSummarizeCarriesStats(t *testing.T) {
	st := &activityState{}
	// token bookkeeping from a summary entry, then a tool call:
	// the tool line must carry the accumulated stats prefix.
	st.summarize(`{"type":"custom","customType":"cache_summary","data":{"turnIndex":5,"cumulative":{"inputTokens":58000,"outputTokens":1000}}}`)
	got := st.summarize(`{"type":"message","message":{"role":"assistant","content":[{"type":"toolCall","id":"c1","name":"read","arguments":{"file_path":"a.go"}}]}}`)
	if !strings.HasPrefix(got, "T5 · in 58k · out 1k · ⚙ read: a.go") {
		t.Fatalf("stats not carried: %q", got)
	}
}

func TestPollTranscriptIncremental(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/s.json"
	var got []string
	emit := func(s string) { got = append(got, s) }

	// partial line must not be processed
	seen := pollTranscript(path, 0, emit, &activityState{})
	if seen != 0 || len(got) != 0 {
		t.Fatalf("partial write processed: seen=%d got=%v", seen, got)
	}
	appendFile(path, `{"type":"message","message":{"role":"assistant","content":[{"type":"thinking","thinking":"Thinking: Let me start by understanding the task."}]}}`+"\n")
	seen = pollTranscript(path, seen, emit, &activityState{})
	if len(got) != 1 || !strings.Contains(got[0], "Let me start") {
		t.Fatalf("first entry: %v", got)
	}
	appendFile(path, `{"type":"message","message":{"role":"assistant","content":[{"type":"toolCall","id":"c1","name":"bash","arguments":{"command":"make test"}}]}}`+"\n")
	seen = pollTranscript(path, seen, emit, &activityState{})
	if len(got) != 2 || !strings.Contains(got[1], "make test") {
		t.Fatalf("second entry: %v", got)
	}
	// re-poll with no growth: nothing new
	if s2 := pollTranscript(path, seen, emit, &activityState{}); s2 != seen {
		t.Fatal("no-growth poll must not advance")
	}
}

func TestNewestTranscriptFindsRecent(t *testing.T) {
	root := t.TempDir() + "/sessions/a"
	mkdir(root)
	old := root + "/old.jsonl"
	writeFile(old, "{}\n")
	twoHoursAgo := time.Now().Add(-2 * time.Hour)
	_ = osChtimes(old, twoHoursAgo, twoHoursAgo)
	recent := root + "/new.jsonl"
	writeFile(recent, "{}\n")

	if got := newestTranscript(filepath.Dir(root), time.Now().Add(-time.Minute), "a"); got != recent {
		t.Fatalf("newest = %q, want %q", got, recent)
	}
	// a review started "now" must not pick the 2h-old-only transcript
	if got := newestTranscript(filepath.Dir(root), time.Now().Add(2*time.Hour), "a"); got != "" {
		t.Fatalf("stale transcript must be ignored, got %q", got)
	}
}

func TestShortenDetail(t *testing.T) {
	home, _ := os.UserHomeDir()
	long := "cd " + home + "/.local/share/mr-review/worktrees/acme_proj-mr55868-9165daad && grep -rn \"pattern\" services/ | head -20"
	got := shortenDetail(long)
	if !strings.HasPrefix(got, "wt: grep -rn") {
		t.Fatalf("cd-worktree noise not collapsed: %q", got)
	}
	if strings.Contains(got, home) {
		t.Fatal("home path must be abbreviated to ~")
	}
	// plain command without cd: only ~ shortening + truncation
	plain := "cat " + home + "/very/long/path/" + strings.Repeat("x", 200)
	got = shortenDetail(plain)
	if len([]rune(got)) > 170 {
		t.Fatalf("overlong detail not truncated: %d runes", len([]rune(got)))
	}
	if !strings.HasPrefix(got, "cat ~/") {
		t.Fatalf("home not abbreviated: %q", got)
	}
}
