package gitlab

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseMRRefURL(t *testing.T) {
	for _, u := range []string{
		"https://gitlab.com/group/sub/proj/-/merge_requests/123",
		"http://gitlab.corp:8080/grp/proj/-/merge_requests/7",
		"https://gitlab.com/g/p/-/merge_requests/9.diff",
	} {
		ref, err := ParseMRRef(u, "")
		if err != nil {
			t.Fatalf("%s: %v", u, err)
		}
		if ref.IID <= 0 || ref.Project == "" || ref.Host == "" {
			t.Fatalf("%s parsed to %+v", u, ref)
		}
	}
}

func TestParseMRRefIIDFromRemote(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return string(out)
	}
	git("init", "-q", ".")
	git("remote", "add", "origin", "git@gitlab.example.com:grp/sub/proj.git")
	ref, err := ParseMRRef("77", dir)
	if err != nil {
		t.Fatalf("iid parse: %v", err)
	}
	if ref.Project != "grp/sub/proj" || ref.Host != "gitlab.example.com" || ref.IID != 77 {
		t.Fatalf("bad ref: %+v", ref)
	}
	// https form
	git("remote", "set-url", "origin", "https://gitlab.corp.io/team/mono.git")
	ref, err = ParseMRRef("5", dir)
	if err != nil || ref.Project != "team/mono" || ref.Host != "gitlab.corp.io" {
		t.Fatalf("https remote: %v %+v", err, ref)
	}
}

func TestParseMRRefRejectsGarbage(t *testing.T) {
	if _, err := ParseMRRef("not-a-mr", "/tmp"); err == nil {
		t.Fatal("garbage must be rejected")
	}
	if _, err := ParseMRRef("12", "/tmp/definitely-not-a-repo"); err == nil {
		t.Fatal("IID without repo must be rejected")
	}
}

func TestHostFromRemote(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	cmd := exec.Command("git", "init", "-q", ".")
	cmd.Dir = dir
	_ = cmd.Run()
	cmd = exec.Command("git", "remote", "add", "origin", "git@gitlab.com:foo/bar.git")
	cmd.Dir = dir
	_ = cmd.Run()
	if h := HostFromRemote(dir); h != "gitlab.com" {
		t.Fatalf("host: %q", h)
	}
	if h := HostFromRemote("/tmp"); h != "" {
		t.Fatalf("empty expected outside repo, got %q", h)
	}
	if !strings.Contains(EncPath("a/b"), "%2F") {
		t.Fatal("EncPath broken")
	}
}
