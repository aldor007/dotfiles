package tui

import "testing"

// TestFooterHintsResolve — the footer hints table (registry.go contextHints)
// must only reference keys the action registry actually serves in some
// context. Composite input hints ("j/k", "Ctrl+S") are mode labels, not
// registry keys, and are checked for non-emptiness only. This is the
// "footer can never drift from the registry" invariant.
func TestFooterHintsResolve(t *testing.T) {
	contexts := []struct {
		name string
		set  func(m *model)
	}{
		{"findings", func(m *model) {}},
		{"detail", func(m *model) { m.focus = focusDetail }},
		{"files", func(m *model) { m.setView(viewFiles) }},
		{"comments", func(m *model) { m.setView(viewComments) }},
		{"visual", func(m *model) { m.browseFilePath(changedFileOf(m)); m.startVisual() }},
		{"publish", func(m *model) { m.overlay = ovPublish }},
		{"palette", func(m *model) { m.paletteOn = true }},
	}
	resolvable := map[string]bool{}
	for _, tc := range contexts {
		m := newMultiFileModel(t, 120, 35)
		tc.set(m)
		for i := range enabledActions(m) {
			for _, k := range enabledActions(m)[i].Keys {
				resolvable[k] = true
			}
		}
	}
	composites := map[string]bool{
		"j/k": true, "↑/↓": true, "enter/space": true, "Ctrl+S": true,
		"Esc": true, "1/2/3": true, "[:]": true, "type": true,
		"h/l": true, "Enter": true, "esc": true,
	}
	for _, tc := range contexts {
		m := newMultiFileModel(t, 120, 35)
		tc.set(m)
		hints := m.footerHints()
		if len(hints) == 0 {
			t.Fatalf("%s: no footer hints", tc.name)
		}
		for _, h := range hints {
			if h.Keys == "" {
				t.Fatalf("%s: empty hint keys", tc.name)
			}
			if composites[h.Keys] {
				continue
			}
			if !resolvable[h.Keys] {
				t.Errorf("%s: hint key %q is not served by the registry", tc.name, h.Keys)
			}
		}
	}
}

// changedFileOf returns any changed file of the fixture model.
func changedFileOf(m *model) string {
	files := m.changedFiles()
	if len(files) == 0 {
		t_emptyFatal()
	}
	return files[0]
}

func t_emptyFatal() {}

func TestPrefixHintsFromRegistry(t *testing.T) {
	m := newMultiFileModel(t, 120, 35)
	m.pendingKey = "g"
	got := m.prefixCompletions("g")
	want := []string{"gP", "gd", "gg", "gn", "gp"} // sorted; gp/gP = next/prev pending
	if len(got) != len(want) {
		t.Fatalf("prefixCompletions(g) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("prefixCompletions(g) = %v, want %v", got, want)
		}
	}
	hints := m.footerHints()
	if len(hints) != 5 || hints[0].Keys != "gP" {
		t.Fatalf("footer hints while g pending = %v", hints)
	}
	// a prefix with no completions falls back to an empty bar, not garbage
	m.pendingKey = "z"
	if hints := m.footerHints(); len(hints) != 0 {
		t.Fatalf("footer hints for dead prefix = %v", hints)
	}
}
