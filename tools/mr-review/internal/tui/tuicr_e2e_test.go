package tui

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"mr-review/internal/store"
	"mr-review/internal/tuicr"
)

// fakeTuicrScript writes a Python tuicr stand-in implementing the documented
// JSON contract (review list/comments/add) for one GitLab PR session.
func fakeTuicrScript(t *testing.T, iid int) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "fake-tuicr")
	script := "#!/usr/bin/env python3\n" +
		"import json, os, sys\n" +
		"iid = " + strconv.Itoa(iid) + "\n" +
		"args = sys.argv[1:]\n" +
		"state = os.environ['FAKE_TUICR_STATE']\n" +
		"def load():\n" +
		"    if os.path.exists(state):\n" +
		"        with open(state) as f: return json.load(f)\n" +
		"    return {'comments': [], 'next': 1}\n" +
		"def save(d):\n" +
		"    with open(state, 'w') as f: json.dump(d, f)\n" +
		"def out(v): print(json.dumps(v))\n" +
		"if args[0:2] == ['review', 'list']:\n" +
		"    out([{'slug': 'gl:grp/proj/pr/%d' % iid, 'kind': 'pr', 'path': '/tmp/s.json',\n" +
		"          'updated_at': '2026-10-21T10:00:00Z', 'comment_count': 0,\n" +
		"          'reviewed_count': 0, 'file_count': 3, 'anchor': 'pr/%d' % iid, 'active': False}])\n" +
		"elif args[0:2] == ['review', 'comments']:\n" +
		"    out(load()['comments'])\n" +
		"elif args[0:3] == ['review', 'add', '--session']:\n" +
		"    payload = json.loads(sys.stdin.read())\n" +
		"    d = load()\n" +
		"    line = payload.get('line', 0)\n" +
		"    c = {'id': 'comment-%04d' % d['next'],\n" +
		"         'location': '%s:%d' % (payload.get('file', ''), line),\n" +
		"         'path': payload.get('file', ''),\n" +
		"         'start_line': line,\n" +
		"         'end_line': payload.get('end_line', line),\n" +
		"         'side': payload.get('side', 'new'), 'comment_type': 'issue',\n" +
		"         'author': payload.get('username', 'user'),\n" +
		"         'lifecycle_state': 'local_draft', 'created_at': '2026-10-21T10:00:00Z',\n" +
		"         'content': payload.get('content', '')}\n" +
		"    d['comments'].append(c); d['next'] += 1; save(d); out(c)\n" +
		"else:\n" +
		"    sys.exit('unexpected args')\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MR_REVIEW_TUICR", bin)
	t.Setenv("FAKE_TUICR_STATE", filepath.Join(t.TempDir(), "session.json"))
}

// TestHybridE2E — the §18 critical assertions in one flow:
//  1. manual draft comment exists while the bg review runs,
//  2. AI completes → findings appear as Pending (auto-reload),
//  3. manual draft survives,
//  4. only the approved finding is staged into tuicr
//     (rejected/pending never staged), and staging is local-only.
func TestHybridE2E(t *testing.T) {
	fakeTuicrScript(t, 55868)
	m := newMultiFileModel(t, 160, 45)

	// 1. human writes a manual draft while "AI" is running
	m.browseFilePath("services/x/handler.go")
	tap(m, "c")
	if m.draftEdit == nil {
		t.Fatal("manual comment editor must open during AI run")
	}
	m.draftEdit.ta.SetValue("my own note")
	tap(m, "ctrl+s")
	if len(m.comments) != 1 {
		t.Fatal("manual draft must be saved")
	}

	// 2-3. bg review completes → findings arrive as pending, draft survives
	if err := store.SaveFindings(m.dir, mockFindings(3)); err != nil {
		t.Fatal(err)
	}
	bgWriteState(t, m.dir, store.AgentState{Status: store.AgentDone, PID: os.Getpid(), Findings: 3})
	if !m.refreshBGState() {
		t.Fatal("done transition must be detected")
	}
	if err := m.reload(); err != nil {
		t.Fatal(err)
	}
	if len(m.findings) != 3 {
		t.Fatalf("AI findings must appear automatically, got %d", len(m.findings))
	}
	for _, idx := range m.filtered {
		if m.findings[idx].Decision != "pending" {
			t.Fatalf("AI findings must start pending, got %q", m.findings[idx].Decision)
		}
	}
	if len(m.comments) != 1 || m.comments[0].Text != "my own note" {
		t.Fatal("manual draft must survive AI completion")
	}

	// 4. approve F001, reject F002, leave F003 pending → stage in tuicr
	m.reselect("F001")
	tap(m, "a")
	m.reselect("F002")
	tap(m, "r")

	cmd := m.stageTuicrCmd()
	if cmd == nil {
		t.Fatal("staging action must be available with approved findings")
	}
	msg := cmd()
	done, ok := msg.(tuicrDoneMsg)
	if !ok {
		t.Fatalf("expected tuicrDoneMsg, got %T", msg)
	}
	if done.err != nil {
		t.Fatalf("staging failed: %v", done.err)
	}
	if !strings.Contains(done.note, "staged 1") {
		t.Fatalf("exactly one finding must be staged: %q", done.note)
	}

	// verify via the mapping store + the fake session
	mappings, err := tuicr.LoadMappings(m.dir)
	if err != nil {
		t.Fatal(err)
	}
	mp, ok := mappings.Get("F001")
	if !ok || mp.CommentID == "" {
		t.Fatal("F001 must be mapped to a tuicr comment")
	}
	if _, ok := mappings.Get("F002"); ok {
		t.Fatal("rejected finding must never be staged")
	}
	if _, ok := mappings.Get("F003"); ok {
		t.Fatal("pending finding must never be staged")
	}
	cli := tuicr.NewCLI()
	comments, err := cli.Comments(context.Background(), mp.Session, m.rev.Worktree)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 1 || !strings.Contains(comments[0].Content, "Please fix finding 1.") {
		t.Fatalf("tuicr session must contain exactly the approved comment, got %+v", comments)
	}

	// 5. manual comment still intact in the review dir
	cs, err := store.LoadComments(m.dir)
	if err != nil || len(cs) != 1 || cs[0].Text != "my own note" {
		t.Fatalf("manual draft lost: %v %+v", err, cs)
	}
}
