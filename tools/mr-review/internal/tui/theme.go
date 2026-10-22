// theme.go — centralized semantic theme (vi-SQL-style): every visual style
// in the package derives from the tokens of internal/tui/widget/theme.go, so
// focus/selection/state colors stay consistent across panels and the theme
// can be swapped at runtime (SetTheme). Under NO_COLOR everything degrades
// to plain output via plainMode().
package tui

import (
	"image/color"
	"os"

	"mr-review/internal/tui/widget"

	"charm.land/lipgloss/v2"
)

// currentTheme is the active theme; the default matches the pre-widget
// hardcoded palette byte-for-byte (see widget.Default).
var currentTheme *widget.Theme = widget.Default()

// Semantic token colors (reassigned by applyTheme).
var (
	themeFocus  color.Color // focused border / active accent
	themeOK     color.Color // approved / ready / added
	themeWarn   color.Color // pending / modified / warnings
	themeErr    color.Color // errors / deleted
	themePub    color.Color // published
	themeHeld   color.Color // rejected / held / dim
	themeAccent color.Color // selection text
)

// Styled building blocks (reassigned wholesale by applyTheme).
var (
	styleHeader  lipgloss.Style
	styleBranch  lipgloss.Style
	styleDim     lipgloss.Style
	styleSection lipgloss.Style
	styleWarn    lipgloss.Style
	styleSel     lipgloss.Style
	styleCursor  lipgloss.Style
	styleMarker  lipgloss.Style
	styleErr     lipgloss.Style
	styleOK      lipgloss.Style
	styleBorder  lipgloss.Style
	styleBorderF lipgloss.Style

	styleTabActive lipgloss.Style // active segment in the tab bar

	styleSeverity = map[string]lipgloss.Style{}
	styleDecision = map[string]lipgloss.Style{}

	stylePublished lipgloss.Style
)

func init() {
	applyTheme(currentTheme)
	applyIcons()
}

func fg(t *widget.Theme, tok widget.Token) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.Color(tok))
}

// applyTheme resolves every package style from the theme tokens.
func applyTheme(t *widget.Theme) {
	currentTheme = t
	themeFocus = t.Color(widget.TokFocus)
	themeOK = t.Color(widget.TokOK)
	themeWarn = t.Color(widget.TokWarn)
	themeErr = t.Color(widget.TokErr)
	themePub = t.Color(widget.TokPub)
	themeHeld = t.Color(widget.TokHeld)
	themeAccent = t.Color(widget.TokAccent)

	styleHeader = lipgloss.NewStyle().Bold(true)
	styleBranch = fg(t, widget.TokBranch)
	styleDim = lipgloss.NewStyle().Faint(true)
	styleSection = lipgloss.NewStyle().Bold(true).Foreground(t.Color(widget.TokSection))
	styleWarn = fg(t, widget.TokWarn)
	styleSel = fg(t, widget.TokSel)
	styleCursor = lipgloss.NewStyle().Reverse(true)
	styleMarker = lipgloss.NewStyle().Bold(true).Foreground(t.Color(widget.TokMarker))
	styleErr = fg(t, widget.TokErr)
	styleOK = fg(t, widget.TokOK)
	styleBorder = fg(t, widget.TokBorder)
	styleBorderF = fg(t, widget.TokBorderFocus)
	styleTabActive = lipgloss.NewStyle().Bold(true).
		Background(t.Color(widget.TokTabActiveBg)).
		Foreground(t.Color(widget.TokTabActiveFg))

	styleSeverity = map[string]lipgloss.Style{
		"P0": lipgloss.NewStyle().Bold(true).Foreground(t.Color(widget.TokSevP0)),
		"P1": lipgloss.NewStyle().Bold(true).Foreground(t.Color(widget.TokSevP1)),
		"P2": fg(t, widget.TokSevP2),
		"P3": fg(t, widget.TokSevP3), // neutral: minor
	}
	styleDecision = map[string]lipgloss.Style{
		"pending":  fg(t, widget.TokDecisionPending),
		"approved": fg(t, widget.TokOK),
		"rejected": fg(t, widget.TokHeld),
	}
	stylePublished = fg(t, widget.TokPub)
}

// SetTheme switches the active theme ("default", "dracula") with optional
// per-token color overrides (token name → "39" / "#hex"). Unknown names,
// tokens or empty colors error without changing the current theme.
func SetTheme(name string, overrides map[string]string) error {
	t, err := widget.Resolve(name, overrides)
	if err != nil {
		return err
	}
	applyTheme(t)
	mdReset() // markdown glamour style follows the theme
	return nil
}

// ThemeName reports the active theme's name.
func ThemeName() string { return currentTheme.Name }

// CycleTheme switches between the built-in themes; returns the new name.
func CycleTheme() error {
	next := "dracula"
	if currentTheme.Name == "dracula" {
		next = "default"
	}
	return SetTheme(next, nil)
}

// ---- icons -----------------------------------------------------------------

// iconMode is the configured icons mode ("auto", "nerd", "ascii"); icons is
// the resolved set. Resolution is plain-mode-aware: NO_COLOR always renders
// the byte-compatible ASCII markers.
var (
	iconMode = "auto"
	icons    widget.IconSet
)

func applyIcons() {
	set, err := widget.ResolveIcons(iconMode, plainMode())
	if err != nil { // config validated at SetIcons; never breaks rendering
		set, _ = widget.ResolveIcons("auto", plainMode())
	}
	icons = set
}

// SetIcons switches the icon set ("auto", "nerd", "ascii").
func SetIcons(mode string) error {
	if _, err := widget.ResolveIcons(mode, plainMode()); err != nil {
		return err
	}
	iconMode = mode
	applyIcons()
	return nil
}

// IconsMode reports the active icon mode.
func IconsMode() string { return iconMode }

func lipglossGreen() lipgloss.Style { return lipgloss.NewStyle().Foreground(themeOK) }
func lipglossRed() lipgloss.Style   { return lipgloss.NewStyle().Foreground(themeErr) }
func lipglossYellow() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(themeWarn)
}

// decisionStyleOf styles comment-navigator states.
func decisionStyleOf(ready, published bool) lipgloss.Style {
	switch {
	case published:
		return stylePublished
	case ready:
		return styleOK
	default:
		return styleDim
	}
}

// plainMode reports whether NO_COLOR is set (plain output for every custom
// style; lipgloss also honors it, this keeps hand-built ANSI constants out
// of plain output).
func plainMode() bool { return os.Getenv("NO_COLOR") != "" }

// plainOr returns the plain marker under NO_COLOR, else the styled one.
func plainOr(plain string, styled string) string {
	if plainMode() {
		return plain
	}
	return styled
}

// focusIndicator renders the focus dot for a pane header (icon-set aware;
// the resolved sets keep NO_COLOR byte-compatible with the legacy output).
func focusIndicator(focused bool) string {
	if focused {
		return icons.FocusOn
	}
	return icons.FocusOff
}
