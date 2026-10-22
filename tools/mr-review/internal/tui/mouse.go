// mouse.go — cockpit mouse support: click to position, drag to select,
// release to copy (OSC52 via tea.SetClipboard), wheel to scroll.
//
// Coordinate mapping (wide two-pane layout, alt-screen):
//
//	row 0-1  header            row 2  box top border
//	row 3..  body content      row 3+inner  box bottom border
//	col 0    left border       col 1..listWidth()  left pane
//	col listWidth()+1  divider        col listWidth()+2..  right pane
//
// The tuicr pane forwards its own events (wsHandleMouse) before this runs;
// narrow mode is keyboard-first and stays mouse-free for now.
package tui

import (
	"fmt"
	"strings"
	"time"

	"mr-review/internal/tui/widget"

	tea "charm.land/bubbletea/v2"
)

// mousePress tracks an in-progress left-button drag in the diff pane.
type mousePress struct {
	dragging bool
	row      int // document row where the press landed
	at       time.Time
}

// handleCockpitMouse routes mouse events the tuicr pane did not consume.
func (m *model) handleCockpitMouse(msg tea.MouseMsg) tea.Cmd {
	if m.narrow || m.curMode() != modeNormal {
		return nil // narrow stays keyboard-first; no mouse in text modes
	}
	mo := msg.Mouse()

	switch msg.(type) {
	case tea.MouseClickMsg:
		if mo.Button != tea.MouseLeft {
			return nil
		}
		return m.mouseClick(mo)
	case tea.MouseMotionMsg:
		if mo.Button == tea.MouseLeft && m.mouse.dragging {
			return m.mouseDrag(mo)
		}
		return nil
	case tea.MouseReleaseMsg:
		if mo.Button == tea.MouseLeft && m.mouse.dragging {
			return m.mouseRelease()
		}
		return nil
	case tea.MouseWheelMsg:
		return m.mouseWheel(mo)
	}
	return nil
}

// hitTest classifies a mouse position: which pane and which content row.
// Returns ok=false for borders, headers, tab bars and the footer.
func (m *model) hitTest(x, y int) (pane string, row, col int, ok bool) {
	inner := m.bodyHeight() - 2
	if y < 3 || y >= 3+inner {
		return "", 0, 0, false // header / borders / footer
	}
	lw := m.listWidth()
	switch {
	case x >= 1 && x <= lw:
		item := y - 5 // pane header (row 3) + rule (row 4)
		if item < 0 {
			return "", 0, 0, false
		}
		return "list", item, x, true
	case x > lw+1 && x <= m.width-2:
		innerIdx := y - 3
		if innerIdx < 4 { // pane header + tab bar (3 rows)
			return "", 0, 0, false
		}
		return "detail", innerIdx - 4, x, true
	}
	return "", 0, 0, false // borders / divider
}

// mouseClick positions the cursor in the clicked pane. In the diff pane a
// click also clears any selection (a following drag starts a new one).
func (m *model) mouseClick(mo tea.Mouse) tea.Cmd {
	pane, row, _, ok := m.hitTest(mo.X, mo.Y)
	if !ok {
		return nil
	}
	switch pane {
	case "list":
		// same window math the views use: items start two rows into the pane
		vis := m.bodyHeight() - 4
		m.focus = focusList
		switch m.leftView {
		case viewFiles:
			rows := m.treeRows()
			top, _ := (&widget.List{Cursor: m.treeCursor, Len: len(rows)}).Window(vis)
			idx := top + row
			if idx < 0 || idx >= len(rows) {
				return nil
			}
			m.treeCursor = idx
			if r := m.treeRowAt(idx); r != nil && r.node.dir {
				r.node.expanded = !r.node.expanded // click on a dir: expand/collapse
				m.rebuildTreeRows()
			}
			return nil
		case viewComments:
			rows := m.filteredComments()
			top, _ := (&widget.List{Cursor: m.commentCur, Len: len(rows)}).Window(vis)
			idx := top + row
			if idx >= 0 && idx < len(rows) {
				m.commentCur = idx
			}
			return nil
		}
		return nil
	case "detail":
		if m.tab != tabDiff || m.browseFile == "" {
			return nil // v1: mouse copy lives in the continuous diff
		}
		m.focus = focusDetail
		doc := m.browseDoc()
		top := m.detailTop[tabDiff]
		idx := top + row
		if idx < 0 || idx >= len(doc) {
			return nil
		}
		m.visualOn, m.visualStart = false, -1
		m.diffCursor = idx
		m.syncBrowseFile()
		m.ensureBrowseVisible()
		// arm a potential drag from this row
		m.mouse = mousePress{dragging: true, row: idx, at: time.Now()}
	}
	return nil
}

// mouseDrag extends the selection while the left button is held in the diff.
func (m *model) mouseDrag(mo tea.Mouse) tea.Cmd {
	pane, row, _, ok := m.hitTest(mo.X, mo.Y)
	if !ok || pane != "detail" || m.tab != tabDiff {
		return nil
	}
	doc := m.browseDoc()
	idx := m.detailTop[tabDiff] + row
	if idx < 0 || idx >= len(doc) || doc[idx].header {
		return nil
	}
	if !m.visualOn {
		m.visualOn = true
		m.visualStart = m.mouse.row
	}
	m.diffCursor = idx
	m.syncBrowseFile()
	m.ensureBrowseVisible()
	return nil
}

// mouseRelease copies the dragged selection (mouse analogue of y).
func (m *model) mouseRelease() tea.Cmd {
	defer func() { m.mouse = mousePress{} }()
	if !m.visualOn {
		return nil
	}
	lo, hi := m.visualRange()
	doc := m.browseDoc()
	var b strings.Builder
	n := 0
	for i := lo; i <= hi && i < len(doc); i++ {
		if doc[i].header {
			continue
		}
		if n > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(doc[i].dl.Text)
		n++
	}
	if n == 0 {
		return nil
	}
	m.msg = fmt.Sprintf("copied %d line(s) via mouse", n)
	return tea.SetClipboard(b.String())
}

// mouseWheel scrolls the pane under the wheel.
func (m *model) mouseWheel(mo tea.Mouse) tea.Cmd {
	pane, _, _, ok := m.hitTest(mo.X, mo.Y)
	if !ok {
		return nil
	}
	step := 0
	switch mo.Button {
	case tea.MouseWheelUp:
		step = -3
	case tea.MouseWheelDown:
		step = 3
	default:
		return nil
	}
	if pane == "detail" && m.browseFile != "" {
		for i := 0; i < abs(step); i++ {
			m.browseMove(step)
		}
		return nil
	}
	for i := 0; i < abs(step); i++ {
		if step < 0 {
			m.cursorUp()
		} else {
			m.cursorDown()
		}
	}
	return nil
}
