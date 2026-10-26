package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mr-review/internal/store"
)

func TestResolveProjectArg(t *testing.T) {
	t.Setenv(store.EnvHome, t.TempDir())

	// bare group/project → default host
	host, project, err := resolveProjectArg("acme/widgets")
	if err != nil || host != "gitlab.com" || project != "acme/widgets" {
		t.Fatalf("group/project: host=%q project=%q err=%v", host, project, err)
	}

	// host/group/project
	host, project, err = resolveProjectArg("gitlab.mycompany.com/team/svc")
	if err != nil || host != "gitlab.mycompany.com" || project != "team/svc" {
		t.Fatalf("host/group/project: host=%q project=%q err=%v", host, project, err)
	}

	// review dir with review.json wins over ref parsing
	dir := t.TempDir()
	rev := &store.Review{Project: "from/dir", IID: 1, Host: "gitlab.example.com", HeadSHA: "abc"}
	if err := rev.Save(dir); err != nil {
		t.Fatal(err)
	}
	host, project, err = resolveProjectArg(dir)
	if err != nil || host != "gitlab.example.com" || project != "from/dir" {
		t.Fatalf("review dir: host=%q project=%q err=%v", host, project, err)
	}

	// garbage is an error, not a guess
	if _, _, err := resolveProjectArg("not-a-project"); err == nil {
		t.Fatal("expected error for unresolvable arg")
	}
}

func TestDefaultBookName(t *testing.T) {
	cases := map[string]string{
		"acme/widgets":      "book-of-widgets",
		"castai/kubecast":   "book-of-kubecast",
		"g/some-repo.name":  "book-of-some-repo-name",
		"g/Trailing/Slash/": "book-of-slash",
	}
	for in, want := range cases {
		if got := defaultBookName(in); got != want {
			t.Errorf("defaultBookName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRunContextGen(t *testing.T) {
	t.Setenv(store.EnvHome, t.TempDir())

	// stub agent: "explores" the repo by writing the pack the prompt names
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "fakepi")
	script := `#!/bin/bash
out=$(grep -oE "/[^ ]+\.md" | head -1)
mkdir -p "$(dirname "$out")"
printf "# generated pack\n\nfacts here\n" > "$out"
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MR_REVIEW_AGENT", bin)

	if err := runContextGen(context.Background(), "gitlab.com", "acme/widgets", repo, "", "", time.Minute); err != nil {
		t.Fatalf("gen: %v", err)
	}
	pack := filepath.Join(store.ContextDir("gitlab.com", "acme/widgets"), "book-of-widgets.md")
	b, err := os.ReadFile(pack)
	if err != nil || !strings.Contains(string(b), "generated pack") {
		t.Fatalf("agent-written pack missing (%v): %s", err, b)
	}

	// gen never overwrites an existing note
	err = runContextGen(context.Background(), "gitlab.com", "acme/widgets", repo, "", "", time.Minute)
	if err == nil || !strings.Contains(err.Error(), "never overwrites") {
		t.Fatalf("second gen must refuse, got %v", err)
	}

	// agent that writes nothing → honest failure pointing at the log
	bad := filepath.Join(t.TempDir(), "badpi")
	if err := os.WriteFile(bad, []byte("#!/bin/bash\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MR_REVIEW_AGENT", bad)
	err = runContextGen(context.Background(), "gitlab.com", "acme/other", repo, "", "", time.Minute)
	if err == nil || !strings.Contains(err.Error(), "was not written") {
		t.Fatalf("silent agent must fail honestly, got %v", err)
	}
}

func TestLoadGenPrompt(t *testing.T) {
	t.Setenv(store.EnvHome, t.TempDir())
	cdir := store.ContextDir("gitlab.com", "g/p")
	target := filepath.Join(cdir, "book-of-x.md")

	// first load seeds the editable file from the built-in
	prompt, source, err := loadGenPrompt(cdir, "", "g/p", target)
	if err != nil {
		t.Fatal(err)
	}
	if source != filepath.Join(cdir, GenPromptFile) {
		t.Fatalf("prompt source = %q, want the seeded file", source)
	}
	if !strings.Contains(prompt, "g/p") || !strings.Contains(prompt, target) {
		t.Fatalf("tokens not substituted:\n%s", prompt)
	}
	seeded, _ := os.ReadFile(source)
	if !strings.Contains(string(seeded), "{{PROJECT}}") {
		t.Fatal("the seeded FILE must keep raw tokens for editing")
	}

	// user edits are honored verbatim (plus OUTPUT re-appended when lost)
	if err := os.WriteFile(source, []byte("my custom prompt, be terse"), 0o644); err != nil {
		t.Fatal(err)
	}
	prompt, _, err = loadGenPrompt(cdir, "", "g/p", target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "be terse") || !strings.Contains(prompt, target) {
		t.Fatalf("custom prompt lost or OUTPUT not appended:\n%s", prompt)
	}

	// empty custom prompt fails loudly
	if err := os.WriteFile(source, []byte("   "), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadGenPrompt(cdir, "", "g/p", target); err == nil {
		t.Fatal("empty prompt must error")
	}

	// --prompt one-off file wins over the seeded one
	oneoff := filepath.Join(t.TempDir(), "p.md")
	if err := os.WriteFile(oneoff, []byte("one-off {{PROJECT}} {{OUTPUT}}"), 0o644); err != nil {
		t.Fatal(err)
	}
	prompt, source, err = loadGenPrompt(cdir, oneoff, "g/p", target)
	if err != nil || source != oneoff || !strings.Contains(prompt, target) {
		t.Fatalf("--prompt: source=%q err=%v prompt=%q", source, err, prompt)
	}
}

func TestResolveGenRepo(t *testing.T) {
	t.Setenv(store.EnvHome, t.TempDir())
	home, _ := os.UserHomeDir()

	// --repo wins and must be a git checkout
	if _, err := resolveGenRepo("g/p", t.TempDir()); err == nil {
		t.Fatal("non-git --repo must be rejected")
	}
	gitdir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(gitdir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := resolveGenRepo("g/p", gitdir)
	if err != nil || got != gitdir {
		t.Fatalf("--repo: got %q err %v", got, err)
	}

	// no --repo: ~/work/<base> heuristic
	heuristic := filepath.Join(home, "work", "widgets")
	if err := os.MkdirAll(filepath.Join(heuristic, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err = resolveGenRepo("acme/widgets", "")
	if err != nil || got != heuristic {
		t.Fatalf("heuristic: got %q err %v", got, err)
	}

	// nothing found → actionable error
	if _, err := resolveGenRepo("acme/missing", ""); err == nil || !strings.Contains(err.Error(), "--repo") {
		t.Fatalf("missing checkout must suggest --repo, got %v", err)
	}
}

func TestAddContextNote(t *testing.T) {
	t.Setenv(store.EnvHome, t.TempDir())

	// add into an existing section of a scaffolded note
	path, err := addContextNote("gitlab.com", "g/p", "context", "gotchas", "watch the CI child pipelines")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path)
	s := string(body)
	if !strings.Contains(s, "- watch the CI child pipelines") {
		t.Fatalf("bullet missing:\n%s", s)
	}
	// must sit inside the gotchas section (after its heading, before EOF)
	gi := strings.Index(s, "## Review gotchas")
	bi := strings.Index(s, "- watch the CI child pipelines")
	if gi < 0 || bi < gi {
		t.Fatalf("bullet not under gotchas section:\n%s", s)
	}

	// second add appends to the same section, before any later heading
	if _, err := addContextNote("gitlab.com", "g/p", "context", "gotchas", "second note"); err != nil {
		t.Fatal(err)
	}
	body, _ = os.ReadFile(path)
	s = string(body)
	if strings.Index(s, "- second note") < strings.Index(s, "- watch the CI") {
		t.Fatalf("appends must preserve order:\n%s", s)
	}

	// unknown section is created at the end
	if _, err := addContextNote("gitlab.com", "g/p", "context", "blast radius", " touching charts affects prod"); err != nil {
		t.Fatal(err)
	}
	body, _ = os.ReadFile(path)
	s = string(body)
	if !strings.Contains(s, "## blast radius\n\n- touching charts affects prod") {
		t.Fatalf("new section missing:\n%s", s)
	}

	// no section: appended at end of file
	if _, err := addContextNote("gitlab.com", "g/p", "context", "", "free-form tail note"); err != nil {
		t.Fatal(err)
	}
	body, _ = os.ReadFile(path)
	s = string(body)
	if !strings.HasSuffix(strings.TrimRight(s, "\n"), "free-form tail note") {
		t.Fatalf("free-form add must land at EOF:\n%s", s)
	}

	// add to a non-existent note scaffolds first
	path2, err := addContextNote("gitlab.com", "g/p", "book-of-orders", "invariants", "plans are resumable")
	if err != nil {
		t.Fatal(err)
	}
	body2, _ := os.ReadFile(path2)
	if !strings.Contains(string(body2), "- plans are resumable") ||
		!strings.Contains(string(body2), "## Invariants") {
		t.Fatalf("scaffold+add failed:\n%s", body2)
	}

	// empty text is rejected
	if _, err := addContextNote("gitlab.com", "g/p", "context", "", "   "); err == nil {
		t.Fatal("empty text must error")
	}
}

func TestScaffoldContext(t *testing.T) {
	t.Setenv(store.EnvHome, t.TempDir())

	path, created, err := scaffoldContext("gitlab.com", "acme/widgets", "book-of-orders")
	if err != nil || !created {
		t.Fatalf("first scaffold: created=%v err=%v", created, err)
	}
	if !strings.HasPrefix(path, store.ContextDir("gitlab.com", "acme/widgets")+string(filepath.Separator)) ||
		filepath.Base(path) != "book-of-orders.md" {
		t.Fatalf("unexpected path %q", path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Book of orders", "Invariants", "Sensitive areas", "acme/widgets"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("skeleton missing %q:\n%s", want, body)
		}
	}

	// second scaffold with a different name must not touch the first
	if err := os.WriteFile(path, []byte("my notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	path2, created2, err := scaffoldContext("gitlab.com", "acme/widgets", "book-of-orders")
	if err != nil || created2 || path2 != path {
		t.Fatalf("second scaffold must be a no-op: created=%v path=%s err=%v", created2, path2, err)
	}
	if b, _ := os.ReadFile(path); string(b) != "my notes" {
		t.Fatalf("existing note was overwritten:\n%s", b)
	}

	// name is sanitized: no traversal, no double suffix
	path3, _, err := scaffoldContext("gitlab.com", "acme/widgets", "../evil.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path3, store.ContextDir("gitlab.com", "acme/widgets")+string(filepath.Separator)) ||
		filepath.Base(path3) != "evil.md" {
		t.Fatalf("name must be sanitized into the context dir, got %q", path3)
	}
}
