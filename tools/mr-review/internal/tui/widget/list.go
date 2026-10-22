package widget

// List is a cursor + viewport over N rows: the scroll math every navigator
// pane needs (vi-SQL grid semantics). Pure state, no rendering — the TUI
// model embeds it per pane and feeds Len on each rebuild.
type List struct {
	Cursor int
	Top    int
	Len    int
}

// Clamp keeps Top inside [0, max(0, Len-h)].
func (l *List) Clamp(h int) {
	if h < 1 {
		h = 1
	}
	maxTop := l.Len - h
	if maxTop < 0 {
		maxTop = 0
	}
	if l.Top > maxTop {
		l.Top = maxTop
	}
	if l.Top < 0 {
		l.Top = 0
	}
}

// EnsureVisible shifts Top so the cursor is inside the [Top, Top+h) window.
func (l *List) EnsureVisible(h int) {
	if h < 1 {
		h = 1
	}
	if l.Cursor < l.Top {
		l.Top = l.Cursor
	}
	if l.Cursor >= l.Top+h {
		l.Top = l.Cursor - h + 1
	}
	l.Clamp(h)
}

// Move shifts the cursor by delta, clamping to [0, Len-1]. Reports whether
// the cursor changed.
func (l *List) Move(delta int) bool {
	next := l.Cursor + delta
	if next < 0 {
		next = 0
	}
	if next > l.Len-1 {
		next = l.Len - 1
	}
	if next < 0 {
		next = 0 // empty list
	}
	if next == l.Cursor {
		return false
	}
	l.Cursor = next
	return true
}

// First / Last jump to the ends.
func (l *List) First() { l.Cursor = 0 }

func (l *List) Last() {
	if l.Len > 0 {
		l.Cursor = l.Len - 1
	}
}

// Window returns the visible row range [start, end) for a viewport of h rows.
func (l *List) Window(h int) (start, end int) {
	l.EnsureVisible(h)
	l.Clamp(h)
	end = l.Top + h
	if end > l.Len {
		end = l.Len
	}
	return l.Top, end
}
