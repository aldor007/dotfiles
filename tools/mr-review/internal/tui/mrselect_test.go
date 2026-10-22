package tui

import (
	"encoding/json"
	"testing"
	"time"

	"mr-review/internal/gitlab"
)

func row(iid int, title, author, branch string, age time.Duration, labels ...string) MRRow {
	return MRRow{
		IID: iid, Title: title, Author: author, SourceBranch: branch,
		TargetBranch: "master", Labels: labels,
		UpdatedAt: time.Now().Add(-age),
	}
}

func TestMatchMRQueryFreeText(t *testing.T) {
	r := row(1, "abort binary search on ctx expiry", "matas.k", "rev-755-fix", 2*time.Hour)
	if !MatchMRQuery(r, "") {
		t.Fatal("empty query matches everything")
	}
	if !MatchMRQuery(r, "binary") {
		t.Fatal("free text should match title")
	}
	if !MatchMRQuery(r, "REV-755") {
		t.Fatal("free text should match branch (case-insensitive)")
	}
	if MatchMRQuery(r, "nonexistent") {
		t.Fatal("non-matching text must reject")
	}
}

func TestMatchMRQueryAuthor(t *testing.T) {
	r := row(1, "t", "matas.k", "b", time.Hour)
	if !MatchMRQuery(r, "author:matas") {
		t.Fatal("author substring should match")
	}
	if MatchMRQuery(r, "author:alice") {
		t.Fatal("wrong author must reject")
	}
	// combined tokens are ANDed
	if !MatchMRQuery(r, "author:matas t") {
		t.Fatal("author + free text must AND")
	}
	if MatchMRQuery(r, "author:matas nonexistent") {
		t.Fatal("AND semantics broken")
	}
}

func TestMatchMRQueryAge(t *testing.T) {
	r2h := row(1, "t", "a", "b", 2*time.Hour)
	r3d := row(2, "t", "a", "b", 3*24*time.Hour)
	if !MatchMRQuery(r2h, "age:<7d") {
		t.Fatal("2h old must pass age:<7d")
	}
	if !MatchMRQuery(r3d, "age:<7d") {
		t.Fatal("3d old must pass age:<7d")
	}
	if MatchMRQuery(r2h, "age:>1d") {
		t.Fatal("2h old must fail age:>1d")
	}
	if !MatchMRQuery(r3d, "age:>1d") {
		t.Fatal("3d old must pass age:>1d")
	}
}

func TestMatchMRQueryLabel(t *testing.T) {
	r := row(1, "t", "a", "b", time.Hour, "team::rev", "go")
	if !MatchMRQuery(r, "label:rev") {
		t.Fatal("label substring should match")
	}
	if MatchMRQuery(r, "label:rust") {
		t.Fatal("missing label must reject")
	}
}

func TestRelAge(t *testing.T) {
	now := time.Now()
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "now"},
		{5 * time.Minute, "5m"},
		{3 * time.Hour, "3h"},
		{2 * 24 * time.Hour, "2d"},
		{45 * 24 * time.Hour, "1mo"},
	}
	for _, c := range cases {
		if got := RelAge(now.Add(-c.d), now); got != c.want {
			t.Errorf("RelAge(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestMRListUpdatedAtParsing(t *testing.T) {
	// GitLab returns RFC3339 timestamps; they must survive JSON decoding so
	// age filters work on real data.
	b := []byte(`{"iid":7,"title":"t","author":{"username":"a"},"updated_at":"2026-10-20T10:11:12.123Z"}`)
	var it gitlab.MRListItem
	if err := json.Unmarshal(b, &it); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if it.UpdatedAt.IsZero() {
		t.Fatal("updated_at not parsed")
	}
}
