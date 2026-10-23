// diffview parses a unified diff into per-file hunks with real line numbers
// and produces focused windows around a finding's location. Pure functions —
// fully unit-testable, no terminal I/O.
package tui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	chromaformatters "github.com/alecthomas/chroma/v2/formatters"
	chromalexers "github.com/alecthomas/chroma/v2/lexers"
	chromastyles "github.com/alecthomas/chroma/v2/styles"
	"github.com/sourcegraph/go-diff/diff"
)

// minimal ANSI helpers (terminal-independent; lipgloss handles color depth)
const (
	reset   = "\x1b[0m"
	inverse = "\x1b[7m"
	red     = "\x1b[31m"
	green   = "\x1b[32m"
	cyan    = "\x1b[36m"
)

// DiffLineKind classifies one diff line.
type DiffLineKind int

const (
	DiffContext DiffLineKind = iota
	DiffAdd
	DiffDel
	DiffHunkHeader
)

// DiffLine is one rendered diff row with resolved line numbers.
type DiffLine struct {
	Kind    DiffLineKind
	OldNo   int // 0 when not applicable
	NewNo   int // 0 when not applicable
	Text    string
	Headers bool // true for @@ header rows
}

// FileDiff holds all hunks of one file inside the diff.
type FileDiff struct {
	Path  string // new path ("" for pure deletions: old path)
	Old   bool   // file was deleted
	Lines []DiffLine

	// git extended-header metadata (go-diff exposes the raw header lines;
	// rename info IS parsed upstream — no guessing required here).
	OldPath string // original path for renames/deletions ("" when unchanged)
	Rename  bool   // git rename detected (OldPath != Path)
	Binary  bool   // binary file: no textual hunks in the diff
}

// ParseDiff converts a unified diff into per-file structures keyed by path.
// go-diff handles the file/hunk-header parsing (names, line starts, renames);
// the hunk Body is walked for per-line classification. Lines are capped to
// keep enormous diffs cheap (rendering only ever slices windows out of these).
func ParseDiff(diffText string, maxLines int) map[string]*FileDiff {
	files := map[string]*FileDiff{}
	fds, err := diff.ParseMultiFileDiff([]byte(diffText))
	if err != nil && len(fds) == 0 {
		return files
	}
	n := 0
	for _, fd := range fds {
		newPath := strings.TrimPrefix(fd.NewName, "b/")
		if newPath == "/dev/null" {
			newPath = strings.TrimPrefix(fd.OrigName, "a/")
		}
		cur := &FileDiff{Path: newPath, Old: fd.NewName == "/dev/null"}
		if old := strings.TrimPrefix(fd.OrigName, "a/"); old != "/dev/null" && old != newPath {
			cur.OldPath = old
			cur.Rename = true
		}
		cur.Binary = fileDiffIsBinary(fd)
		files[newPath] = cur
		for _, h := range fd.Hunks {
			if maxLines > 0 && n > maxLines {
				break
			}
			oldNo, newNo := int(h.OrigStartLine), int(h.NewStartLine)
			cur.Lines = append(cur.Lines, DiffLine{
				Kind: DiffHunkHeader, Headers: true,
				Text: fmt.Sprintf("@@ -%d,%d +%d,%d @@ %s",
					h.OrigStartLine, h.OrigLines, h.NewStartLine, h.NewLines, h.Section),
			})
			for _, raw := range strings.Split(strings.TrimRight(string(h.Body), "\n"), "\n") {
				n++
				if maxLines > 0 && n > maxLines {
					break
				}
				content := ""
				switch {
				case strings.HasPrefix(raw, "+"):
					content = strings.TrimPrefix(raw, "+")
					cur.Lines = append(cur.Lines, DiffLine{Kind: DiffAdd, NewNo: newNo, Text: content})
					newNo++
				case strings.HasPrefix(raw, "-"):
					content = strings.TrimPrefix(raw, "-")
					cur.Lines = append(cur.Lines, DiffLine{Kind: DiffDel, OldNo: oldNo, Text: content})
					oldNo++
				default:
					content = strings.TrimPrefix(raw, " ")
					cur.Lines = append(cur.Lines, DiffLine{Kind: DiffContext, OldNo: oldNo, NewNo: newNo, Text: content})
					oldNo++
					newNo++
				}
			}
		}
	}
	return files
}

// fileDiffIsBinary reports whether a parsed FileDiff carries no textual
// hunks because the patch is binary ("Binary files … differ" / "GIT binary
// patch" extended headers, surfaced by go-diff in FileDiff.Extended).
func fileDiffIsBinary(fd *diff.FileDiff) bool {
	if fd == nil || len(fd.Hunks) > 0 {
		return false
	}
	for _, ext := range fd.Extended {
		l := strings.ToLower(ext)
		if strings.Contains(l, "binary") {
			return true
		}
	}
	return false
}

// parseHunkHeader extracts the new-file start line from "@@ -a,b +c,d @@".
func parseHunkHeader(header string) (oldStart, newStart int) {
	oldStart, newStart = 1, 1
	plusIdx := strings.Index(header, "+")
	if plusIdx < 0 {
		return
	}
	rest := header[plusIdx+1:]
	end := strings.IndexAny(rest, ", @")
	if end < 0 {
		end = len(rest)
	}
	if n, err := strconv.Atoi(rest[:end]); err == nil {
		newStart = n
	}
	minusIdx := strings.Index(header, "-")
	if minusIdx >= 0 {
		rest := header[minusIdx+1:]
		end := strings.IndexAny(rest, ", @")
		if end < 0 {
			end = len(rest)
		}
		if n, err := strconv.Atoi(rest[:end]); err == nil {
			oldStart = n
		}
	}
	return
}

// numberFor returns the line number of a diff row on the finding's side.
func (l DiffLine) numberFor(side string) int {
	if side == "old" {
		return l.OldNo
	}
	if l.NewNo != 0 {
		return l.NewNo
	}
	return l.OldNo
}

// DiffWindow returns the slice of a file's diff rows surrounding the target
// line with `ctx` lines of context on each side of [line, lineEnd], plus the
// index within the returned slice that should be centered/highlighted.
//
// Target resolution: for side=new the new-file numbers are used; side=old
// uses old numbers (deleted files, old-side findings). Rows without a number
// on the relevant side (headers, opposite-side lines) are kept for display
// but never counted as the target.
func DiffWindow(fd *FileDiff, side string, line, lineEnd, ctx int) ([]DiffLine, int) {
	if fd == nil || len(fd.Lines) == 0 {
		return nil, -1
	}
	lo, hi := line, lineEnd
	if hi < lo {
		hi = lo
	}
	// find the index range [first,last] of rows inside the context window
	bestIdx := -1
	bestDist := 1 << 30
	for i, l := range fd.Lines {
		if l.Kind == DiffHunkHeader {
			continue
		}
		no := l.numberFor(side)
		if no == 0 {
			continue
		}
		d := 0
		if no < lo {
			d = lo - no
		} else if no > hi {
			d = no - hi
		}
		if d < bestDist {
			bestDist = d
			bestIdx = i
		}
	}
	if bestIdx < 0 {
		// nothing numbered on this side (e.g. pure new file viewed as old):
		// fall back to the whole (capped) content
		rows := make([]DiffLine, len(fd.Lines))
		copy(rows, fd.Lines)
		return rows, 0
	}

	// expand from bestIdx while we have context budget
	countLeft, countRight := ctx, ctx
	first, last := bestIdx, bestIdx
	for first > 0 && (countLeft > 0 || fd.Lines[first-1].Kind == DiffHunkHeader && false) {
		first--
		if fd.Lines[first].Kind != DiffHunkHeader {
			countLeft--
		}
	}
	for last < len(fd.Lines)-1 && countRight > 0 {
		last++
		if fd.Lines[last].Kind != DiffHunkHeader {
			countRight--
		}
	}
	// absorb adjacent hunk headers/lines that fall inside the window
	for first > 0 && fd.Lines[first-1].Kind != DiffHunkHeader && withinSideWindow(fd.Lines[first-1], side, lo-ctx, hi+ctx) {
		first--
	}
	for last < len(fd.Lines)-1 && fd.Lines[last+1].Kind != DiffHunkHeader && withinSideWindow(fd.Lines[last+1], side, lo-ctx, hi+ctx) {
		last++
	}
	rows := append([]DiffLine(nil), fd.Lines[first:last+1]...)
	// highlight index: nearest row to `line`
	hl := 0
	bestHl := 1 << 30
	for i, l := range rows {
		if l.Kind == DiffHunkHeader {
			continue
		}
		no := l.numberFor(side)
		d := abs(no - line)
		if no != 0 && d < bestHl {
			bestHl = d
			hl = i
		}
	}
	return rows, hl
}

func withinSideWindow(l DiffLine, side string, lo, hi int) bool {
	no := l.numberFor(side)
	if no == 0 {
		return true // keep headers/context of the other side
	}
	return no >= lo && no <= hi
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// RenderDiffLines styles diff rows with gutters, markers and highlight.
// highlighted (when non-nil) carries per-row Chroma-colored code text that
// replaces the plain l.Text in the output (gutter/marker stay plain).
func RenderDiffLines(rows []DiffLine, side string, hlIdx, width int, plain bool, highlighted []string) []string {
	out := make([]string, 0, len(rows))
	for i, l := range rows {
		marker, markCol := " ", ""
		switch l.Kind {
		case DiffAdd:
			marker, markCol = "+", green
		case DiffDel:
			marker, markCol = "-", red
		case DiffHunkHeader:
			out = append(out, clipPlain(cyan+l.Text+reset, width))
			continue
		}
		code := expandTabs(l.Text)
		if highlighted != nil && i < len(highlighted) {
			code = highlighted[i]
		}
		gutter := fmt.Sprintf("%4s %4s ", numStr(l.OldNo), numStr(l.NewNo))
		line := gutter + markCol + marker + " " + code + reset
		if i == hlIdx && !plain {
			line = inverse + clipPlain(gutter+marker+" "+code, width) + reset
		}
		out = append(out, clipPlain(line, width))
	}
	return out
}

func numStr(n int) string {
	if n == 0 {
		return "    "
	}
	return fmt.Sprintf("%4d", n)
}

// cutByCells truncates a string to width counting printable cells while
// preserving ANSI escape sequences (they occupy no cells).
func clipPlain(s string, width int) string {
	if width <= 0 {
		return ""
	}
	cells := 0
	truncated := false
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !(s[j] >= 0x40 && s[j] <= 0x7e) {
				j++
			}
			if j < len(s) {
				b.WriteString(s[i : j+1])
				i = j + 1
				continue
			}
		}
		r, size := rune(s[i]), 1
		if r >= 0x80 {
			// crude multi-byte handling: decode via simple UTF-8 lead byte
			switch {
			case s[i]&0xE0 == 0xC0:
				size = 2
			case s[i]&0xF0 == 0xE0:
				size = 3
			case s[i]&0xF8 == 0xF0:
				size = 4
			}
			if i+size <= len(s) {
				r = []rune(s[i : i+size])[0]
			}
		}
		w := 1
		if r >= 0x1100 && (r <= 0x115F || r == 0x2329 || r == 0x232A ||
			(r >= 0x2E80 && r <= 0xA4CF) || (r >= 0xAC00 && r <= 0xD7A3) ||
			(r >= 0xF900 && r <= 0xFAFF) || (r >= 0xFE30 && r <= 0xFE6F) ||
			(r >= 0xFF00 && r <= 0xFF60) || (r >= 0xFFE0 && r <= 0xFFE6) ||
			(r >= 0x1F300 && r <= 0x1F64F)) {
			w = 2
		}
		if cells+w > width-1 { // reserve one cell for the ellipsis
			truncated = true
			break
		}
		b.WriteRune(r)
		cells += w
		i += size
	}
	if truncated {
		b.WriteString("…")
	}
	return b.String()
}

// --- syntax highlighting (Chroma) -------------------------------------------
// The focused diff window is highlighted with Chroma (lexer guessed from the
// file extension) and cached per (file, window) so scrolling stays cheap.
// NO_COLOR disables highlighting entirely.

var (
	chromaCache  sync.Map
	chromaFormRe = regexp.MustCompile(`\x1b\[38;2;\d+;\d+;\d+m|\x1b\[0m`)
)

// HighlightedCodeLines returns the code texts with syntax-highlight ANSI
// applied (one output line per input line). Falls back to the input when
// highlighting is unavailable or disabled.
func HighlightedCodeLines(path string, texts []string, plain bool) []string {
	if plain || len(texts) == 0 {
		return texts
	}
	ext := strings.TrimPrefix(filepath.Ext(path), ".")
	switch ext {
	case "go", "yaml", "yml", "json", "sh", "bash", "tf", "hcl", "md", "toml", "proto":
	default:
		return texts // unsupported extension: plain
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%v", path, texts)))
	key := hex.EncodeToString(sum[:8])
	if v, ok := chromaCache.Load(key); ok {
		return v.([]string)
	}
	lexer := chromalexers.Get(ext)
	if lexer == nil {
		lexer = chromalexers.Fallback
	}
	formatter := chromaformatters.Get("terminal256")
	if formatter == nil {
		return texts
	}
	style := chromastyles.Get("native")
	if os.Getenv("NO_COLOR") != "" {
		return texts
	}
	src := strings.Join(texts, "\n")
	it, err := lexer.Tokenise(nil, src)
	if err != nil {
		return texts
	}
	var sb strings.Builder
	if err := formatter.Format(&sb, style, it); err != nil {
		return texts
	}
	out := strings.Split(strings.TrimRight(sb.String(), "\n"), "\n")
	if len(out) != len(texts) {
		return texts // token stream reflowed: keep plain (rare)
	}
	chromaCache.Store(key, out)
	return out
}
