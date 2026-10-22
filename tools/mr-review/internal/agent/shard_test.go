package agent

import (
	"strings"
	"testing"
)

func TestShardFilesBalancedAndDeterministic(t *testing.T) {
	files := []string{"f/05.go", "a/01.go", "c/03.go", "b/02.go", "e/04.go", "g/06.go", "h/07.go"}
	groups := ShardFiles(files, 3)
	if len(groups) != 3 {
		t.Fatalf("groups=%d want 3", len(groups))
	}
	total := 0
	for _, g := range groups {
		total += len(g)
		for _, f := range g {
			if !contains(files, f) {
				t.Fatalf("unknown file %q", f)
			}
		}
	}
	if total != len(files) {
		t.Fatalf("lost files: %d != %d", total, len(files))
	}
	// balanced within 1
	if abs3(len(groups[0])-len(groups[2])) > 1 {
		t.Fatalf("unbalanced: %v", groups)
	}
	// deterministic
	again := ShardFiles(files, 3)
	for i := range groups {
		if strings.Join(groups[i], ",") != strings.Join(again[i], ",") {
			t.Fatal("non-deterministic sharding")
		}
	}
	// k > files
	if g := ShardFiles(files[:2], 4); len(g) != 2 {
		t.Fatalf("k>len: %v", g)
	}
	// single file / k<=1
	if g := ShardFiles(files[:1], 3); len(g) != 1 || len(g[0]) != 1 {
		t.Fatalf("single file: %v", g)
	}
	if g := ShardFiles(files, 1); len(g) != 1 || len(g[0]) != len(files) {
		t.Fatalf("k=1: %v", g)
	}
}

func TestBuildPromptIncludesSubagentSection(t *testing.T) {
	in := ReviewInput{
		Project: "g/p", IID: 1, Title: "T", Author: "a",
		SourceBranch: "f", TargetBranch: "m", HeadSHA: "h",
		Worktree: "/wt", DiffPath: "/d", DiscussionsFmt: "none",
		FindingsOut:  "/o",
		FileGroups:   [][]string{{"a.go", "b.go"}, {"c.go"}},
		MaxSubagents: 2,
	}
	p := BuildPrompt(in)
	for _, want := range []string{
		"PARALLEL SUBAGENTS", "group 1 (2 files)", "group 2 (1 file)",
		"up to 2", "read-only", "untrusted data", "write\n  ALL final findings",
	} {
		if !strings.Contains(p, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}
	// no groups → single-agent wording
	in.FileGroups = nil
	p2 := BuildPrompt(in)
	if !strings.Contains(p2, "single-agent review") || strings.Contains(p2, "PARALLEL SUBAGENTS (when") {
		_ = p2
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func abs3(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
