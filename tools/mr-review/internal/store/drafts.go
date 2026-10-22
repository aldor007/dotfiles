// drafts.go holds human-written comment drafts (comments.jsonl) — comments
// created by the reviewer independent of any AI finding. Security notes
// mirror store.go: paths are validated against traversal, nothing from the
// file is ever executed, and writes are atomic.
package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DraftComment states.
const (
	CommentDraft = "draft" // held back, never offered for publication
	CommentReady = "ready" // included in the publication batch (still confirmed)
)

// DraftComment is one human-authored comment for the MR.
//
// Inline comments carry File+Side+Line(+LineEnd for ranges); general MR
// comments carry General=true and no location. Origin is always "human":
// these are never AI-generated. Publication records the remote write after
// an explicit publish run.
type DraftComment struct {
	ID          string            `json:"id"`
	File        string            `json:"file,omitempty"`
	Side        string            `json:"side,omitempty"` // new | old
	Line        int               `json:"line,omitempty"`
	LineEnd     int               `json:"line_end,omitempty"`
	General     bool              `json:"general,omitempty"`
	Text        string            `json:"text"`
	Origin      string            `json:"origin"`
	State       string            `json:"state"` // ready | draft
	Publication *Publication      `json:"publication,omitempty"`
	CreatedAt   string            `json:"created_at"`
	UpdatedAt   string            `json:"updated_at"`
	Extra       map[string]string `json:"extra,omitempty"` // e.g. tuicr_comment_id
}

// Validate checks a draft comment against the schema (fail-closed).
func (c *DraftComment) Validate() error {
	switch {
	case strings.TrimSpace(c.ID) == "":
		return errors.New("missing id")
	case strings.TrimSpace(c.Text) == "":
		return errors.New("missing text")
	}
	if !c.General {
		switch {
		case strings.TrimSpace(c.File) == "":
			return errors.New("missing file")
		case reAbsPath.MatchString(c.File), reBackslash.MatchString(c.File),
			reDotSeg.MatchString(c.File):
			return fmt.Errorf("unsafe file path %q", c.File)
		case c.Line < 1:
			return fmt.Errorf("bad line %d", c.Line)
		}
		if c.LineEnd < c.Line {
			c.LineEnd = c.Line
		}
		switch c.Side {
		case "":
			c.Side = "new"
		case "new", "old":
		default:
			return fmt.Errorf("bad side %q", c.Side)
		}
	}
	switch c.State {
	case "":
		c.State = CommentReady
	case CommentDraft, CommentReady:
	default:
		return fmt.Errorf("bad state %q", c.State)
	}
	if c.Origin == "" {
		c.Origin = "human"
	}
	return nil
}

// Location renders "file:line" or "file:line-end" ("" for general comments).
func (c *DraftComment) Location() string {
	if c.General {
		return ""
	}
	if c.LineEnd > c.Line {
		return fmt.Sprintf("%s:%d-%d", c.File, c.Line, c.LineEnd)
	}
	return fmt.Sprintf("%s:%d", c.File, c.Line)
}

// CommentsPath returns the comments.jsonl path for a review dir.
func CommentsPath(dir string) string { return filepath.Join(dir, "comments.jsonl") }

// LoadComments reads comments.jsonl and re-validates every line.
func LoadComments(dir string) ([]DraftComment, error) {
	f, err := os.Open(CommentsPath(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []DraftComment
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1024*1024), 8*1024*1024)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var c DraftComment
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			return nil, fmt.Errorf("comments.jsonl:%d: %w", n, err)
		}
		if err := c.Validate(); err != nil {
			return nil, fmt.Errorf("comments.jsonl:%d: %w", n, err)
		}
		out = append(out, c)
	}
	return out, sc.Err()
}

// SaveComments writes comments.jsonl atomically.
func SaveComments(dir string, cs []DraftComment) error {
	var b strings.Builder
	for _, c := range cs {
		if err := c.Validate(); err != nil {
			return fmt.Errorf("comment %s: %w", c.ID, err)
		}
		j, err := json.Marshal(c)
		if err != nil {
			return err
		}
		b.Write(j)
		b.WriteByte('\n')
	}
	return atomicWrite(CommentsPath(dir), []byte(b.String()))
}

// NextCommentID returns the next free draft-comment ID (C001, C002, …).
func NextCommentID(cs []DraftComment) string {
	max := 0
	for _, c := range cs {
		var n int
		if _, err := fmt.Sscanf(c.ID, "C%d", &n); err == nil && n > max {
			max = n
		}
	}
	return fmt.Sprintf("C%03d", max+1)
}

// NewDraftComment builds a validated draft (state=ready, origin=human).
func NewDraftComment(id, file, side string, line, lineEnd int, general bool, text string) DraftComment {
	now := time.Now().UTC().Format(time.RFC3339)
	c := DraftComment{
		ID: id, File: file, Side: side, Line: line, LineEnd: lineEnd,
		General: general, Text: text, Origin: "human", State: CommentReady,
		CreatedAt: now, UpdatedAt: now,
	}
	_ = c.Validate() // normalize side/lineEnd; callers own hard errors
	return c
}
