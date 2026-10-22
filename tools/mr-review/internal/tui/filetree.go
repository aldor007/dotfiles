// filetree.go — collapsible changed-file tree (tuicr-inspired). Shows the
// diff's file hierarchy with add/modify/delete status, AI finding and comment
// counts, and the per-file reviewed mark. Pure local state — nothing here is
// ever published.
package tui

import (
	"fmt"
	"sort"
	"strings"

	"mr-review/internal/tui/widget"
)

// treeNode is one directory or file in the changed-file tree.
type treeNode struct {
	name     string
	path     string // full repo-relative path (files only)
	dir      bool
	expanded bool
	status   byte // 'A', 'M', 'D', or ' ' (files only)
	children []*treeNode
}

// treeRow is one visible row of the flattened tree.
type treeRow struct {
	node  *treeNode
	depth int
}

// fileTree is the left-pane tree model.
type fileTree struct {
	roots  []*treeNode
	rows   []treeRow
	filter string // substring filter on full paths ("" = all)
}

// buildFileTree constructs the tree from the parsed diff.
func buildFileTree(paths []string, status func(string) byte) *fileTree {
	ft := &fileTree{}
	for _, p := range paths {
		ft.insert(p, status(p))
	}
	ft.sortAll()
	ft.rebuildRows()
	return ft
}
func (ft *fileTree) insert(path string, status byte) {
	segs := strings.Split(path, "/")
	kids := &ft.roots
	for i, s := range segs {
		file := i == len(segs)-1
		var found *treeNode
		for _, c := range *kids {
			if c.name == s && c.dir != file {
				// a file and a dir never share a name in one parent; match
				// kind too so "x.go" the file and "x.go/" the dir coexist
				found = c
				break
			}
		}
		if found == nil {
			if file {
				found = &treeNode{name: s, path: path, status: status}
			} else {
				found = &treeNode{name: s, dir: true, expanded: true}
			}
			*kids = append(*kids, found)
		}
		kids = &found.children
	}
}

func (ft *fileTree) sortAll() {
	var sortKids func(nodes []*treeNode)
	sortKids = func(nodes []*treeNode) {
		sort.Slice(nodes, func(a, b int) bool {
			if nodes[a].dir != nodes[b].dir {
				return nodes[a].dir // dirs first
			}
			return nodes[a].name < nodes[b].name
		})
		for _, n := range nodes {
			sortKids(n.children)
		}
	}
	sortKids(ft.roots)
}

// rebuildRows flattens the visible tree via widget.FlattenTree (same filter
// semantics: matching files + their ancestor dirs, expanded-or-filtered
// walking). The widget nodes are converted back so the rest of the model
// keeps its *treeNode pointers.
func (ft *fileTree) rebuildRows() {
	orig := map[*widget.TreeNode]*treeNode{}
	var conv func(nodes []*treeNode) []*widget.TreeNode
	conv = func(nodes []*treeNode) []*widget.TreeNode {
		out := make([]*widget.TreeNode, 0, len(nodes))
		for _, n := range nodes {
			w := &widget.TreeNode{
				Name: n.name, Path: n.path, Dir: n.dir, Expanded: n.expanded,
				Status: string(n.status), Reviewed: false,
			}
			orig[w] = n
			w.Children = conv(n.children)
			out = append(out, w)
		}
		return out
	}
	flat := widget.FlattenTree(conv(ft.roots), ft.filter)
	ft.rows = ft.rows[:0]
	for _, r := range flat {
		ft.rows = append(ft.rows, treeRow{node: orig[r.Node], depth: r.Depth})
	}
}

// ---- model integration -----------------------------------------------------

// buildTree constructs (or rebuilds) the tree from the parsed diff.
func (m *model) buildTree() {
	files := m.changedFiles()
	fc := m.findingsPerFile()
	cc := m.commentsPerFile()
	m.tree = buildFileTree(files, func(p string) byte { return m.fileStatus(p) })
	_ = fc
	_ = cc
	m.rebuildTreeRows()
}

func (m *model) rebuildTreeRows() {
	if m.tree == nil {
		m.buildTree()
		return
	}
	m.tree.filter = m.searchText
	m.tree.rebuildRows()
	if m.treeCursor >= len(m.tree.rows) {
		m.treeCursor = len(m.tree.rows) - 1
	}
	if m.treeCursor < 0 {
		m.treeCursor = 0
	}
}

func (m *model) treeRows() []treeRow {
	if m.tree == nil {
		m.buildTree()
	}
	return m.tree.rows
}

func (m *model) treeRowAt(i int) *treeRow {
	rows := m.treeRows()
	if i < 0 || i >= len(rows) {
		return nil
	}
	return &rows[i]
}

// fileStatus classifies a changed file: A added, D deleted, M modified.
// go-diff gives OrigName == "/dev/null" for new files (not preserved in
// FileDiff, so additions are re-derived: pure-addition files count as added).
func (m *model) fileStatus(path string) byte {
	m.ensureDiff()
	fd := m.diffParsed[path]
	if fd == nil {
		return ' '
	}
	if fd.Old {
		return 'D'
	}
	onlyAdds := true
	for _, l := range fd.Lines {
		switch l.Kind {
		case DiffDel, DiffContext:
			onlyAdds = false
		case DiffHunkHeader:
		case DiffAdd:
		}
	}
	if onlyAdds {
		return 'A'
	}
	return 'M'
}

func (m *model) findingsPerFile() map[string]int {
	out := map[string]int{}
	for i := range m.findings {
		out[m.findings[i].File]++
	}
	return out
}

func (m *model) commentsPerFile() map[string]int {
	out := map[string]int{}
	for _, c := range m.comments {
		if !c.General {
			out[c.File]++
		}
	}
	return out
}

// ---- rendering -------------------------------------------------------------

// filesView renders the changed-file tree pane.
func (m *model) filesView() []string {
	w := m.listWidth()
	h := m.bodyHeight() - 2
	var out []string

	files := m.changedFiles()
	reviewed := len(m.reviewedSet())
	hdr := fmt.Sprintf("CHANGED FILES %d/%d reviewed", reviewed, len(files))
	if m.searchText != "" {
		hdr += fmt.Sprintf(" · filter /%s", m.searchText)
	}
	if m.focus == focusList && m.leftView == viewFiles {
		out = append(out, styleBorderF.Render("● "+hdr))
	} else {
		out = append(out, styleDim.Render("○ "+hdr))
	}
	out = append(out, styleBorder.Render(strings.Repeat("─", max(4, w-2))))

	if len(files) == 0 {
		out = append(out, centered("no changed files", w, h-2)...)
		return m.padRows(out, h)
	}

	rows := m.treeRows()
	// keep the cursor visible (widget.List scroll math, legacy semantics)
	l := widget.List{Cursor: m.treeCursor, Len: len(rows)}
	top, _ := l.Window(h - 2)
	for i := top; i < len(rows) && len(out) < h; i++ {
		r := rows[i]
		line := m.renderTreeRow(r, w)
		if i == m.treeCursor {
			line = styleCursor.Render(pad(cut(line, w), w))
		}
		out = append(out, line)
	}
	return m.padRows(out, h)
}

func (m *model) renderTreeRow(r treeRow, w int) string {
	n := r.node
	indent := strings.Repeat("  ", r.depth)
	if n.dir {
		glyph := icons.TreeClosed
		if n.expanded || m.searchText != "" {
			glyph = icons.TreeOpen
		}
		return styleSection.Render(indent + glyph + " " + n.name + "/")
	}
	status := string(n.status)
	style := styleDim
	switch n.status {
	case 'A':
		style = lipglossGreen()
	case 'D':
		style = lipglossRed()
	case 'M':
		style = lipglossYellow()
	}
	fc := m.findingsPerFile()[n.name]
	if n.path != n.name {
		fc = m.findingsPerFile()[n.path]
	}
	cc := m.commentsPerFile()[n.path]
	var meta []string
	if fc > 0 {
		meta = append(meta, fmt.Sprintf("%d finding%s", fc, plural(fc)))
	}
	if cc > 0 {
		meta = append(meta, fmt.Sprintf("%d comment%s", cc, plural(cc)))
	}
	mark := " "
	if m.reviewedSet()[n.path] {
		mark = icons.Reviewed
	}
	metaStr := ""
	if len(meta) > 0 {
		metaStr = " · " + strings.Join(meta, " · ")
	}
	return fmt.Sprintf("%s%s %s %s%s %s",
		indent, style.Render(status), plainOr(mark, styleOK.Render(mark)),
		baseName(n.name), metaStr, "")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func baseName(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}
