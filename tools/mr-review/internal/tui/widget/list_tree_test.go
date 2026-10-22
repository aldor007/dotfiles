package widget

import (
	"reflect"
	"testing"
)

func list(l Len) *List { return &List{Len: int(l)} }

type Len int

func TestListMoveClamps(t *testing.T) {
	l := list(5)
	if !l.Move(2) || l.Cursor != 2 {
		t.Fatalf("Move(2) = cursor %d", l.Cursor)
	}
	if !l.Move(-10) || l.Cursor != 0 {
		t.Fatalf("Move(-10) must clamp to 0, got %d", l.Cursor)
	}
	if !l.Move(99) || l.Cursor != 4 {
		t.Fatalf("Move(99) must clamp to Len-1, got %d", l.Cursor)
	}
	if l.Move(1) {
		t.Error("Move at the last row must report no change")
	}
}

func TestListEmpty(t *testing.T) {
	l := list(0)
	if l.Move(1) {
		t.Error("Move on an empty list must not move")
	}
	if l.Cursor != 0 {
		t.Errorf("empty list cursor = %d", l.Cursor)
	}
	l.Last()
	if l.Cursor != 0 {
		t.Errorf("Last on empty list = %d", l.Cursor)
	}
	start, end := l.Window(5)
	if start != 0 || end != 0 {
		t.Errorf("empty window = [%d,%d)", start, end)
	}
}

func TestListEnsureVisible(t *testing.T) {
	l := list(100)
	l.Cursor = 0
	l.EnsureVisible(10)
	if l.Top != 0 {
		t.Fatalf("Top = %d, want 0", l.Top)
	}
	l.Cursor = 10 // first row below the fold
	l.EnsureVisible(10)
	if l.Top != 1 {
		t.Fatalf("Top = %d, want 1", l.Top)
	}
	l.Cursor = 0 // back above the fold
	l.EnsureVisible(10)
	if l.Top != 0 {
		t.Fatalf("Top = %d, want 0", l.Top)
	}
	l.Cursor = 99 // last row
	l.EnsureVisible(10)
	if l.Top != 90 {
		t.Fatalf("Top = %d, want 90", l.Top)
	}
}

func TestListClampAfterShrink(t *testing.T) {
	l := list(10)
	l.Top = 8
	l.Clamp(3) // max top = 7
	if l.Top != 7 {
		t.Fatalf("Top = %d, want 7", l.Top)
	}
	l.Top = -2
	l.Clamp(3)
	if l.Top != 0 {
		t.Fatalf("Top = %d, want 0", l.Top)
	}
}

func TestListWindow(t *testing.T) {
	l := list(7)
	l.Cursor = 5
	start, end := l.Window(3)
	if start != 3 || end != 6 {
		t.Fatalf("window = [%d,%d), want [3,6)", start, end)
	}
	l.Cursor = 0
	start, end = l.Window(3)
	if start != 0 || end != 3 {
		t.Fatalf("window = [%d,%d), want [0,3)", start, end)
	}
	// viewport larger than the list
	l.Cursor = 6
	start, end = l.Window(20)
	if start != 0 || end != 7 {
		t.Fatalf("window = [%d,%d), want [0,7)", start, end)
	}
}

func TestListFirstLast(t *testing.T) {
	l := list(4)
	l.Last()
	if l.Cursor != 3 {
		t.Fatalf("Last = %d", l.Cursor)
	}
	l.First()
	if l.Cursor != 0 {
		t.Fatalf("First = %d", l.Cursor)
	}
}

func tree() []*TreeNode {
	return []*TreeNode{
		{Name: "services", Path: "services", Dir: true, Expanded: true, Children: []*TreeNode{
			{Name: "api.go", Path: "services/api.go", Status: "M"},
			{Name: "util", Path: "services/util", Dir: true, Expanded: false, Children: []*TreeNode{
				{Name: "x.go", Path: "services/util/x.go", Status: "A"},
			}},
		}},
		{Name: "README.md", Path: "README.md", Status: " "},
	}
}

func TestFlattenTreeExpanded(t *testing.T) {
	rows := FlattenTree(tree(), "")
	// collapsed dirs are still visible rows (that's how you expand them)
	want := []string{"services", "services/api.go", "services/util", "README.md"}
	var got []string
	for _, r := range rows {
		got = append(got, r.Node.Path)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
	// depths — but collapsed util's children are NOT walked
	if rows[0].Depth != 0 || rows[1].Depth != 1 || rows[2].Depth != 1 || rows[3].Depth != 0 {
		t.Errorf("depths = %d,%d,%d,%d", rows[0].Depth, rows[1].Depth, rows[2].Depth, rows[3].Depth)
	}
	for _, r := range rows {
		if r.Node.Path == "services/util/x.go" {
			t.Error("collapsed dir's children must not be emitted")
		}
	}
}

func TestFlattenTreeFilterKeepsAncestors(t *testing.T) {
	rows := FlattenTree(tree(), "x.go")
	var got []string
	for _, r := range rows {
		got = append(got, r.Node.Path)
	}
	want := []string{"services", "services/util", "services/util/x.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("filtered rows = %v, want %v", got, want)
	}
	// the collapsed dir must be walked (children visible) under a filter,
	// without mutating the caller's nodes
	if tree()[0].Children[1].Expanded {
		t.Error("FlattenTree must not mutate node.Expanded")
	}
}

func TestFlattenTreeFilterNoMatch(t *testing.T) {
	if rows := FlattenTree(tree(), "nope"); len(rows) != 0 {
		t.Errorf("no-match filter returned %d rows", len(rows))
	}
}

func TestFlattenTreeCaseInsensitive(t *testing.T) {
	if rows := FlattenTree(tree(), "readme"); len(rows) != 1 {
		t.Errorf("case-insensitive filter matched %d rows, want 1", len(rows))
	}
}

func TestIndentPrefix(t *testing.T) {
	if IndentPrefix(0) != "" || IndentPrefix(2) != "    " {
		t.Error("IndentPrefix must emit two spaces per level")
	}
}
