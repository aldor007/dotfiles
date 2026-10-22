package widget

import "fmt"

// IconSet carries the glyphs used across the UI. The ASCII set must
// byte-match the markers hardcoded before the icon layer existed (focus
// dots, decision markers, card cursor) so golden tests and plain terminals
// render exactly as before. The Unicode set is today's colored-mode output;
// the Nerd set is the opt-in vi-SQL-style decoration.
type IconSet struct {
	FocusOn    string // focused pane header dot
	FocusOff   string // unfocused pane header dot
	CardCursor string // selected finding card bar

	DecApproved  string // approved decision marker
	DecRejected  string // rejected decision marker
	DecPublished string // published decision marker
	DecStale     string // stale decision marker
	DecPending   string // pending decision marker

	TreeClosed string // collapsible dir, closed
	TreeOpen   string // collapsible dir, open
	Reviewed   string // reviewed file mark

	CommentPublished string // published draft marker
	CommentReady     string // ready draft marker
	CommentHeld      string // held draft marker
	FindingMark      string // AI finding row marker
	RemoteMark       string // remote discussion marker
}

// ASCII is the byte-compatible fallback set (used under NO_COLOR and as the
// per-glyph fallback of every other set).
func ASCII() IconSet {
	return IconSet{
		FocusOn:    "[*]",
		FocusOff:   "[ ]",
		CardCursor: ">",

		DecApproved:  "[x]",
		DecRejected:  "[-]",
		DecPublished: "[up]",
		DecStale:     "[?]",
		DecPending:   "[ ]",

		TreeClosed: "▸",
		TreeOpen:   "▾",
		Reviewed:   "✓",

		CommentPublished: "✓",
		CommentReady:     "●",
		CommentHeld:      "◌",
		FindingMark:      "✦",
		RemoteMark:       "≡",
	}
}

// Unicode is the default set for color-capable terminals (byte-identical to
// the pre-widget glyphs).
func Unicode() IconSet {
	return IconSet{
		FocusOn:    "●",
		FocusOff:   "○",
		CardCursor: "▌",

		DecApproved:  "✓",
		DecRejected:  "✗",
		DecPublished: "↑",
		DecStale:     "?",
		DecPending:   "·",

		TreeClosed: "▸",
		TreeOpen:   "▾",
		Reviewed:   "✓",

		CommentPublished: "✓",
		CommentReady:     "●",
		CommentHeld:      "◌",
		FindingMark:      "✦",
		RemoteMark:       "≡",
	}
}

// Nerd is the Nerd Fonts decoration set (vi-SQL icons.yaml spirit). Widths
// are single-cell; terminals without a Nerd font show tofu — that is why
// this set is strictly opt-in (icons: nerd).
func Nerd() IconSet {
	const (
		check       = "\uf00c" //  check
		times       = "\uf00d" //  times
		arrowUp     = "\uf062" //  arrow-up
		question    = "\uf059" //  question-circle
		circle      = "\uf111" //  circle
		circleO     = "\uf10c" //  circle-o
		folder      = "\uf07b" //  folder
		folderOpen  = "\uf07c" //  folder-open
		chevronR    = "\uf054" //  chevron-right
		chevronD    = "\uf078" //  chevron-down
		bolt        = "\uf0e7" //  bolt (AI)
		commentO    = "\uf0e5" //  comment-o (remote)
		checkCircle = "\uf058" //  check-circle (published)
	)
	return IconSet{
		FocusOn:    circle,
		FocusOff:   circleO,
		CardCursor: chevronR,

		DecApproved:  check,
		DecRejected:  times,
		DecPublished: arrowUp,
		DecStale:     question,
		DecPending:   circleO,

		TreeClosed: folder,
		TreeOpen:   folderOpen,
		Reviewed:   check,

		CommentPublished: checkCircle,
		CommentReady:     circle,
		CommentHeld:      circleO,
		FindingMark:      bolt,
		RemoteMark:       commentO,
	}
}

// ResolveIcons picks the icon set for a mode ("auto", "nerd", "ascii") and
// the plain-output flag. auto resolves to the byte-compatible ASCII set
// under NO_COLOR and to the Unicode set otherwise (exactly the pre-widget
// behavior); nerd is opt-in decoration with the same plain fallback.
func ResolveIcons(mode string, plain bool) (IconSet, error) {
	switch mode {
	case "", "auto":
		if plain {
			return ASCII(), nil
		}
		return Unicode(), nil
	case "ascii":
		return ASCII(), nil
	case "nerd":
		if plain {
			return ASCII(), nil
		}
		return Nerd(), nil
	default:
		return IconSet{}, fmt.Errorf("unknown icons mode %q (available: auto, nerd, ascii)", mode)
	}
}
