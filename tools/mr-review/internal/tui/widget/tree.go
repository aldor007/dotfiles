package widget

import "strings"

// TreeNode is one directory or file in a rendered tree (plain data — the
// TUI style layer decides colors and decorations).
type TreeNode struct {
	Name     string
	Path     string // full repo-relative path (files only)
	Dir      bool
	Expanded bool
	Status   string // file status marker ("A", "M", "D", " " for files; "" for dirs)
	Meta     []string
	Reviewed bool
	Children []*TreeNode
}

// TreeRow is one visible row of a flattened tree.
type TreeRow struct {
	Node  *TreeNode
	Depth int
}

// FlattenTree walks the roots depth-first, emitting visible rows. Dirs emit
// children when expanded or when a filter is active (filtered trees keep the
// ancestors of matching files visible and expanded — vi-SQL schema-tree
// behavior). A non-empty filter keeps only files whose path contains it
// (case-insensitive), plus their ancestor dirs.
func FlattenTree(roots []*TreeNode, filter string) []TreeRow {
	q := strings.ToLower(filter)
	var keep func(n *TreeNode) bool
	keep = func(n *TreeNode) bool {
		if !n.Dir {
			return q == "" || strings.Contains(strings.ToLower(n.Path), q)
		}
		for _, c := range n.Children {
			if keep(c) {
				return true
			}
		}
		return false
	}
	var out []TreeRow
	var walk func(nodes []*TreeNode, depth int)
	walk = func(nodes []*TreeNode, depth int) {
		for _, n := range nodes {
			if !keep(n) {
				continue
			}
			out = append(out, TreeRow{Node: n, Depth: depth})
			if n.Dir && (n.Expanded || q != "") {
				walk(n.Children, depth+1)
			}
		}
	}
	walk(roots, 0)
	return out
}

// IndentPrefix is the leading spaces for a row at the given depth (two cells
// per level, matching the legacy tree rendering).
func IndentPrefix(depth int) string {
	if depth <= 0 {
		return ""
	}
	return strings.Repeat("  ", depth)
}
