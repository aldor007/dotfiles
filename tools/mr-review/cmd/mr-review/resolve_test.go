package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mr-review/internal/store"
)

func resolveFixture(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("MR_REVIEW_HOME", home)
	mk := func(project string, iid int) string {
		dir := store.ReviewDir("gitlab.example.com", project, iid)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		r := store.Review{Project: project, IID: iid, Host: "gitlab.example.com",
			HeadSHA: "fixed0sha123456", State: "reviewing"}
		if err := r.Save(dir); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	mk("grp/proj", 42)  // for the URL case
	mk("grp/alpha", 7)  // unique bare-number case
	mk("grp/beta", 7)   // makes !7 ambiguous
	mk("other/one", 99) // unrelated
	return home
}

func TestResolveReviewDir(t *testing.T) {
	resolveFixture(t) // sets MR_REVIEW_HOME; paths come from store.ReviewDir

	// existing review dir: passthrough — use the real store path (host-encoded
	// slug: store.ReviewDir → gitlab_2eexample_2ecom_grp_2falpha/mr7); a bare
	// grp_alpha/mr7 without review.json is correctly NOT a review dir
	dir := store.ReviewDir("gitlab.example.com", "grp/alpha", 7)
	got, err := resolveReviewDir(dir)
	if err != nil || got != dir {
		t.Fatalf("dir passthrough: %q %v", got, err)
	}

	// MR URL: full coordinates
	url := "https://gitlab.example.com/grp/proj/-/merge_requests/42"
	want := store.ReviewDir("gitlab.example.com", "grp/proj", 42)
	got, err = resolveReviewDir(url)
	if err != nil || got != want {
		t.Fatalf("url: got %q want %q (%v)", got, want, err)
	}

	// bare MR number, unique across projects
	wantUnique := store.ReviewDir("gitlab.example.com", "other/one", 99)
	got, err = resolveReviewDir("99")
	if err != nil || got != wantUnique {
		t.Fatalf("unique number: got %q want %q (%v)", got, wantUnique, err)
	}

	// bare MR number, ambiguous: error lists candidates
	_, err = resolveReviewDir("7")
	if err == nil || !strings.Contains(err.Error(), "2 projects") {
		t.Fatalf("ambiguous number: %v", err)
	}
	if !strings.Contains(err.Error(), "grp_2falpha") || !strings.Contains(err.Error(), "grp_2fbeta") {
		t.Errorf("ambiguous error must list candidates (host-encoded slugs): %v", err)
	}

	// bare MR number with no review: error with a start hint
	_, err = resolveReviewDir("54231")
	if err == nil || !strings.Contains(err.Error(), "no in-progress review for MR !54231") {
		t.Fatalf("missing number: %v", err)
	}

	// URL with no in-progress review: start hint
	_, err = resolveReviewDir("https://gitlab.example.com/grp/proj/-/merge_requests/43")
	if err == nil || !strings.Contains(err.Error(), "start one") {
		t.Fatalf("missing url: %v", err)
	}

	// garbage
	if _, err = resolveReviewDir("not-a-thing"); err == nil {
		t.Fatal("garbage must error")
	}
}

func TestResolveReviewDirAmbiguousHostSlugs(t *testing.T) {
	// The same MR may legitimately exist under several host slugs in one
	// state home (legacy single-arg slug vs the current host-escaped one);
	// resolution must yield a unique dir, or an ambiguity error that lists
	// the candidates. Fixture-based so it runs on any machine.
	home := t.TempDir()
	t.Setenv("MR_REVIEW_HOME", home)

	rev := &store.Review{Project: "acme/widgets", IID: 54231, Host: "gitlab.com", HeadSHA: "abc"}
	legacy := filepath.Join(home, "acme_widgets", "mr54231")
	modern := filepath.Join(home, "gitlab_2ecom_acme_2fwidgets", "mr54231")
	for _, d := range []string{legacy, modern} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := rev.Save(d); err != nil {
			t.Fatal(err)
		}
	}
	_, err := resolveReviewDir("54231")
	if err == nil || !strings.Contains(err.Error(), "2 projects") || !strings.Contains(err.Error(), "mr54231") {
		t.Fatalf("two host slugs must produce an ambiguity error listing candidates, got %v", err)
	}

	// a single slug resolves cleanly
	if err := os.RemoveAll(legacy); err != nil {
		t.Fatal(err)
	}
	got, err := resolveReviewDir("54231")
	if err != nil {
		t.Fatalf("single slug must resolve: %v", err)
	}
	if !strings.HasSuffix(got, filepath.Join("gitlab_2ecom_acme_2fwidgets", "mr54231")) {
		t.Errorf("resolution = %q", got)
	}
}
