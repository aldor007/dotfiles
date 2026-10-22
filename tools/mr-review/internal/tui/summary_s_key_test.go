package tui

// S is contextual (vi-SQL-style one-key-per-context): inside the hybrid
// workspace it stages approved findings into tuicr; outside it edits the
// summary comment. The workspace footer advertises "S stage→tuicr" — this
// test keeps the binding honest.

import (
	"strings"
	"testing"

	"mr-review/internal/plex"
	"mr-review/internal/store"
)

func TestSummaryEditKeyIsContextual(t *testing.T) {
	dir := writeReviewFixture(t, t.TempDir())

	// approve F001 on disk so staging has an eligible finding
	fs, err := store.LoadFindings(dir)
	if err != nil || len(fs) == 0 {
		t.Fatalf("fixture findings: %v", err)
	}
	fs[0].Decision = "approved"
	if err := store.SaveFindings(dir, fs); err != nil {
		t.Fatal(err)
	}

	t.Run("outside workspace: S edits the summary", func(t *testing.T) {
		wsPane = nil
		defer func() { wsPane = nil }()
		m := newSessionModel(t, dir)
		a := m.lookup("S")
		if a == nil || a.ID != "view.summary-edit" {
			t.Fatalf("S outside workspace routed to %v, want view.summary-edit", a)
		}
		if cmd := a.Run(m); cmd == nil {
			t.Error("summary edit must produce its $EDITOR cmd")
		}
	})

	t.Run("workspace open: S stages into tuicr", func(t *testing.T) {
		wsPane = &plex.Pane{}
		defer func() { wsPane = nil }()

		m := newSessionModel(t, dir)
		a := m.lookup("S")
		if a == nil || a.ID != "tuicr.stage" {
			t.Fatalf("S in workspace routed to %v, want tuicr.stage", a)
		}
		if cmd := a.Run(m); cmd == nil {
			t.Error("staging must produce its background cmd")
		}
	})

	t.Run("workspace open without approved findings: S gives honest feedback", func(t *testing.T) {
		wsPane = &plex.Pane{}
		defer func() { wsPane = nil }()
		m := newSessionModel(t, writeReviewFixture(t, t.TempDir())) // fresh: both pending
		a := m.lookup("S")
		if a == nil || a.ID != "tuicr.stage" {
			t.Fatalf("S in workspace must always route to staging, got %v", a)
		}
		if cmd := a.Run(m); cmd != nil {
			t.Error("with nothing stageable Run must give feedback, not spawn staging")
		}
		if !strings.Contains(m.msg, "nothing approved to stage") {
			t.Errorf("feedback msg = %q", m.msg)
		}
	})
}

func TestSummaryEditStillInPaletteOutsideWorkspace(t *testing.T) {
	wsPane = nil
	defer func() { wsPane = nil }()
	m := newSessionModel(t, writeReviewFixture(t, t.TempDir()))
	for _, a := range enabledActions(m) {
		if a.ID == "view.summary-edit" {
			return
		}
	}
	t.Error("view.summary-edit must remain enabled outside the workspace")
}
