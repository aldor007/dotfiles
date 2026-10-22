package tui

import (
	"strings"
	"testing"
)

const sampleDiff = `diff --git a/services/review/handler.go b/services/review/handler.go
--- a/services/review/handler.go
+++ b/services/review/handler.go
@@ -10,6 +10,9 @@ func handler() {
 	context.Background()
+	ctx, cancel := context.WithCancel(ctx)
+	defer cancel()
 	return
 }
diff --git a/services/review/other.go b/services/review/other.go
--- a/services/review/other.go
+++ b/services/review/other.go
@@ -100,4 +100,5 @@ func other() {
 	a()
+	b()
 	c()
`

func TestParseDiffLineNumbers(t *testing.T) {
	files := ParseDiff(sampleDiff, 0)
	f := files["services/review/handler.go"]
	if f == nil {
		t.Fatal("handler.go missing")
	}
	// hunk: @@ -10,6 +10,9 @@ → context@10, add@10(new 10), add, context(10/13)
	var got []string
	for _, l := range f.Lines {
		if l.Kind == DiffHunkHeader {
			continue
		}
		got = append(got, l.Kind.String()+":"+itoa2(l.NewNo))
	}
	// unified-diff semantics: the leading context line owns new-line 10;
	// the two added lines are new-lines 11 and 12 (per @@ -10,6 +10,9)
	want := "ctx:10,add:11,add:12,ctx:13,ctx:14"
	if strings.Join(got, ",") != want {
		t.Fatalf("numbers: got %v want %s", got, want)
	}
}

func TestDiffWindowTargetsFindingLine(t *testing.T) {
	// a brand-new file with 300 lines: the finding at :172 must show ~172,
	// not the imports at the top
	var b strings.Builder
	b.WriteString("diff --git a/big.go b/big.go\n--- /dev/null\n+++ b/big.go\n@@ -0,0 +1,300 @@\n")
	for i := 1; i <= 300; i++ {
		b.WriteString("+line " + itoa2(i) + "\n")
	}
	files := ParseDiff(b.String(), 0)
	rows, hl := DiffWindow(files["big.go"], "new", 172, 172, 5)
	if len(rows) == 0 {
		t.Fatal("empty window")
	}
	if !strings.Contains(rows[hl].Text, "line 172") {
		t.Fatalf("highlight row is %q, want line 172", rows[hl].Text)
	}
	// window should start near 167, not at 1
	first := firstNumbered(rows, "new")
	if first < 165 || first > 170 {
		t.Fatalf("window starts at %d, want ~167", first)
	}
}

func TestDiffWindowMultiHunkAndSecondFile(t *testing.T) {
	files := ParseDiff(sampleDiff, 0)
	// finding in the SECOND file at :101 (b() was added at new line 101)
	rows, hl := DiffWindow(files["services/review/other.go"], "new", 101, 101, 2)
	if hl < 0 || !strings.Contains(rows[hl].Text, "b()") {
		t.Fatalf("highlight = %d %q, want b()", hl, rows[hl].Text)
	}
	// finding in the first file near the hunk start: the leading context
	// line owns new-line 10; the first added line is new-line 11
	rows, hl = DiffWindow(files["services/review/handler.go"], "new", 11, 11, 3)
	if !strings.Contains(rows[hl].Text, "ctx, cancel") {
		t.Fatalf("highlight = %q", rows[hl].Text)
	}
}

func TestDiffWindowDeletedFileAndOldSide(t *testing.T) {
	diff := `diff --git a/gone.go b/gone.go
--- a/gone.go
+++ b/gone.go
@@ -5,3 +1,2 @@
-	one()
-	two()
 	three()
`
	files := ParseDiff(diff, 0)
	fd := files["gone.go"]
	if fd == nil {
		t.Fatal("gone.go missing")
	}
	// old-side finding at :6 (two())
	rows, hl := DiffWindow(fd, "old", 6, 6, 2)
	if hl < 0 || !strings.Contains(rows[hl].Text, "two()") {
		t.Fatalf("old-side highlight = %d %q", hl, rows[hl].Text)
	}
	if rows[hl].OldNo != 6 {
		t.Fatalf("old number = %d, want 6", rows[hl].OldNo)
	}
}

func TestDiffWindowNewFileSideOldFallback(t *testing.T) {
	// a finding reported on side=old for a brand-new file: no old numbers
	// exist → fall back to content instead of an empty window
	var b strings.Builder
	b.WriteString("diff --git a/new.go b/new.go\n--- /dev/null\n+++ b/new.go\n@@ -0,0 +1,5 @@\n")
	for i := 1; i <= 5; i++ {
		b.WriteString("+alpha " + itoa2(i) + "\n")
	}
	files := ParseDiff(b.String(), 0)
	rows, _ := DiffWindow(files["new.go"], "old", 2, 2, 2)
	if len(rows) == 0 {
		t.Fatal("expected fallback content for old side of a new file")
	}
}

func TestClipPlainPreservesANSIAndCountsCells(t *testing.T) {
	s := "\x1b[32mok\x1b[0m " + strings.Repeat("x", 40)
	got := clipPlain(s, 10)
	if !strings.Contains(got, "\x1b[32m") {
		t.Fatal("ANSI sequence dropped")
	}
	if cells := visibleCells(got); cells > 10 {
		t.Fatalf("clip exceeded width: %d cells", cells)
	}
	if got2 := clipPlain("日本語テスト", 6); visibleCells(got2) > 6 {
		t.Fatalf("wide runes miscounted: %q", got2)
	}
}

// --- helpers ---------------------------------------------------------------

func (k DiffLineKind) String() string {
	switch k {
	case DiffAdd:
		return "add"
	case DiffDel:
		return "del"
	case DiffContext:
		return "ctx"
	default:
		return "hdr"
	}
}

func itoa2(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func firstNumbered(rows []DiffLine, side string) int {
	for _, l := range rows {
		if n := l.numberFor(side); n != 0 {
			return n
		}
	}
	return 0
}

// visibleCells counts printable cells, ignoring ANSI sequences and
// counting multi-byte runes as one cell.
func visibleCells(s string) int {
	cells := 0
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !(s[j] >= 0x40 && s[j] <= 0x7e) {
				j++
			}
			if j < len(s) {
				i = j + 1
				continue
			}
		}
		if s[i] < 0x80 {
			cells++
			i++
			continue
		}
		r, size := decodeRune(s[i:])
		_ = r
		cells++
		i += size
	}
	return cells
}

func decodeRune(s string) (rune, int) {
	for size := 2; size <= 4; size++ {
		if len(s) >= size {
			r := []rune(s[:size])
			if len(r) == 1 && r[0] != 0xFFFD {
				return r[0], size
			}
		}
	}
	return 0xFFFD, 1
}
