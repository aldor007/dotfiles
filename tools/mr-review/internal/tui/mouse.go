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
// Returns ok=false for borders, gaps, headers and the footer.
// Pane geometry (wide): each pane is a bordered box of inner width pw
// (2 border cols) with a 1-col gap between panes:
//
//	files box [0, fw+2)  gap  diff box [fw+3, fw+3+dw+2)  gap  findings [fw+dw+6, …)
//	row 0-1 header · row 2 box top border · row 3.. content · row 3+inner bottom border
func (m *model) hitTest(x, y int) (pane string, row, col int, ok bool) {
	inner := m.bodyHeight() - 2
	if y < 3 || y >= 3+inner {
		return "", 0, 0, false // header / borders / footer
	}
	if m.zoom {
		return "", 0, 0, false // zoomed body is one full-width pane: the 3-pane
		// geometry below does not apply (a click would corrupt pane state)
	}
	L := m.layout()
	if L.mode == layoutNarrow {
		return "", 0, 0, false // narrow stays keyboard-first
	}
	filesEnd := L.filesW + 2 // right edge (exclusive) of the files box
	diffStart := filesEnd + 1
	diffEnd := diffStart + L.diffW + 2
	findingsStart := diffEnd + 1
	switch {
	case x >= 1 && x <= L.filesW: // inside files box (border cols excluded)
		item := y - 5 // pane header (row 3) + CHANGED FILES header + rule
		if item < 0 {
			return "", 0, 0, false
		}
		return "files", item, x - 1, true
	case x >= diffStart+1 && x <= diffEnd-2: // inside diff box
		docRow := y - 5 // pane header (row 3) + browse title (row 4)
		if docRow < 0 {
			return "", 0, 0, false
		}
		return "diff", docRow, x - diffStart - 1, true
	case L.mode == layoutWide && x >= findingsStart+1 && x <= findingsStart+L.findingsW:
		listRow := y - 4 // pane header (row 3); findings cards follow
		if listRow < 0 {
			return "", 0, 0, false
		}
		return "findings", listRow, x - findingsStart - 1, true
	}
	return "", 0, 0, false // borders / gaps
}

// mouseClick positions the cursor in the clicked pane and focuses it. In the
// diff pane a click also clears any selection (a following drag starts a new one).
func (m *model) mouseClick(mo tea.Mouse) tea.Cmd {
	pane, row, _, ok := m.hitTest(mo.X, mo.Y)
	if !ok {
		return nil
	}
	switch pane {
	case "files":
		m.focus = focusFiles
		m.leftView = viewFiles
		vis := m.bodyHeight() - 4
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
	case "findings":
		m.focus = focusFindings
		if m.leftView == viewComments {
			rows := m.filteredComments()
			top, _ := (&widget.List{Cursor: m.commentCur, Len: len(rows)}).Window(m.bodyHeight() - 4)
			idx := top + row
			if idx >= 0 && idx < len(rows) {
				m.commentCur = idx
			}
			return nil
		}
		// findings cards are 2 lines each; visible cards start at listTop
		card := row/2 + m.listTop
		if card >= 0 && card < len(m.filtered) && card != m.cursor {
			m.cursor = card
			m.syncDiffScroll()
		}
		return nil
	case "diff":
		if m.browseFile == "" {
			return nil // mouse copy lives in the continuous diff
		}
		m.focus = focusDiff
		doc := m.browseDoc()
		top := m.detailTop[tabOverview]
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
	if !ok || pane != "diff" {
		return nil
	}
	doc := m.browseDoc()
	idx := m.detailTop[tabOverview] + row
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

// mouseWheel scrolls the pane under the wheel (wheel follows the pointer:
// it scrolls that pane without requiring a prior click to move focus).
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
	switch pane {
	case "diff":
		if m.browseFile != "" {
			for i := 0; i < abs(step); i++ {
				m.browseMove(step)
			}
			return nil
		}
		// browse exited (esc): still scroll the pane under the pointer, not
		// whatever pane happens to hold focus
		m.focus = focusDiff
		m.scrollBy(step * 3)
		return nil
	case "files":
		m.focus = focusFiles
		m.leftView = viewFiles
	case "findings":
		m.focus = focusFindings
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
