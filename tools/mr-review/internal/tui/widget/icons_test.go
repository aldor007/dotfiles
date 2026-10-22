package widget

import "testing"

// The ASCII and Unicode sets must byte-match the markers hardcoded before
// the icon layer existed — golden tests and plain terminals depend on it.
func TestSetsMatchLegacyMarkers(t *testing.T) {
	ascii := ASCII()
	wantASCII := IconSet{
		FocusOn: "[*]", FocusOff: "[ ]", CardCursor: ">",
		DecApproved: "[x]", DecRejected: "[-]", DecPublished: "[up]",
		DecStale: "[?]", DecPending: "[ ]",
		TreeClosed: "▸", TreeOpen: "▾", Reviewed: "✓",
		CommentPublished: "✓", CommentReady: "●", CommentHeld: "◌",
		FindingMark: "✦", RemoteMark: "≡",
	}
	if ascii != wantASCII {
		t.Errorf("ASCII set drifted:\n got %#v\nwant %#v", ascii, wantASCII)
	}
	uni := Unicode()
	wantUni := IconSet{
		FocusOn: "●", FocusOff: "○", CardCursor: "▌",
		DecApproved: "✓", DecRejected: "✗", DecPublished: "↑",
		DecStale: "?", DecPending: "·",
		TreeClosed: "▸", TreeOpen: "▾", Reviewed: "✓",
		CommentPublished: "✓", CommentReady: "●", CommentHeld: "◌",
		FindingMark: "✦", RemoteMark: "≡",
	}
	if uni != wantUni {
		t.Errorf("Unicode set drifted:\n got %#v\nwant %#v", uni, wantUni)
	}
}

func TestResolveIconsAuto(t *testing.T) {
	plain, err := ResolveIcons("auto", true)
	if err != nil {
		t.Fatalf("auto+plain: %v", err)
	}
	if plain != ASCII() {
		t.Error("auto+plain must resolve to the ASCII set (NO_COLOR byte-compat)")
	}
	color, err := ResolveIcons("auto", false)
	if err != nil {
		t.Fatalf("auto+color: %v", err)
	}
	if color != Unicode() {
		t.Error("auto+color must resolve to the Unicode set (byte-compat)")
	}
	// empty mode means auto
	if _, err := ResolveIcons("", false); err != nil {
		t.Errorf("empty mode: %v", err)
	}
}

func TestResolveIconsExplicit(t *testing.T) {
	if got, _ := ResolveIcons("ascii", false); got != ASCII() {
		t.Error("ascii mode must force the ASCII set even on color terminals")
	}
	if got, _ := ResolveIcons("nerd", false); got != Nerd() {
		t.Error("nerd mode must resolve to the Nerd set")
	}
	if got, _ := ResolveIcons("nerd", true); got != ASCII() {
		t.Error("nerd mode must fall back to ASCII under NO_COLOR")
	}
}

func TestResolveIconsUnknownMode(t *testing.T) {
	if _, err := ResolveIcons("emoji", false); err == nil {
		t.Fatal("unknown mode must error")
	}
}

func TestIconWidths(t *testing.T) {
	// Unicode and Nerd glyphs must occupy exactly one cell so row columns
	// stay aligned (rune-width 1 ≈ one cell for these BMP glyphs). ASCII
	// fallbacks are intentionally multi-cell strings — they replace glyphs
	// only in plain mode, where the legacy rendering measured them the same
	// way, so their exact legacy text is asserted by TestSetsMatchLegacyMarkers.
	sets := map[string]IconSet{"unicode": Unicode(), "nerd": Nerd()}
	for name, s := range sets {
		for _, g := range []string{
			s.FocusOn, s.FocusOff, s.CardCursor,
			s.DecApproved, s.DecRejected, s.DecPublished, s.DecStale, s.DecPending,
			s.TreeClosed, s.TreeOpen, s.Reviewed,
			s.CommentPublished, s.CommentReady, s.CommentHeld, s.FindingMark, s.RemoteMark,
		} {
			if n := len([]rune(g)); n != 1 {
				t.Errorf("%s: glyph %q is %d runes, want 1", name, g, n)
			}
		}
	}
	// ASCII set: non-empty, and equal to the legacy markers (already covered
	// above) — just assert every slot is non-empty.
	a := ASCII()
	for _, g := range []string{
		a.FocusOn, a.FocusOff, a.CardCursor,
		a.DecApproved, a.DecRejected, a.DecPublished, a.DecStale, a.DecPending,
		a.TreeClosed, a.TreeOpen, a.Reviewed,
		a.CommentPublished, a.CommentReady, a.CommentHeld, a.FindingMark, a.RemoteMark,
	} {
		if g == "" {
			t.Error("ASCII set has an empty slot")
		}
	}
}
