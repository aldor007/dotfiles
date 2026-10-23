// filetree.go — collapsible changed-file tree (lazygit-style data model).
// Shows the diff's file hierarchy with add/modify/delete status, AI finding
// and comment counts, per-file ±line counts and the reviewed mark. Pure
// local state — nothing here is ever published.
//
// Stability contract: the node structure is rebuilt ONLY when the parsed
// diff changes (diffVersion). Expansion state is keyed by dir path in
// expandedDirs, sort/flat/filter kind live on the tree struct — every
// rebuildTreeRows() preserves them, so filters and review toggles never
// lose the user's view.
package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Files-pane sort modes (cycled by filesCycleSort). Dirs always sort first
// (by path); files sort by the active mode with the path as tiebreaker.
const (
	filesSortDiff     = iota // original diff.patch order
	filesSortPath            // alphabetical
	filesSortReview          // unreviewed first
	filesSortFindings        // most findings first
	filesSortCount
)

var filesSortNames = [...]string{"diff order", "path", "review status", "findings"}

// Files-pane path filters (cycled/set by filesSetFilter·filesCycleFilter).
const (
	filesFilterAll = iota
	filesFilterUnreviewed
	filesFilterReviewed
	filesFilterFindings
	filesFilterComments
	filesFilterModified
	filesFilterAdded
	filesFilterDeleted
	filesFilterCount
)

var filesFilterNames = [...]string{
	"all", "unreviewed", "reviewed", "findings", "comments",
	"modified", "added", "deleted",
}

// fileMeta is the per-file state snapshot the tree sorts and renders from.
// Refreshed on every rebuildTreeRows (cheap: O(files + findings)).
type fileMeta struct {
	status   byte
	reviewed bool
	findings int
	comments int
	added    int
	deleted  int
}

// treeNode is one directory or file in the changed-file tree.
type treeNode struct {
	name     string
	path     string // full repo-relative path (files AND dirs)
	dir      bool
	expanded bool
	status   byte // 'A', 'M', 'D', or ' ' (files only)
	order    int  // position in the original diff (filesSortDiff)
	children []*treeNode
}

// treeRow is one visible row of the flattened tree.
type treeRow struct {
	node  *treeNode
	depth int
}

// fileTree is the FILES-pane tree model.
type fileTree struct {
	roots []*treeNode
	rows  []treeRow

	filter     string // substring filter on full paths ("" = all)
	filterKind int    // filesFilter* predicate on file state
	flat       bool   // flat file list instead of the hierarchy
	sortMode   int    // filesSort*

	// expandedDirs keys dir paths so expansion survives node rebuilds; it
	// is re-synced from the live nodes after every row rebuild (foreign
	// toggles like openSelection/mouse write node.expanded directly).
	expandedDirs map[string]bool
	builtVer     int                 // diffVersion the roots were built from
	meta         map[string]fileMeta // per-file snapshot for sort/render

	// previewPath is the file the cursor last previewed in the center diff
	// (filesPreviewSync): a preview only fires when the cursor MOVES onto a
	// different file, so re-focusing the pane never yanks the diff back.
	previewPath string
}

// buildFileTree constructs the node hierarchy (not the visible rows —
// rebuildRows does that once filter/meta are attached).
func buildFileTree(paths []string, order map[string]int, status func(string) byte, expanded map[string]bool) *fileTree {
	if expanded == nil {
		expanded = map[string]bool{}
	}
	ft := &fileTree{expandedDirs: expanded, meta: map[string]fileMeta{}}
	for i, p := range paths {
		ord, ok := order[p]
		if !ok {
			ord = len(paths) + i // unknown diff position: stable, after known
		}
		ft.insert(p, status(p), ord)
	}
	return ft
}

func (ft *fileTree) insert(path string, status byte, order int) {
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
				found = &treeNode{name: s, path: path, status: status, order: order}
			} else {
				dirPath := strings.Join(segs[:i+1], "/")
				exp := true // dirs start expanded unless remembered collapsed
				if v, ok := ft.expandedDirs[dirPath]; ok {
					exp = v
				}
				found = &treeNode{name: s, path: dirPath, dir: true, expanded: exp}
			}
			*kids = append(*kids, found)
		}
		kids = &found.children
	}
}

// applySort orders the hierarchy by the active sort mode. Dirs always come
// first (path-sorted, so groups are stable); files compare by mode with the
// path as tiebreaker. SliceStable keeps equal keys in diff order.
func (ft *fileTree) applySort() {
	mode := ft.sortMode
	var sortKids func(nodes []*treeNode)
	sortKids = func(nodes []*treeNode) {
		sort.SliceStable(nodes, func(a, b int) bool {
			x, y := nodes[a], nodes[b]
			if x.dir != y.dir {
				return x.dir
			}
			if x.dir {
				return x.path < y.path
			}
			return fileLess(ft, x, y, mode)
		})
		for _, n := range nodes {
			sortKids(n.children)
		}
	}
	sortKids(ft.roots)
}

func fileLess(ft *fileTree, a, b *treeNode, mode int) bool {
	ma, mb := ft.meta[a.path], ft.meta[b.path]
	switch mode {
	case filesSortReview:
		if ma.reviewed != mb.reviewed {
			return !ma.reviewed // triage order: unreviewed first
		}
	case filesSortFindings:
		if ma.findings != mb.findings {
			return ma.findings > mb.findings
		}
	case filesSortDiff:
		if a.order != b.order {
			return a.order < b.order
		}
	}
	return a.path < b.path
}

// kept reports whether a FILE row survives the active search + kind filter.
func (ft *fileTree) kept(n *treeNode) bool {
	if n.dir {
		return false // dirs are kept iff a descendant matches (matches())
	}
	mt := ft.meta[n.path]
	switch ft.filterKind {
	case filesFilterUnreviewed:
		if mt.reviewed {
			return false
		}
	case filesFilterReviewed:
		if !mt.reviewed {
			return false
		}
	case filesFilterFindings:
		if mt.findings == 0 {
			return false
		}
	case filesFilterComments:
		if mt.comments == 0 {
			return false
		}
	case filesFilterModified:
		if mt.status != 'M' {
			return false
		}
	case filesFilterAdded:
		if mt.status != 'A' {
			return false
		}
	case filesFilterDeleted:
		if mt.status != 'D' {
			return false
		}
	}
	if ft.filter != "" &&
		!strings.Contains(strings.ToLower(n.path), strings.ToLower(ft.filter)) {
		return false
	}
	return true
}

// matches reports whether the node or any descendant file is kept.
func (ft *fileTree) matches(n *treeNode) bool {
	if !n.dir {
		return ft.kept(n)
	}
	for _, c := range n.children {
		if ft.matches(c) {
			return true
		}
	}
	return false
}

// rebuildRows flattens the visible tree. Tree mode: dirs emit children when
// expanded or when a filter is active (ancestors of matching files stay
// visible). Flat mode: every kept file at depth 0. Ends by syncing the
// expansion map from the live nodes.
func (ft *fileTree) rebuildRows() {
	filterActive := ft.filter != "" || ft.filterKind != filesFilterAll
	ft.rows = ft.rows[:0]
	if ft.flat {
		var walkFiles func(nodes []*treeNode)
		walkFiles = func(nodes []*treeNode) {
			for _, n := range nodes {
				if n.dir {
					walkFiles(n.children)
					continue
				}
				if ft.kept(n) {
					ft.rows = append(ft.rows, treeRow{node: n, depth: 0})
				}
			}
		}
		walkFiles(ft.roots)
		ft.syncExpandedDirs()
		return
	}
	var walk func(nodes []*treeNode, depth int)
	walk = func(nodes []*treeNode, depth int) {
		for _, n := range nodes {
			if !ft.matches(n) {
				continue
			}
			if n.dir {
				if filterActive {
					// Under a filter the hierarchy flattens away: kept files
					// list directly, and a matching dir only surfaces as a
					// collapsed group header for the matches inside it.
					if !n.expanded {
						ft.rows = append(ft.rows, treeRow{node: n, depth: depth})
						continue
					}
					walk(n.children, depth)
					continue
				}
				ft.rows = append(ft.rows, treeRow{node: n, depth: depth})
				if n.expanded {
					walk(n.children, depth+1)
				}
				continue
			}
			ft.rows = append(ft.rows, treeRow{node: n, depth: depth})
		}
	}
	walk(ft.roots, 0)
	ft.syncExpandedDirs()
}

// syncExpandedDirs records every dir's current expansion so a later node
// rebuild (new diffVersion) restores it exactly.
func (ft *fileTree) syncExpandedDirs() {
	var walk func(nodes []*treeNode)
	walk = func(nodes []*treeNode) {
		for _, n := range nodes {
			if n.dir {
				ft.expandedDirs[n.path] = n.expanded
				walk(n.children)
			}
		}
	}
	walk(ft.roots)
}

// ---- model integration -----------------------------------------------------

// buildTree constructs (or rebuilds) the tree from the parsed diff. The node
// structure is only rebuilt when diffVersion moved; view state (expansion,
// sort, flat, filter kind) carries over.
func (m *model) buildTree() {
	m.ensureDiff()
	if m.tree != nil && m.tree.builtVer == m.diffVersion {
		m.rebuildTreeRows()
		return
	}
	var prev *fileTree
	if m.tree != nil {
		m.tree.syncExpandedDirs() // capture foreign node.expanded toggles
		prev = m.tree
	}
	files := m.changedFiles()
	order := diffFileOrder(m.dir)
	m.tree = buildFileTree(files, order, func(p string) byte { return m.fileStatus(p) }, prevExpandedDirs(prev))
	m.tree.builtVer = m.diffVersion
	if prev != nil {
		m.tree.flat = prev.flat
		m.tree.sortMode = prev.sortMode
		m.tree.filterKind = prev.filterKind
	}
	m.rebuildTreeRows()
}

func prevExpandedDirs(prev *fileTree) map[string]bool {
	if prev == nil {
		return nil
	}
	return prev.expandedDirs
}

// diffFileOrder extracts the file order of the original diff.patch (stable
// order for filesSortDiff). Deleted files appear as "+++ /dev/null" — the
// preceding "--- a/<path>" line names them. Unreadable patch → empty map
// (files fall back to a stable alphabetical position).
func diffFileOrder(dir string) map[string]int {
	out := map[string]int{}
	b, err := os.ReadFile(filepath.Join(dir, "diff.patch"))
	if err != nil {
		return out
	}
	n := 0
	lastOld := ""
	for _, line := range strings.Split(string(b), "\n") {
		switch {
		case strings.HasPrefix(line, "--- a/"):
			lastOld = strings.TrimPrefix(line, "--- a/")
		case strings.HasPrefix(line, "+++ b/"):
			p := strings.TrimPrefix(line, "+++ b/")
			if p == "/dev/null" {
				p = lastOld
			}
			if p != "" && p != "/dev/null" {
				if _, seen := out[p]; !seen {
					out[p] = n
					n++
				}
			}
		}
	}
	return out
}

// refreshTreeMeta snapshots per-file counts (findings, comments, ±lines,
// review mark, status) for sorting and rendering.
func (m *model) refreshTreeMeta() {
	if m.tree == nil {
		return
	}
	m.ensureDiff()
	fc := m.findingsPerFile()
	cc := m.commentsPerFile()
	rev := m.reviewedSet()
	meta := make(map[string]fileMeta, len(m.diffParsed))
	for p, fd := range m.diffParsed {
		mt := fileMeta{
			status:   m.fileStatus(p),
			reviewed: rev[p],
			findings: fc[p],
			comments: cc[p],
		}
		for _, l := range fd.Lines {
			switch l.Kind {
			case DiffAdd:
				mt.added++
			case DiffDel:
				mt.deleted++
			}
		}
		meta[p] = mt
	}
	m.tree.meta = meta
}

// rebuildTreeRows re-flattens the visible rows after any filter/sort/search
// change, preserving the selected node (by identity — nodes are stable
// across row rebuilds) and clamping the cursor into range.
func (m *model) rebuildTreeRows() {
	if m.tree == nil {
		m.buildTree()
		return
	}
	var sel *treeNode
	if r := m.treeRowAt(m.treeCursor); r != nil {
		sel = r.node
	}
	m.tree.filter = m.searchText
	m.refreshTreeMeta()
	m.tree.applySort()
	m.tree.rebuildRows()
	if sel != nil {
		for i := range m.tree.rows {
			if m.tree.rows[i].node == sel {
				m.treeCursor = i
				break
			}
		}
	}
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
