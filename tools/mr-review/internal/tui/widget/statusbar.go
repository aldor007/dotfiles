package widget

import "charm.land/lipgloss/v2"

// StatusSegment is one styled span of the status line. A nil Style renders
// as plain text.
type StatusSegment struct {
	Text  string
	Style *lipgloss.Style
}

// RenderStatusLine composes the status line: prefix + segments, cut to the
// width. The truncation function is injected (the TUI has the legacy
// cell-aware truncPlain) so output stays byte-identical.
func RenderStatusLine(prefix string, segments []StatusSegment, w int, trunc func(string, int) string) string {
	line := prefix
	for _, s := range segments {
		if s.Style == nil {
			line += s.Text
		} else {
			line += s.Style.Render(s.Text)
		}
	}
	if trunc != nil {
		line = trunc(line, w)
	}
	return line
}

// SplitProgress renders "n/total" style progress ("3/12 reviewed").
func SplitProgress(done, total int) string {
	return itoa(done) + "/" + itoa(total)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
