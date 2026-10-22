// exportdlg.go — vi-SQL-style export dialog. Export is a purely local,
// read-only operation on review state: it can never publish, never change
// decisions and never touches GitLab.
package tui

import (
	"fmt"
	"strings"

	"mr-review/internal/export"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

var scopeChoices = []struct {
	label string
	scope export.Scope
}{
	{"All findings", export.ScopeAll},
	{"Approved findings", export.ScopeApproved},
	{"Pending findings", export.ScopePending},
	{"Rejected findings", export.ScopeRejected},
	{"Published comments", export.ScopePublished},
	{"Stale findings", export.ScopeStale},
	{"Selected finding", export.ScopeSelected},
	{"Manual comments only", export.ScopeManual},
	{"Full review report", export.ScopeReport},
}

// exportDialog holds the export form state.
type exportDialog struct {
	field        int // 0 scope · 1 format · 2 destination · 3..6 checkboxes
	scopeIdx     int
	fmtIdx       int // 0 json · 1 markdown
	dest         textinput.Model
	destDirty    bool
	incEvidence  bool
	incComments  bool
	incRejected  bool
	incProgress  bool
	armOverwrite bool
	running      bool
}

// openExport builds the dialog with sane defaults.
func (m *model) openExport() tea.Cmd {
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 200
	d := &exportDialog{dest: ti, incEvidence: true, incComments: true}
	d.dest.SetValue(m.defaultExportName())
	m.exportDlg = d
	return nil
}

func (m *model) defaultExportName() string {
	return export.DefaultFilename(m.rev, m.exportDlgFormat(), m.exportDlgScope())
}

func (m *model) exportDlgScope() export.Scope {
	if m.exportDlg == nil {
		return export.ScopeAll
	}
	return scopeChoices[m.exportDlg.scopeIdx].scope
}

func (m *model) exportDlgFormat() string {
	if m.exportDlg == nil || m.exportDlg.fmtIdx == 0 {
		return export.FormatJSON
	}
	return export.FormatMarkdown
}

const exportFields = 7 // scope, format, dest, 4 checkboxes

func (d *exportDialog) move(delta int) {
	d.field = (d.field + delta + exportFields) % exportFields
	d.dest.Blur()
	if d.field == 2 {
		d.dest.Focus()
	}
}

func (m *model) updateExportDialog(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	d := m.exportDlg
	switch msg.String() {
	case "esc":
		m.exportDlg = nil
		m.msg = "export cancelled — nothing written"
		return m, nil
	case "ctrl+s":
		return m, m.runExport()
	case "tab", "down", "j":
		d.move(1)
		return m, nil
	case "shift+tab", "up", "k":
		d.move(-1)
		return m, nil
	case "space":
		switch d.field {
		case 3:
			d.incEvidence = !d.incEvidence
		case 4:
			d.incComments = !d.incComments
		case 5:
			d.incRejected = !d.incRejected
		case 6:
			d.incProgress = !d.incProgress
		}
		return m, nil
	case "right", "l", "enter":
		switch d.field {
		case 0:
			d.scopeIdx = (d.scopeIdx + 1) % len(scopeChoices)
			if !d.destDirty {
				d.dest.SetValue(m.defaultExportName())
			}
		case 1:
			d.fmtIdx = (d.fmtIdx + 1) % 2
			if !d.destDirty {
				d.dest.SetValue(m.defaultExportName())
			}
		case 2:
			return m, m.runExport()
		case 3:
			d.incEvidence = !d.incEvidence
		case 4:
			d.incComments = !d.incComments
		case 5:
			d.incRejected = !d.incRejected
		case 6:
			d.incProgress = !d.incProgress
		}
		return m, nil
	case "left", "h":
		switch d.field {
		case 0:
			d.scopeIdx = (d.scopeIdx - 1 + len(scopeChoices)) % len(scopeChoices)
			if !d.destDirty {
				d.dest.SetValue(m.defaultExportName())
			}
		case 1:
			d.fmtIdx = (d.fmtIdx + 1) % 2
		}
		return m, nil
	}
	if d.field == 2 { // typing the destination path
		var cmd tea.Cmd
		before := d.dest.Value()
		d.dest, cmd = d.dest.Update(msg)
		if d.dest.Value() != before {
			d.destDirty = true
			d.armOverwrite = false
		}
		return m, cmd
	}
	return m, nil
}

// runExport performs the export (local file write only).
func (m *model) runExport() tea.Cmd {
	d := m.exportDlg
	if d == nil {
		return nil
	}
	opts := export.Options{
		Dir:             m.dir,
		Scope:           m.exportDlgScope(),
		Format:          m.exportDlgFormat(),
		Out:             strings.TrimSpace(d.dest.Value()),
		IncludeEvidence: d.incEvidence,
		IncludeComments: d.incComments,
		IncludeRejected: d.incRejected,
		IncludeProgress: d.incProgress,
	}
	if opts.Scope == export.ScopeSelected {
		f := m.current()
		if f == nil {
			m.msg = "no finding selected"
			return nil
		}
		opts.FindingID = f.ID
	}
	res, err := export.Run(opts)
	if err != nil {
		if strings.Contains(err.Error(), "refusing to overwrite") {
			d.armOverwrite = true
		}
		m.msg = "export failed: " + err.Error()
		return nil
	}
	m.exportDlg = nil
	if res.Path == "" {
		m.msg = fmt.Sprintf("exported %d finding(s), %d comment(s) to stdout", res.Findings, res.Comments)
	} else {
		m.msg = fmt.Sprintf("exported %d finding(s), %d comment(s) → %s", res.Findings, res.Comments, res.Path)
	}
	return nil
}

// exportDialogView renders the overlay.
func (m *model) exportDialogView(w, h int) string {
	d := m.exportDlg
	var b strings.Builder
	b.WriteString(styleHeader.Render("EXPORT REVIEW DATA") + "\n\n")

	row := func(idx int, label, value string, selected bool) string {
		cursor := "  "
		if selected {
			cursor = "> "
		}
		return fmt.Sprintf("%s%-13s %s", cursor, label, value)
	}
	b.WriteString(row(0, "Scope:", scopeChoices[d.scopeIdx].label, d.field == 0) + "\n")
	fmtStr := "JSON (versioned schema)"
	if d.fmtIdx == 1 {
		fmtStr = "Markdown (readable report)"
	}
	b.WriteString(row(1, "Format:", fmtStr, d.field == 1) + "\n")
	b.WriteString(row(2, "Destination:", truncPlain(d.dest.View(), w-28), d.field == 2) + "\n")
	b.WriteString("\n")
	check := func(idx int, label string, on bool) string {
		mark := "[ ]"
		if on {
			mark = "[x]"
		}
		return row(idx, mark, label, d.field == idx)
	}
	b.WriteString(check(3, "Include evidence", d.incEvidence) + "\n")
	b.WriteString(check(4, "Include manual comments", d.incComments) + "\n")
	b.WriteString(check(5, "Include rejected findings", d.incRejected) + "\n")
	b.WriteString(check(6, "Include review progress", d.incProgress) + "\n")
	b.WriteString("\n")
	if d.armOverwrite {
		b.WriteString(styleWarn.Render("  file exists — change the destination or confirm again") + "\n")
	}
	b.WriteString(styleDim.Render("  ↑/↓ move · enter/left-right change · space toggle · ctrl+s export · esc cancel") + "\n")

	boxW := min(w-6, 72)
	boxH := min(h-4, 14)
	return overlayBoxRaw(b.String(), boxW, boxH)
}
