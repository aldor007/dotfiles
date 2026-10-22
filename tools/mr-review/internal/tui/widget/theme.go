// Package widget holds reusable, model-agnostic TUI building blocks for
// mr-review's Bubble Tea UI: themes, icons, scroll/window math and chrome
// renderers. It is styled after vi-SQL's widget layer (core styles + icons +
// widget/component split) but implemented with lipgloss v2 primitives.
//
// One-way dependency rule: this package must never import internal/tui.
package widget

import (
	"fmt"
	"image/color"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
)

// Token is one semantic color slot of the theme. Everything visual derives
// from these tokens so focus/selection/state colors stay consistent across
// panels (vi-SQL styles.yaml semantics, scoped to semantic tokens).
type Token string

const (
	TokFocus           Token = "Focus"           // focused border / active accent
	TokOK              Token = "OK"              // approved / ready / added
	TokWarn            Token = "Warn"            // pending / modified / warnings
	TokErr             Token = "Err"             // errors / deleted
	TokPub             Token = "Pub"             // published
	TokHeld            Token = "Held"            // rejected / held / dim accent
	TokAccent          Token = "Accent"          // selection text
	TokSection         Token = "Section"         // section headers
	TokBorder          Token = "Border"          // unfocused borders
	TokBorderFocus     Token = "BorderFocus"     // focused borders
	TokMarker          Token = "Marker"          // selection markers
	TokSel             Token = "Sel"             // selected text
	TokBranch          Token = "Branch"          // branch names
	TokSevP0           Token = "SevP0"           // severity P0
	TokSevP1           Token = "SevP1"           // severity P1
	TokSevP2           Token = "SevP2"           // severity P2
	TokSevP3           Token = "SevP3"           // severity P3
	TokDecisionPending Token = "DecisionPending" // pending decision label
	TokTabActiveBg     Token = "TabActiveBg"     // active tab background
	TokTabActiveFg     Token = "TabActiveFg"     // active tab foreground
)

// AllTokens lists every token (for validation and docs).
func AllTokens() []string {
	out := []string{
		string(TokFocus), string(TokOK), string(TokWarn), string(TokErr),
		string(TokPub), string(TokHeld), string(TokAccent), string(TokSection),
		string(TokBorder), string(TokBorderFocus), string(TokMarker),
		string(TokSel), string(TokBranch), string(TokSevP0), string(TokSevP1),
		string(TokSevP2), string(TokSevP3), string(TokDecisionPending),
		string(TokTabActiveBg), string(TokTabActiveFg),
	}
	sort.Strings(out)
	return out
}

// Theme is a named set of semantic colors. Values are lipgloss color
// strings: 256-color indexes ("39") or hex ("#bd93f9"). lipgloss v2 degrades
// hex values to the terminal's color profile automatically; 256-color values
// are passed through, which keeps the default theme byte-identical to the
// pre-widget rendering.
type Theme struct {
	Name   string
	colors map[Token]string
}

// Color resolves one token to a color for lipgloss styles.
func (t *Theme) Color(tok Token) color.Color {
	if v, ok := t.colors[tok]; ok {
		return lipgloss.Color(v)
	}
	return lipgloss.Color("")
}

// Raw returns the raw color string of one token ("" when unset).
func (t *Theme) Raw(tok Token) string { return t.colors[tok] }

// With sets one token and returns the theme (builder-style, for overrides).
func (t *Theme) With(tok Token, value string) *Theme {
	t.colors[tok] = value
	return t
}

// Default is the original mr-review palette (256-color indexes exactly as
// they were hardcoded before the theme engine existed).
func Default() *Theme {
	return &Theme{Name: "default", colors: map[Token]string{
		TokFocus:           "39",
		TokOK:              "78",
		TokWarn:            "214",
		TokErr:             "196",
		TokPub:             "213",
		TokHeld:            "243",
		TokAccent:          "39",
		TokSection:         "63",
		TokBorder:          "245",
		TokBorderFocus:     "39",
		TokMarker:          "39",
		TokSel:             "39",
		TokBranch:          "39",
		TokSevP0:           "196",
		TokSevP1:           "203",
		TokSevP2:           "214",
		TokSevP3:           "250",
		TokDecisionPending: "185",
		TokTabActiveBg:     "39",
		TokTabActiveFg:     "235",
	}}
}

// Dracula is the vi-SQL-style Dracula palette (hex values; lipgloss v2
// degrades them to the terminal's color profile — 256/16/NO_COLOR).
func Dracula() *Theme {
	return &Theme{Name: "dracula", colors: map[Token]string{
		TokFocus:           "#bd93f9", // purple
		TokOK:              "#50fa7b", // green
		TokWarn:            "#ffb86c", // orange
		TokErr:             "#ff5555", // red
		TokPub:             "#ff79c6", // pink
		TokHeld:            "#6272a4", // comment gray
		TokAccent:          "#8be9fd", // cyan
		TokSection:         "#bd93f9", // purple
		TokBorder:          "#6272a4", // comment gray (visible on bg)
		TokBorderFocus:     "#bd93f9", // purple
		TokMarker:          "#ff79c6", // pink
		TokSel:             "#8be9fd", // cyan
		TokBranch:          "#8be9fd", // cyan
		TokSevP0:           "#ff5555", // red
		TokSevP1:           "#ffb86c", // orange
		TokSevP2:           "#f1fa8c", // yellow
		TokSevP3:           "#6272a4", // comment gray
		TokDecisionPending: "#f1fa8c", // yellow
		TokTabActiveBg:     "#bd93f9", // purple
		TokTabActiveFg:     "#282a36", // background
	}}
}

// Available lists the built-in theme names.
func Available() []string { return []string{"default", "dracula"} }

// Resolve builds a theme by name with optional per-token overrides
// (token name → color string). Unknown names or tokens are errors so config
// typos fail loudly instead of silently rendering wrong colors.
func Resolve(name string, overrides map[string]string) (*Theme, error) {
	var t *Theme
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "default":
		t = Default()
	case "dracula":
		t = Dracula()
	default:
		return nil, fmt.Errorf("unknown theme %q (available: %s)", name, strings.Join(Available(), ", "))
	}
	keys := make([]string, 0, len(overrides))
	for k := range overrides {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		tok := Token(k)
		if _, ok := t.colors[tok]; !ok {
			return nil, fmt.Errorf("unknown theme token %q (available: %s)", k, strings.Join(AllTokens(), ", "))
		}
		if overrides[k] == "" {
			return nil, fmt.Errorf("theme token %q has an empty color", k)
		}
		t = t.With(tok, overrides[k])
	}
	return t, nil
}
