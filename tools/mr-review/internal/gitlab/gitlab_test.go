package gitlab

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGetMR(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "glab")
	body := `{"iid":7,"title":"Add ctx","author":{"username":"alice"},
	  "source_branch":"feat/x","target_branch":"master","sha":"headsha1",
	  "state":"opened","diff_refs":{"base_sha":"basesha","start_sha":"startsha","head_sha":"headsha1"},
	  "source_project_id":42,"web_url":"https://gitlab.com/g/p/-/merge_requests/7",
	  "user_notes_count":3,"has_conflicts":false,"draft":false}`
	if err := os.WriteFile(bin, []byte("#!/bin/bash\ncat <<'EOF'\n"+body+"\nEOF\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := &Client{Bin: bin}
	mr, err := c.GetMR("g/p", 7)
	if err != nil {
		t.Fatalf("GetMR: %v", err)
	}
	if mr.IID != 7 || mr.Author.Username != "alice" || mr.DiffRefs.HeadSHA != "headsha1" {
		t.Fatalf("bad parse: %+v", mr)
	}
}

func TestGetMRErrorFailsClosed(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "glab")
	_ = os.WriteFile(bin, []byte("#!/bin/bash\necho '404 Not Found' >&2\nexit 1\n"), 0o755)
	c := &Client{Bin: bin}
	if _, err := c.GetMR("g/p", 999); err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected error mentioning 404, got %v", err)
	}
}

const postResponse = `{"id":"abc123","notes":[{"id":555,"body":"posted"}]}`

func TestPostDiscussionSingleLine(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "glab")
	log := filepath.Join(dir, "calls.log")
	script := "#!/bin/bash\nprintf '%s\\n' \"$@\" > " + log + "\ncat <<'EOF'\n" + postResponse + "\nEOF\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	c := &Client{Bin: bin}
	line := 42
	pos := &Position{NewPath: "a.go", OldPath: "a.go", NewLine: &line}
	drefs := DiffRefs{BaseSHA: "b", StartSHA: "s", HeadSHA: "h"}
	id, noteID, err := c.PostDiscussion("g/p", 7, "please fix", &drefs, pos)
	if err != nil || id != "abc123" || noteID != 555 {
		t.Fatalf("post: %v %s %d", err, id, noteID)
	}
	args, _ := os.ReadFile(log)
	sargs := string(args)
	for _, want := range []string{
		"-X", "POST",
		"position[new_path]=a.go", "position[new_line]=42",
		"position[base_sha]=b", "position[head_sha]=h", "body=please fix",
	} {
		if !strings.Contains(sargs, want) {
			t.Fatalf("missing %q in args:\n%s", want, sargs)
		}
	}
}

type lrBound struct {
	Line *int
	Type string
}

func makeRangePos(s, e int) *Position {
	sp, ep := s, e
	return &Position{
		NewPath: "a.go", OldPath: "a.go",
		LineRange: &struct {
			Start *struct {
				Line    *int   `json:"line,omitempty"`
				OldLine *int   `json:"old_line,omitempty"`
				Type    string `json:"type,omitempty"`
			} `json:"start,omitempty"`
			End *struct {
				Line    *int   `json:"line,omitempty"`
				OldLine *int   `json:"old_line,omitempty"`
				Type    string `json:"type,omitempty"`
			} `json:"end,omitempty"`
		}{
			Start: &struct {
				Line    *int   `json:"line,omitempty"`
				OldLine *int   `json:"old_line,omitempty"`
				Type    string `json:"type,omitempty"`
			}{Line: &sp, Type: "new"},
			End: &struct {
				Line    *int   `json:"line,omitempty"`
				OldLine *int   `json:"old_line,omitempty"`
				Type    string `json:"type,omitempty"`
			}{Line: &ep, Type: "new"},
		},
	}
}

func TestPostDiscussionLineRange(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "glab")
	log := filepath.Join(dir, "calls.log")
	script := "#!/bin/bash\nprintf '%s\\n' \"$@\" > " + log + "\ncat <<'EOF'\n" + postResponse + "\nEOF\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	c := &Client{Bin: bin}
	if _, _, err := c.PostDiscussion("g/p", 7, "range comment",
		&DiffRefs{BaseSHA: "b", StartSHA: "s", HeadSHA: "h"}, makeRangePos(5, 9)); err != nil {
		t.Fatalf("post: %v", err)
	}
	args, _ := os.ReadFile(log)
	for _, want := range []string{
		"position[line_range][start][type]=new",
		"position[line_range][start][line]=5",
		"position[line_range][end][line]=9",
	} {
		if !strings.Contains(string(args), want) {
			t.Fatalf("missing %q in:\n%s", want, args)
		}
	}
	_ = lrBound{}
}

func TestListOpenMRs(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "glab")
	body := `[{"iid":3,"title":"t3","author":{"username":"a"},"references":{"full":"g/p!3"}},
	          {"iid":2,"title":"t2","author":{"username":"b"},"references":{"full":"g/p!2"}}]`
	_ = os.WriteFile(bin, []byte("#!/bin/bash\ncat <<'EOF'\n"+body+"\nEOF\n"), 0o755)
	c := &Client{Bin: bin}
	items, err := c.ListOpenMRs("g/p", 5)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 2 || items[0].IID != 3 || items[0].Reference != "g/p!3" {
		t.Fatalf("bad list: %+v", items)
	}
}

func TestListLabeledAndAssigned(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "glab")
	log := filepath.Join(dir, "calls.log")
	body := `[{"iid":9,"title":"labeled change","author":{"username":"a"},
	          "labels":["code-review","go"],"references":{"full":"g/p!9"}}]`
	script := "#!/bin/bash\nprintf '%s\\n' \"$*\" > " + log + "\n" +
		"if [[ \"$*\" == *\"labels=\"* ]]; then\n" +
		"  cat <<'FIXTURE'\n" + body + "\nFIXTURE\n" +
		"elif [[ \"$*\" == *\"assignee_username\"* ]]; then\n  echo '[]'\nelse\n  echo '[]'\nfi\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	c := &Client{Bin: bin}

	items, err := c.ListLabeled("g/p", []string{"code-review", "go"}, 2)
	if err != nil {
		t.Fatalf("ListLabeled: %v", err)
	}
	if len(items) != 1 || items[0].IID != 9 {
		t.Fatalf("bad items: %+v", items)
	}
	if len(items[0].Labels) != 2 || items[0].Labels[0] != "code-review" {
		t.Fatalf("labels not parsed: %+v", items[0].Labels)
	}
	args, _ := os.ReadFile(log)
	if !strings.Contains(string(args), "labels=code-review%2Cgo") {
		t.Fatalf("labels param missing/malformed:\n%s", args)
	}

	if _, err := c.ListAssigned("g/p", "me", 2); err != nil {
		t.Fatalf("ListAssigned: %v", err)
	}
	args, _ = os.ReadFile(log)
	if !strings.Contains(string(args), "assignee_username=me") {
		t.Fatalf("assignee param missing:\n%s", args)
	}
	// empty labels falls back to open MRs
	if _, err := c.ListLabeled("g/p", nil, 2); err != nil {
		t.Fatalf("empty labels: %v", err)
	}
}

func TestParseAndEnvHost(t *testing.T) {
	c := &Client{Bin: "glab", Host: "gitlab.corp"}
	if got := EncPath("grp/sub/proj"); got != "grp%2Fsub%2Fproj" {
		t.Fatalf("EncPath: %s", got)
	}
	env := c.env()
	found := false
	for _, kv := range env {
		if kv == "GITLAB_HOST=gitlab.corp" {
			found = true
		}
	}
	if !found {
		t.Fatal("GITLAB_HOST not injected")
	}
}

func TestGetMRTolerantFieldShapes(t *testing.T) {
	// regression: real GitLab instances return detailed_merge_status as a
	// plain string; the struct must accept string, object and null shapes.
	dir := t.TempDir()
	bin := filepath.Join(dir, "glab")
	body := `{"iid":8,"title":"T","author":{"username":"a"},
	  "state":"opened","sha":"h1",
	  "diff_refs":{"base_sha":"b","start_sha":"s","head_sha":"h1"},
	  "detailed_merge_status":"ci_still_running",
	  "head_pipeline":{"status":"running","web_url":"u"}}`
	if err := os.WriteFile(bin, []byte("#!/bin/bash\ncat <<'EOF'\n"+body+"\nEOF\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := &Client{Bin: bin}
	mr, err := c.GetMR("g/p", 8)
	if err != nil {
		t.Fatalf("string shape: %v", err)
	}
	if mr.DetailedMergeStatus.Value != "ci_still_running" {
		t.Fatalf("flex string: %+v", mr.DetailedMergeStatus)
	}

	// object shape (older GitLab)
	body2 := strings.Replace(body, `"ci_still_running"`, `{"label":"ci_still_running"}`, 1)
	_ = os.WriteFile(bin, []byte("#!/bin/bash\ncat <<'EOF'\n"+body2+"\nEOF\n"), 0o755)
	if mr, err = c.GetMR("g/p", 8); err != nil || mr.DetailedMergeStatus.Value != "ci_still_running" {
		t.Fatalf("object shape: %v %+v", err, mr)
	}

	// null shape must also never fail
	body3 := strings.Replace(body, `"ci_still_running"`, `null`, 1)
	_ = os.WriteFile(bin, []byte("#!/bin/bash\ncat <<'EOF'\n"+body3+"\nEOF\n"), 0o755)
	if mr, err = c.GetMR("g/p", 8); err != nil {
		t.Fatalf("null shape: %v", err)
	}
}

// ---- P2 #5: paginated per-file diff listing ---------------------------------

// diffsStub installs a glab stub serving the MR diffs endpoint. page1 is
// always served for page=1; page2 (if non-empty) for page=2; other pages
// return []. Returns (bin, log).
func diffsStub(t *testing.T, dir, page1, page2 string) (string, string) {
	t.Helper()
	log := filepath.Join(dir, "calls.log")
	bin := filepath.Join(dir, "glab")
	script := "#!/bin/bash\nprintf '%s\\n' \"$*\" >> " + log + "\ncase \"$*\" in\n"
	if page2 != "" {
		script += "  *'page=2') cat <<'EOF'\n" + page2 + "\nEOF\n;;\n"
	}
	script += "  *'page=1') cat <<'EOF'\n" + page1 + "\nEOF\n;;\n  *) echo '[]' ;;\nesac\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, log
}

func TestMRDiffsFollowsPagesUntilShortPage(t *testing.T) {
	dir := t.TempDir()
	var entries []string
	for i := 1; i <= 100; i++ { // a FULL page: the loop must fetch page 2
		entries = append(entries, fmt.Sprintf(`{"old_path":"f%03d.go","new_path":"f%03d.go","diff":"x"}`, i, i))
	}
	page1 := "[" + strings.Join(entries, ",") + "]"
	page2 := `[{"old_path":"tail.go","new_path":"tail.go","diff":"y"}]`
	bin, log := diffsStub(t, dir, page1, page2)
	c := &Client{Bin: bin}

	diffs, err := c.MRDiffs("g/p", 7)
	if err != nil {
		t.Fatalf("MRDiffs: %v", err)
	}
	if len(diffs) != 101 {
		t.Fatalf("want 101 diffs across two pages, got %d", len(diffs))
	}
	calls, _ := os.ReadFile(log)
	if !strings.Contains(string(calls), "merge_requests/7/diffs?per_page=100&page=1") ||
		!strings.Contains(string(calls), "page=2") {
		t.Fatalf("pagination calls missing:\n%s", calls)
	}
	if strings.Contains(string(calls), "page=3") {
		t.Fatalf("pagination must stop on a short page:\n%s", calls)
	}
}

func TestMRDiffsParsesRenamesAndTooLarge(t *testing.T) {
	dir := t.TempDir()
	body := `[
	  {"old_path":"a.go","new_path":"a.go","diff":"@@ -1 +1 @@\\n-a\\n+b\\n"},
	  {"old_path":"old_name.go","new_path":"new_name.go","renamed_file":true,
	   "diff":"rename from old_name.go\\nrename to new_name.go\\n"},
	  {"old_path":"big.bin","new_path":"big.bin","too_large":true,"diff":""},
	  {"old_path":"gone.go","new_path":"gone.go","deleted_file":true,
	   "diff":"@@ -1 +0,0 @@\\n-x\\n"}
	]`
	bin, _ := diffsStub(t, dir, body, "")
	c := &Client{Bin: bin}

	diffs, err := c.MRDiffs("g/p", 7)
	if err != nil {
		t.Fatalf("MRDiffs: %v", err)
	}
	if len(diffs) != 4 {
		t.Fatalf("want 4 entries (incl. collapsed/too-large), got %d", len(diffs))
	}
	ren := diffs[1]
	if !ren.RenamedFile || ren.OldPath != "old_name.go" || ren.NewPath != "new_name.go" {
		t.Fatalf("rename not parsed: %+v", ren)
	}
	if !diffs[2].TooLarge || !diffs[2].DiffUnavailable() {
		t.Fatalf("too-large entry must keep the file but mark the diff: %+v", diffs[2])
	}
	if !diffs[3].DeletedFile {
		t.Fatalf("deletion not parsed: %+v", diffs[3])
	}
}

func TestChangedFilesOneEntryPerFile(t *testing.T) {
	dir := t.TempDir()
	// rename counts the NEW path exactly once (the old path is NOT listed);
	// too-large and deleted files still count their file
	body := `[
	  {"old_path":"a.go","new_path":"a.go","diff":"x"},
	  {"old_path":"old_name.go","new_path":"new_name.go","renamed_file":true,"diff":"x"},
	  {"old_path":"big.bin","new_path":"big.bin","too_large":true,"diff":""},
	  {"old_path":"gone.go","new_path":"gone.go","deleted_file":true,"diff":"x"},
	  {"old_path":"new_name.go","new_path":"new_name.go","diff":"x"}
	]`
	bin, _ := diffsStub(t, dir, body, "")
	c := &Client{Bin: bin}

	got, err := c.ChangedFiles("g/p", 7)
	if err != nil {
		t.Fatalf("ChangedFiles: %v", err)
	}
	want := []string{"a.go", "new_name.go", "big.bin", "gone.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want exactly one entry per file %v, got %v", want, got)
	}
}
