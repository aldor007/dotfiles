package widget

import "charm.land/lipgloss/v2"

// Hint is one footer key hint ("keys" + short description, e.g.
// "a" + "approve").
type Hint struct{ Keys, Desc string }

// RenderHints joins the hints with separators and cuts to the width. The
// caller styles the result (the legacy footer renders the whole keys bar
// dim, prefixed with "┃ "). Byte-compatible with the legacy footer.
func RenderHints(hints []Hint, w int) string {
	out := ""
	for i, h := range hints {
		if i > 0 {
			out += " · "
		}
		out += h.Keys
		if h.Desc != "" {
			out += " " + h.Desc
		}
	}
	if w > 0 && lipgloss.Width(out) > w {
		out = lipgloss.NewStyle().MaxWidth(w).Render(out)
	}
	return out
}

// PrefixHints derives the footer hints for a pending multi-key prefix:
// every enabled action key starting with the prefix. Order follows the
// sorted key order (stable, predictable).
func PrefixHints(prefix string, keys []string) []Hint {
	var out []Hint
	seen := map[string]bool{}
	for _, k := range keys {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix && !seen[k] {
			seen[k] = true
			out = append(out, Hint{Keys: k})
		}
	}
	return out
}
