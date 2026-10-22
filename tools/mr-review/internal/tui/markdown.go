package tui

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
)

// Markdown rendering (Glamour) for the prose tabs: Explanation, Comment,
// Discussion. Rendered output is cached per (width, content) — glamour is
// too expensive to run on every keystroke/render.

var (
	mdCache     sync.Map // key -> rendered string
	mdOnce      sync.Map // per-width renderer singleton
	mdMu        sync.Mutex
	mdRenderers = map[int]*glamour.TermRenderer{}
)

func mdRenderer(w int) *glamour.TermRenderer {
	mdMu.Lock()
	defer mdMu.Unlock()
	if r, ok := mdRenderers[w]; ok {
		return r
	}
	opts := []glamour.TermRendererOption{
		glamour.WithWordWrap(max(40, w-2)),
		glamour.WithStandardStyle(mdStyleName()),
	}
	r, err := glamour.NewTermRenderer(opts...)
	if err != nil {
		r, _ = glamour.NewTermRenderer() // last resort: defaults
	}
	mdRenderers[w] = r
	return r
}

// mdStyleName picks the glamour style WITHOUT querying the terminal.
// glamour.WithAutoStyle() (the old behavior) sends an OSC 11 background
// query at first render — the async response raced Bubble Tea's input
// reader and leaked `rgb:0000/0000/0000`-style bytes into focused inputs
// (the command palette showed “0 actions” with garbage in the filter).
// The theme already encodes dark/light intent, so derive the style from it.
func mdStyleName() string {
	if plainMode() {
		return "ascii"
	}
	if currentTheme.Name == "dracula" {
		return "dracula"
	}
	return "dark"
}

// mdReset drops cached renderers and rendered content (theme switch
// restyles markdown lazily on the next render).
func mdReset() {
	mdMu.Lock()
	defer mdMu.Unlock()
	mdCache = sync.Map{}
	mdRenderers = map[int]*glamour.TermRenderer{}
}

func mdKey(w int, content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:8]) + ":" + strconv.Itoa(w)
}

// renderMarkdown renders content as terminal markdown at the given width.
func renderMarkdown(content string, w int) string {
	content = strings.TrimRight(content, "\n")
	if content == "" {
		return ""
	}
	key := mdKey(w, content)
	if v, ok := mdCache.Load(key); ok {
		return v.(string)
	}
	out, err := mdRenderer(w).Render(content)
	if err != nil {
		return content // degrade to raw text, never fail the view
	}
	out = strings.TrimRight(out, "\n")
	mdCache.Store(key, out)
	return out
}

// strings_TrimRight avoids importing strings twice in call sites.
func strings_TrimRight(s, cutset string) string {
	for len(s) > 0 && strings_HasSuffixByte(s, cutset) {
		s = s[:len(s)-1]
	}
	return s
}

func strings_HasSuffixByte(s, cutset string) bool {
	if len(s) == 0 {
		return false
	}
	last := s[len(s)-1]
	for i := 0; i < len(cutset); i++ {
		if cutset[i] == last {
			return true
		}
	}
	return false
}
