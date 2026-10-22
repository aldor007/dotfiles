// mrselect is the interactive MR chooser: a tuicr-inspired list with styled
// rows (Nerd Font glyphs), a `/`-style filter supporting author:/age:/label:
// tokens plus free text, and vim navigation. Rendering-only state; all
// mutations are local (selection returns an IID to the caller).
package tui

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// MRRow is one selectable MR in the chooser.
type MRRow struct {
	IID          int
	Title        string
	Author       string
	SourceBranch string
	TargetBranch string
	Labels       []string
	UpdatedAt    time.Time
}

// Nerd Font glyphs (JetBrains Mono NF is installed on this machine).
const (
	glyphPR     = "\uf409" // git pull request
	glyphBranch = "\uf418" // git branch
	glyphPerson = "\uf508" // person
	glyphTag    = "\uf412" // tag
	glyphClock  = "\uf43a" // clock
)

var mrSelectStyles = struct {
	title, dim, author, age, label, cursor, header, keys lipgloss.Style
}{
	title:  lipgloss.NewStyle().Bold(true),
	dim:    lipgloss.NewStyle().Faint(true),
	author: lipgloss.NewStyle().Foreground(lipgloss.Color("117")),
	age:    lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
	label:  lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
	cursor: lipgloss.NewStyle().Reverse(true),
	header: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")),
	keys:   lipgloss.NewStyle().Faint(true),
}

type mrSelectModel struct {
	title    string
	items    []MRRow
	filtered []int
	cursor   int
	query    string
	filterOn bool
	input    textinput.Model
	sortAsc  bool
	width    int
	err      error
	quit     bool
}

// RunMRSelect shows the chooser and returns the selected IID (0 = cancelled).
func RunMRSelect(title string, items []MRRow) (int, error) {
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	ti := textinput.New()
	ti.Placeholder = "author:matas age:<7d text…   (/ to filter)"
	ti.Prompt = "/ "
	m := &mrSelectModel{title: title, items: items, input: ti}
	m.applyFilter()

	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		return 0, err
	}
	if m.quit || m.cursor < 0 || m.cursor >= len(m.filtered) {
		return 0, nil
	}
	return m.items[m.filtered[m.cursor]].IID, nil
}

func (m *mrSelectModel) Init() tea.Cmd { return textinput.Blink }

func (m *mrSelectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case tea.KeyPressMsg:
		if m.filterOn {
			return m.updateFilter(msg)
		}
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.quit = true
			return m, tea.Quit
		case "/":
			m.filterOn = true
			m.input.Focus()
			m.input.SetValue(m.query)
			return m, textinput.Blink
		case "j", "down":
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "g", "home":
			m.cursor = 0
		case "G", "end":
			m.cursor = len(m.filtered) - 1
		case "o":
			m.sortAsc = !m.sortAsc
			m.applyFilter()
		case "enter":
			if len(m.filtered) > 0 {
				return m, tea.Quit
			}
		}
		return m, nil
	}
	return m, nil
}

func (m *mrSelectModel) updateFilter(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.filterOn = false
		m.input.Blur()
		return m, nil
	case "esc":
		if m.input.Value() == "" {
			m.quit = true
			return m, tea.Quit
		}
		m.input.SetValue("")
		m.query = ""
		m.filterOn = false
		m.input.Blur()
		m.applyFilter()
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != m.query {
		m.query = m.input.Value()
		m.cursor = 0
		m.applyFilter()
	}
	return m, cmd
}

// ---- filter language (tuicr-inspired) --------------------------------------
//
// Space-separated tokens, all must match:
//   author:sub   case-insensitive substring on the author name
//   age:<7d      updated within the last 7 days (h/m/s units work too)
//   age:>2d      updated more than 2 days ago
//   label:sub    any label containing the substring
//   <text>       free text: substring of title or branch names

var reAgeToken = regexp.MustCompile(`^age:([<>])(\d+)([dhms])$`)

// MatchMRQuery reports whether a row matches the filter query.
func MatchMRQuery(r MRRow, query string) bool {
	q := strings.TrimSpace(query)
	if q == "" {
		return true
	}
	for _, tok := range strings.Fields(q) {
		lower := strings.ToLower(tok)
		switch {
		case strings.HasPrefix(lower, "author:"):
			if !strings.Contains(strings.ToLower(r.Author), strings.TrimPrefix(lower, "author:")) {
				return false
			}
		case strings.HasPrefix(lower, "label:"):
			sub := strings.TrimPrefix(lower, "label:")
			any := false
			for _, l := range r.Labels {
				if strings.Contains(strings.ToLower(l), sub) {
					any = true
					break
				}
			}
			if !any {
				return false
			}
		case reAgeToken.MatchString(lower):
			g := reAgeToken.FindStringSubmatch(lower)
			d := ageDuration(g[2], g[3])
			age := time.Since(r.UpdatedAt)
			if g[1] == "<" && age > d {
				return false
			}
			if g[1] == ">" && age < d {
				return false
			}
		default:
			hay := strings.ToLower(r.Title + " " + r.SourceBranch + " " + r.TargetBranch)
			if !strings.Contains(hay, lower) {
				return false
			}
		}
	}
	return true
}

func ageDuration(num, unit string) time.Duration {
	n, _ := strconv.Atoi(num)
	switch unit {
	case "h":
		return time.Duration(n) * time.Hour
	case "m":
		return time.Duration(n) * time.Minute
	case "s":
		return time.Duration(n) * time.Second
	default:
		return time.Duration(n) * 24 * time.Hour
	}
}

// RelAge renders a compact relative age ("3h", "2d", "5mo").
func RelAge(t time.Time, now time.Time) string {
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dmo", int(d.Hours()/24/30))
	}
}

func (m *mrSelectModel) applyFilter() {
	m.filtered = m.filtered[:0]
	for i := range m.items {
		if MatchMRQuery(m.items[i], m.query) {
			m.filtered = append(m.filtered, i)
		}
	}
	sort.SliceStable(m.filtered, func(a, b int) bool {
		x, y := m.items[m.filtered[a]], m.items[m.filtered[b]]
		if m.sortAsc {
			return x.UpdatedAt.Before(y.UpdatedAt)
		}
		return x.UpdatedAt.After(y.UpdatedAt)
	})
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// ---- view ------------------------------------------------------------------

func (m *mrSelectModel) View() tea.View {
	w := m.width
	if w <= 0 {
		w = 100
	}
	h := 24 // fixed-ish height; bubbletea sizes the alt screen anyway
	var b strings.Builder

	count := fmt.Sprintf("%d/%d", len(m.filtered), len(m.items))
	sortDir := "new→old"
	if m.sortAsc {
		sortDir = "old→new"
	}
	b.WriteString(mrSelectStyles.header.Render(fmt.Sprintf("%s %s", glyphPR, m.title)) +
		mrSelectStyles.dim.Render(fmt.Sprintf("  %s matched · %s · sort %s", count, glyphClock+" "+sortDir, "")) +
		"\n\n")

	visible := h - 4
	start := 0
	if m.cursor >= visible-1 {
		start = m.cursor - (visible - 2)
	}
	end := min(len(m.filtered), start+visible)
	for i := start; i < end; i++ {
		idx := m.filtered[i]
		line := renderMRRow(m.items[idx], w)
		if i == m.cursor {
			line = mrSelectStyles.cursor.Render(line)
		}
		b.WriteString(line + "\n")
	}
	for i := end - start; i < visible; i++ {
		b.WriteString("\n")
	}

	if m.filterOn {
		b.WriteString(mrSelectStyles.keys.Render(
			"tokens: author:sub · age:<7d · age:>2d · label:sub · free text") + "\n")
		b.WriteString(m.input.View())
	} else {
		b.WriteString(mrSelectStyles.keys.Render(
			"j/k move · o sort · / filter · enter review · q quit"))
	}
	return tea.NewView(b.String())
}

func renderMRRow(r MRRow, w int) string {
	title := r.Title
	maxTitle := w - 40
	if maxTitle < 20 {
		maxTitle = 20
	}
	if len(title) > maxTitle {
		title = title[:maxTitle-1] + "…"
	}
	right := fmt.Sprintf("%s %s  %s%s  %s%s",
		mrSelectStyles.author.Render(glyphPerson+" "+r.Author),
		"",
		mrSelectStyles.dim.Render(glyphBranch+" "+truncStr(r.SourceBranch, 20)),
		"",
		mrSelectStyles.age.Render(glyphClock+" "+RelAge(r.UpdatedAt, time.Now())),
		"")
	if len(r.Labels) > 0 {
		right += " " + mrSelectStyles.label.Render(glyphTag+" "+strings.Join(r.Labels, ","))
	}
	line := fmt.Sprintf("%s%-6d%s %s  %s", glyphPR, r.IID, "", mrSelectStyles.title.Render(title), right)
	// keep total width bounded so reverse-video rows look clean
	if lipgloss.Width(line) > w {
		line = cut(line, w)
	}
	return line
}

func truncStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
