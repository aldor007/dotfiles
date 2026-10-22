// nav.go — context-aware navigation: the registry handlers dispatch here.
// Movement never destroys unrelated state (selection, scroll, filters); the
// diff follows the focused item, never the other way round.
package tui

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/textinput"

	tea "charm.land/bubbletea/v2"
)

// ---- vertical movement -----------------------------------------------------

func (m *model) cursorDown() {
	if m.focus == focusList {
		switch m.leftView {
		case viewFindings:
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
				m.syncDiffScroll()
			}
		case viewFiles:
			if m.treeCursor < len(m.treeRows())-1 {
				m.treeCursor++
			}
		case viewComments:
			if m.commentCur < m.filteredCommentCount()-1 {
				m.commentCur++
			}
		case viewFileFindings:
			if m.fileFindCur < m.fileFindingsCount()-1 {
				m.fileFindCur++
			}
		}
		m.trackFileFindingsTarget()
		return
	}
	// detail focus
	if m.tab == tabDiff && m.browseFile != "" {
		m.browseMove(1)
		return
	}
	m.scrollBy(1)
}

func (m *model) cursorUp() {
	if m.focus == focusList {
		switch m.leftView {
		case viewFindings:
			if m.cursor > 0 {
				m.cursor--
				m.syncDiffScroll()
			}
		case viewFiles:
			if m.treeCursor > 0 {
				m.treeCursor--
			}
		case viewComments:
			if m.commentCur > 0 {
				m.commentCur--
			}
		case viewFileFindings:
			if m.fileFindCur > 0 {
				m.fileFindCur--
			}
		}
		m.trackFileFindingsTarget()
		return
	}
	if m.tab == tabDiff && m.browseFile != "" {
		m.browseMove(-1)
		return
	}
	m.scrollBy(-1)
}

func (m *model) toFirst() {
	if m.focus == focusList {
		switch m.leftView {
		case viewFindings:
			m.cursor = 0
			m.syncDiffScroll()
		case viewFiles:
			m.treeCursor = 0
		case viewComments:
			m.commentCur = 0
		case viewFileFindings:
			m.fileFindCur = 0
		}
		return
	}
	if m.tab == tabDiff && m.browseFile != "" {
		m.diffCursor = 0
		m.ensureBrowseVisible()
		return
	}
	m.setScroll(0)
}

func (m *model) toLast() {
	if m.focus == focusList {
		switch m.leftView {
		case viewFindings:
			m.cursor = len(m.filtered) - 1
			m.syncDiffScroll()
		case viewFiles:
			m.treeCursor = len(m.treeRows()) - 1
		case viewComments:
			m.commentCur = m.filteredCommentCount() - 1
		case viewFileFindings:
			m.fileFindCur = m.fileFindingsCount() - 1
		}
		return
	}
	if m.tab == tabDiff && m.browseFile != "" {
		m.diffCursor = m.browseDocLen() - 1
		m.syncBrowseFile()
		m.ensureBrowseVisible()
		return
	}
	m.setScroll(1 << 30)
}

func (m *model) halfScroll(dir int) {
	if m.focus == focusList {
		page := m.listVisibleRows() / 2
		if page < 1 {
			page = 1
		}
		for i := 0; i < page; i++ {
			if dir > 0 {
				m.cursorDown()
			} else {
				m.cursorUp()
			}
		}
		return
	}
	if m.tab == tabDiff && m.browseFile != "" {
		m.browseMove(dir * m.browseViewportH() / 2)
		return
	}
	m.scrollBy(dir * m.bodyHeight() / 2)
}

// ---- focus -----------------------------------------------------------------

func (m *model) focusNext() {
	if m.focus == focusList {
		m.focus = focusDetail
	} else {
		m.focus = focusList
	}
	m.clampScrolls()
}

func (m *model) focusPrev() { m.focusNext() }

func (m *model) focusPane(dir int) {
	if dir < 0 {
		m.focus = focusList
	} else {
		m.focus = focusDetail
	}
	m.clampScrolls()
}

func (m *model) cycleTab(dir int) {
	if m.focus != focusDetail {
		return
	}
	m.tab = (m.tab + dir + tabCount) % tabCount
	m.clampScrolls()
}

// ---- left pane views -------------------------------------------------------

func (m *model) setView(v int) {
	if v == m.leftView {
		return
	}
	m.leftView = v
	if m.narrow {
		m.focus = focusList
	}
	m.treeCursor = 0
	m.commentCur = 0
	m.fileFindCur = 0
	m.clampScrolls()
	switch v {
	case viewFindings:
		m.msg = "findings"
	case viewFiles:
		m.msg = fmt.Sprintf("changed files · %d reviewed", len(m.reviewedSet()))
	case viewComments:
		m.msg = fmt.Sprintf("comments · %d local drafts", len(m.comments))
	case viewFileFindings:
		if m.fileFindingsFile == "" {
			m.msg = "in file — pick a file (2), then Enter"
		} else {
			m.msg = "findings in " + shortPath(m.fileFindingsFile)
		}
	}
}

// fileFindingsCount returns the number of filtered findings for the
// currently focused file (viewFileFindings). Returns 0 when no file is set.
func (m *model) fileFindingsCount() int {
	if m.fileFindingsFile == "" {
		return 0
	}
	n := 0
	for _, idx := range m.filtered {
		if m.findings[idx].File == m.fileFindingsFile {
			n++
		}
	}
	return n
}

// trackFileFindingsTarget keeps m.fileFindingsFile in sync with the file
// currently highlighted in FILES view. This lets the user navigate the tree
// with j/k, then press 4 to see that file's findings — no Enter required,
// no second source of truth.
func (m *model) trackFileFindingsTarget() {
	if m.leftView != viewFiles {
		return
	}
	if r := m.treeRowAt(m.treeCursor); r != nil && !r.node.dir {
		m.fileFindingsFile = r.node.path
	}
}

// currentFileFindingID returns the ID of the file-scoped finding under the
// cursor in viewFileFindings (empty when none / outside that view).
func (m *model) currentFileFindingID() string {
	if m.leftView != viewFileFindings || m.fileFindingsFile == "" {
		return ""
	}
	n := 0
	for _, idx := range m.filtered {
		if m.findings[idx].File != m.fileFindingsFile {
			continue
		}
		if n == m.fileFindCur {
			return m.findings[idx].ID
		}
		n++
	}
	return ""
}

// ---- diff navigation -------------------------------------------------------

func (m *model) hunkJump(dir int) {
	if m.browseFile != "" {
		doc := m.browseDoc()
		n := len(doc)
		if n == 0 {
			return
		}
		if dir > 0 {
			for i := m.diffCursor + 1; i < n; i++ {
				if doc[i].dl.Kind == DiffHunkHeader {
					m.diffCursor = i
					m.syncBrowseFile()
					m.ensureBrowseVisible()
					return
				}
			}
			m.msg = "last hunk"
			return
		}
		for i := m.diffCursor - 1; i >= 0; i-- {
			if doc[i].dl.Kind == DiffHunkHeader {
				m.diffCursor = i
				m.syncBrowseFile()
				m.ensureBrowseVisible()
				return
			}
		}
		m.msg = "first hunk"
		return
	}
	// findings mode: windowed jump around the focused finding
	m.jumpHunk(dir)
}

// fileJump moves to the next/previous changed file. In browse mode it browses
// that file; in findings mode it jumps to the first finding of the adjacent
// file (switching to file sort).
func (m *model) fileJump(dir int) {
	files := m.changedFiles()
	if len(files) == 0 {
		return
	}
	cur := m.browseFile
	if cur == "" {
		if f := m.current(); f != nil {
			cur = f.File
		}
	}
	if cur == "" {
		cur = files[0]
	}
	idx := sort.SearchStrings(files, cur)
	if idx >= len(files) || files[idx] != cur {
		idx = 0
	}
	next := idx + dir
	if next < 0 {
		next = 0
		m.msg = "first file"
		return
	} else if next >= len(files) {
		next = len(files) - 1
		m.msg = "last file"
		return
	}
	if m.browseFile != "" {
		m.browseFilePath(files[next])
		m.msg = shortPath(files[next])
		return
	}
	// findings mode: sort by file, select its first finding
	if m.sortMode != sortFile {
		m.sortMode = sortFile
		m.rebuild()
	}
	for i, fi := range m.filtered {
		if m.findings[fi].File == files[next] {
			m.cursor = i
			m.syncDiffScroll()
			m.msg = shortPath(files[next])
			return
		}
	}
}

func (m *model) ctxAdjust(delta int) {
	if m.focus != focusDetail || m.tab != tabDiff || m.browseFile != "" {
		return
	}
	m.ctxLines += delta
	if m.ctxLines < 2 {
		m.ctxLines = 2
	}
	m.clampScrolls()
	m.syncDiffScroll()
}

func (m *model) horizScroll(delta int) {
	if m.focus != focusDetail || m.tab != tabDiff {
		return
	}
	m.diffHOff += delta
	if m.diffHOff < 0 {
		m.diffHOff = 0
	}
}

// ---- browse mode (continuous all-MR diff) ----------------------------------

// browseDocLen is the total row count of the continuous document.
func (m *model) browseDocLen() int { return len(m.browseDoc()) }

// browseRowAt returns the document row at a global index (nil when out of
// range or on a file header row).
func (m *model) browseRowAt(i int) *browseRow {
	doc := m.browseDoc()
	if i < 0 || i >= len(doc) || doc[i].header {
		return nil
	}
	return &doc[i]
}

// firstAddressableRow returns the global index of the first row of a file's
// section that can carry a comment (skips the file header and hunk headers).
func (m *model) firstAddressableRow(path string) int {
	doc := m.browseDoc()
	seen := false
	for i, r := range doc {
		if r.file != path {
			if seen {
				break
			}
			continue
		}
		seen = true
		if !r.header && r.dl.Kind != DiffHunkHeader {
			return i
		}
	}
	return 0
}

// browseFinding opens the selected finding's location in the continuous diff.
func (m *model) browseFinding() {
	f := m.current()
	if f == nil {
		return
	}
	m.ensureDiff()
	if m.diffParsed[f.File] == nil {
		m.msg = "file not part of this diff: " + f.File
		return
	}
	m.browseFile = f.File
	m.diffCursor = m.nearestDocRow(f.File, f.Side, f.Line)
	m.visualOn, m.visualStart = false, -1
	m.focus = focusDetail
	m.tab = tabDiff
	m.diffHOff = 0
	m.ensureBrowseVisible()
	m.msg = "continuous diff at " + shortPath(f.File) + " (esc returns to findings)"
}

func (m *model) exitBrowse() {
	m.browseFile = ""
	m.visualOn, m.visualStart = false, -1
	m.syncDiffScroll()
	m.clampScrolls()
	m.msg = "back to findings"
}

// browseMove moves the diff cursor through the whole document (extending an
// active visual selection); browseFile follows the cursor's file.
func (m *model) browseMove(delta int) {
	n := m.browseDocLen()
	if n == 0 {
		return
	}
	m.diffCursor += delta
	if m.diffCursor < 0 {
		m.diffCursor = 0
	}
	if m.diffCursor >= n {
		m.diffCursor = n - 1
	}
	m.syncBrowseFile()
	m.ensureBrowseVisible()
}

// browseViewportH is the number of visible rows in the diff pane.
func (m *model) browseViewportH() int {
	h := m.contentHeight() - 1 // header row
	if h < 3 {
		h = 3
	}
	return h
}

// ensureBrowseVisible keeps the cursor row inside the viewport.
func (m *model) ensureBrowseVisible() {
	h := m.browseViewportH()
	top := m.detailTop[tabDiff]
	if m.diffCursor < top {
		top = m.diffCursor
	}
	if m.diffCursor >= top+h {
		top = m.diffCursor - h + 1
	}
	if top < 0 {
		top = 0
	}
	m.detailTop[tabDiff] = top
}

// nearestDocRow finds the global row index of the line closest to `line` on
// `side` within one file's section of the document.
func (m *model) nearestDocRow(file, side string, line int) int {
	doc := m.browseDoc()
	best, bestDist := 0, 1<<30
	for i, r := range doc {
		if r.header || r.file != file {
			continue
		}
		no := r.dl.numberFor(side)
		if no == 0 {
			continue
		}
		d := abs(no - line)
		if d < bestDist {
			bestDist, best = d, i
		}
	}
	return best
}

// ---- findings helpers ------------------------------------------------------

// jumpPending moves to the next/previous pending finding.
func (m *model) jumpPending(dir int) {
	n := len(m.filtered)
	if n == 0 {
		return
	}
	for step := 1; step <= n; step++ {
		idx := (m.cursor + dir*step + n) % n
		f := &m.findings[m.filtered[idx]]
		if f.Decision == "pending" && f.Publication == nil && !f.Stale {
			m.cursor = idx
			m.syncDiffScroll()
			m.msg = "pending " + f.ID
			return
		}
	}
	m.msg = "no other pending findings"
}

// setStatusFilter switches the findings status filter (pending/approved/…)
// while keeping the severity and text parts, and preserves the selection.
func (m *model) setStatusFilter(kind string) {
	m.filter.Kind = kind
	id := m.currentID()
	m.rebuild()
	m.reselect(id)
	m.msg = "filter: " + m.filter.Describe()
}

func (m *model) cycleSort() {
	m.sortMode = (m.sortMode + 1) % 3
	id := m.currentID()
	m.rebuild()
	m.reselect(id)
	m.msg = "sort: " + []string{"severity", "file", "decision"}[m.sortMode]
}

// startDiscuss opens the AI question input (existing behaviour of d).
func (m *model) startDiscuss() tea.Cmd {
	f := m.current()
	if f == nil {
		m.msg = "no finding selected"
		return nil
	}
	m.inputOn = true
	m.input.Focus()
	return textinput.Blink
}

// openSelection reacts to Enter depending on the focused view.
func (m *model) openSelection() tea.Cmd {
	if wsPane != nil {
		// hybrid workspace: everything happens INSIDE the cockpit strip
		// (the tuicr pane keeps running untouched — no fullscreen takeover)
		switch m.leftView {
		case viewFindings:
			if m.current() != nil {
				wsDetail = true // finding details in the strip
				wsDetailTop = 0
			}
		case viewFiles:
			// drill down: file → its findings submenu
			if r := m.treeRowAt(m.treeCursor); r != nil && !r.node.dir {
				m.fileFindingsFile = r.node.path
				m.setView(viewFileFindings)
				m.fileFindCur = 0
			} else if r != nil {
				r.node.expanded = !r.node.expanded
				m.rebuildTreeRows()
			}
		case viewFileFindings:
			m.focus = focusDetail
			m.clampScrolls()
		case viewComments:
			m.jumpToComment()
		}
		return nil
	}
	if m.focus == focusList {
		switch m.leftView {
		case viewFindings:
			m.focus = focusDetail
			m.clampScrolls()
		case viewFiles:
			if r := m.treeRowAt(m.treeCursor); r != nil {
				if r.node.dir {
					r.node.expanded = !r.node.expanded
					m.rebuildTreeRows()
				} else {
					m.fileFindingsFile = r.node.path // 4 jumps here from FILES view
					m.browseFilePath(r.node.path)
				}
			}
		case viewFileFindings:
			// Enter drills into the focused finding (same as FINDINGS tab)
			m.focus = focusDetail
			m.clampScrolls()
		case viewComments:
			m.jumpToComment()
		}
		return nil
	}
	if m.focus == focusDetail && m.tab == tabDiff && m.browseFile != "" {
		// Enter on a diff row with an active selection opens the range editor
		if m.visualOn {
			return m.startDraftComment()
		}
	}
	return nil
}

// browseFilePath browses a file from the tree (first hunk in view).
func (m *model) browseFilePath(path string) {
	m.ensureDiff()
	if m.diffParsed[path] == nil {
		m.msg = "not part of this diff"
		return
	}
	m.diffCursor = m.firstAddressableRow(path)
	m.syncBrowseFile()
	m.focus = focusDetail
	m.tab = tabDiff
	m.ensureBrowseVisible()
	m.msg = shortPath(path)
}

// ---- changed files / review progress --------------------------------------

func (m *model) changedFiles() []string {
	m.ensureDiff()
	out := make([]string, 0, len(m.diffParsed))
	for p := range m.diffParsed {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func (m *model) reviewedSet() map[string]bool {
	out := map[string]bool{}
	for _, f := range m.rev.ReviewedFiles {
		out[f] = true
	}
	return out
}

func (m *model) treeSelection() string {
	if m.leftView != viewFiles {
		return ""
	}
	if r := m.treeRowAt(m.treeCursor); r != nil && !r.node.dir {
		return r.node.path
	}
	return ""
}

// currentReviewedTarget is the file a reviewed-toggle applies to.
func (m *model) currentReviewedTarget() string {
	if m.browseFile != "" {
		return m.browseFile
	}
	return m.treeSelection()
}

func (m *model) toggleReviewedFile() {
	path := m.currentReviewedTarget()
	if path == "" {
		m.msg = "select a file first"
		return
	}
	set := m.reviewedSet()
	if set[path] {
		delete(set, path)
		m.msg = "un-reviewed " + shortPath(path)
	} else {
		set[path] = true
		m.msg = "reviewed " + shortPath(path)
	}
	m.rev.ReviewedFiles = nil
	for f := range set {
		m.rev.ReviewedFiles = append(m.rev.ReviewedFiles, f)
	}
	sort.Strings(m.rev.ReviewedFiles)
	if err := m.rev.Save(m.dir); err != nil {
		m.msg = "save failed: " + err.Error()
	}
}

// nextUnreviewed jumps to the next changed file without a reviewed mark.
func (m *model) nextUnreviewed() {
	files := m.changedFiles()
	set := m.reviewedSet()
	cur := m.currentReviewedTarget()
	start := 0
	for i, f := range files {
		if f == cur {
			start = i + 1
			break
		}
	}
	for step := 0; step < len(files); step++ {
		f := files[(start+step)%len(files)]
		if !set[f] {
			if m.leftView == viewFiles {
				m.revealFileRow(f)
			}
			m.browseFilePath(f)
			m.msg = "unreviewed: " + shortPath(f)
			return
		}
	}
	m.msg = "all changed files reviewed — nice"
}

// hunkKey identifies a hunk for review tracking: "path|newStart".
func (m *model) hunkKey(rowIdx int) string {
	doc := m.browseDoc()
	if rowIdx < 0 || rowIdx >= len(doc) || doc[rowIdx].header {
		return ""
	}
	path := doc[rowIdx].file
	for i := rowIdx; i >= 0; i-- {
		if doc[i].header {
			return "" // walked past the file start: no hunk header above
		}
		if doc[i].dl.Kind == DiffHunkHeader {
			_, newStart := parseHunkHeader(doc[i].dl.Text)
			return fmt.Sprintf("%s|%d", path, newStart)
		}
	}
	return ""
}

func (m *model) toggleReviewedHunk() {
	if m.browseFile == "" {
		m.msg = "hunk review works in the continuous diff (gd)"
		return
	}
	key := m.hunkKey(m.diffCursor)
	if key == "" {
		m.msg = "no hunk at the cursor"
		return
	}
	found := false
	var out []string
	for _, k := range m.rev.ReviewedHunks {
		if k == key {
			found = true
			continue
		}
		out = append(out, k)
	}
	if !found {
		out = append(out, key)
		m.msg = "hunk reviewed"
	} else {
		m.msg = "hunk un-reviewed"
	}
	m.rev.ReviewedHunks = out
	if err := m.rev.Save(m.dir); err != nil {
		m.msg = "save failed: " + err.Error()
	}
}

func (m *model) reviewedHunkSet() map[string]bool {
	out := map[string]bool{}
	for _, k := range m.rev.ReviewedHunks {
		out[k] = true
	}
	return out
}

// revealFileRow expands the tree so the file row is visible and selects it.
func (m *model) revealFileRow(path string) {
	if m.tree == nil {
		m.buildTree()
	}
	segs := strings.Split(path, "/")
	node := (*treeNode)(nil)
	var kids *[]*treeNode = &m.tree.roots
	for i, s := range segs {
		found := (*treeNode)(nil)
		for _, c := range *kids {
			if c.name == s {
				found = c
				break
			}
		}
		if found == nil {
			return
		}
		node = found
		if i < len(segs)-1 {
			node.expanded = true
			kids = &node.children
		}
	}
	m.rebuildTreeRows()
	for i, r := range m.tree.rows {
		if !r.node.dir && r.node.path == path {
			m.treeCursor = i
			return
		}
	}
}

// ---- search ----------------------------------------------------------------

func (m *model) openSearch() {
	m.searchOn = true
	m.searchInput.SetValue(m.searchText)
	m.searchInput.Focus()
}

func (m *model) updateSearch(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.searchOn = false
		m.searchInput.Blur()
		m.searchText = "" // the footer promises Esc=clear
		m.applyViewFilters()
		return m, nil
	case "enter":
		m.searchOn = false
		m.searchInput.Blur()
		m.searchText = strings.TrimSpace(m.searchInput.Value())
		m.applyViewFilters()
		if m.searchText != "" {
			m.searchJump(1)
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	if m.focus == focusList { // live filter the navigator views
		m.searchText = strings.TrimSpace(m.searchInput.Value())
		m.applyViewFilters()
	}
	return m, cmd
}

// applyViewFilters recomputes all filtered views after a search change.
func (m *model) applyViewFilters() {
	id := m.currentID()
	m.filter.Text = m.searchText
	m.rebuild()
	m.reselect(id)
	m.rebuildTreeRows()
	m.commentCur = 0
}

// matchCount returns the number of matches in the current search context.
func (m *model) searchMatches() int {
	if m.searchText == "" {
		return 0
	}
	q := strings.ToLower(m.searchText)
	switch {
	case m.focus == focusDetail && m.tab == tabDiff:
		n := 0
		for _, r := range m.browseDoc() {
			if !r.header && strings.Contains(strings.ToLower(r.dl.Text), q) {
				n++
			}
		}
		return n
	case m.leftView == viewFindings:
		return len(m.filtered)
	case m.leftView == viewFiles:
		n := 0
		for _, r := range m.treeRows() {
			if !r.node.dir && strings.Contains(strings.ToLower(r.node.path), q) {
				n++
			}
		}
		return n
	case m.leftView == viewComments:
		return m.filteredCommentCount()
	}
	return 0
}

// searchJump moves to the next/previous match in the current context.
func (m *model) searchJump(dir int) {
	if m.searchText == "" {
		m.msg = "no search — press / first"
		return
	}
	q := strings.ToLower(m.searchText)
	if m.focus == focusDetail && m.tab == tabDiff {
		doc := m.browseDoc()
		n := len(doc)
		if n == 0 {
			return
		}
		for step := 1; step <= n; step++ {
			i := (m.diffCursor + dir*step + n) % n
			if !doc[i].header && strings.Contains(strings.ToLower(doc[i].dl.Text), q) {
				m.diffCursor = i
				m.syncBrowseFile()
				m.ensureBrowseVisible()
				m.msg = fmt.Sprintf("/%s · %d matches", m.searchText, m.searchMatches())
				return
			}
		}
		m.msg = "no matches for /" + m.searchText
		return
	}
	// navigator views are already live-filtered; jump to the first row
	if m.focus == focusList {
		switch m.leftView {
		case viewFindings:
			m.cursor = 0
			if len(m.filtered) > 0 {
				m.syncDiffScroll()
			}
		case viewFiles:
			m.treeCursor = 0
		case viewComments:
			m.commentCur = 0
		}
	}
}

// ---- misc ------------------------------------------------------------------

func (m *model) jumpToComment() {
	r := m.selectedRow()
	if r == nil {
		return
	}
	switch r.kind {
	case "draft":
		c := r.draft
		if c.General {
			m.msg = "general comment (no diff location)"
			return
		}
		m.browseFilePath(c.File)
		m.diffCursor = m.nearestDocRow(c.File, c.Side, c.Line)
		m.syncBrowseFile()
		m.ensureBrowseVisible()
		m.msg = "comment " + c.ID + " at " + c.Location()
	case "finding":
		f := r.finding
		m.ensureDiff()
		if m.diffParsed[f.File] == nil {
			m.msg = "file not part of this diff"
			return
		}
		m.diffCursor = m.nearestDocRow(f.File, f.Side, f.Line)
		m.syncBrowseFile()
		m.focus, m.tab = focusDetail, tabDiff
		m.ensureBrowseVisible()
		m.msg = "finding " + f.ID + " in diff"
	default:
		m.msg = "remote discussion — read-only here"
	}
}

func shortPath(p string) string {
	if len(p) > 44 {
		return "…" + p[len(p)-43:]
	}
	return p
}
