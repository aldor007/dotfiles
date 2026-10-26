// context command — manage the per-project AI-reviewer context pack.
//
// Every top-level *.md file under
// ~/.local/share/mr-review/<host_project>/context/ is automatically
// injected into the review prompt as PROJECT CONTEXT (architecture,
// invariants, review gotchas — the "book of <service>"). This command
// scaffolds, edits, and lists those notes; the pack is the config.
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"

	"mr-review/internal/store"

	"github.com/spf13/cobra"
)

func newContextCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context",
		Short: `manage the AI reviewer's per-project context pack ("book of <service>")`,
		Long: `The AI reviewer automatically reads every top-level *.md file in the
project's context directory as PROJECT CONTEXT: architecture, invariants,
cost/availability-sensitive areas, review gotchas. The pack lives outside
the reviewed repository (per host+project) and is reviewer-maintained.

  mr-review context new castai/kubecast                  # scaffold context.md
  mr-review context new castai/kubecast book-of-kent     # named note
  mr-review context add castai/kubecast -s gotchas "text" # append user info
  mr-review context edit ~/.local/share/mr-review/<slug> # existing review dir
  mr-review context show castai/kubecast                 # list notes
  mr-review context path castai/kubecast                 # print the directory`,
		SilenceUsage: true,
	}
	addCmd := &cobra.Command{
		Use:   "add <MR|dir|[host/]group/project> [text]",
		Short: "add info to a context note (text arg or stdin; -s picks the section)",
		Long: `Appends to a note in the pack, creating/scaffolding it if needed.
With -s the text lands as a bullet under the matching "## " heading
(case-insensitive contains-match; a missing section is created at the
end). Without text, the note text is read from stdin — handy from herdr:

  echo "kent restarts lose streaming state" | mr-review context add castai/kubecast -s gotchas
  mr-review context add castai/kubecast -f book-of-kent -s invariants "plans are resumable"`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			host, project, err := resolveProjectArg(args[0])
			if err != nil {
				return err
			}
			text := ""
			if len(args) == 2 {
				text = args[1]
			} else {
				b, rerr := io.ReadAll(os.Stdin)
				if rerr != nil {
					return fmt.Errorf("read stdin: %w", rerr)
				}
				text = string(b)
			}
			name, _ := cmd.Flags().GetString("file")
			section, _ := cmd.Flags().GetString("section")
			path, err := addContextNote(host, project, name, section, text)
			if err != nil {
				return err
			}
			fmt.Printf("added to %s — it rides along on every review of %s/%s\n", path, host, project)
			return nil
		},
	}
	addCmd.Flags().StringP("file", "f", "context", "note inside the pack (without .md)")
	addCmd.Flags().StringP("section", "s", "", "append under this section heading (created if missing)")
	cmd.AddCommand(addCmd,
		&cobra.Command{
			Use:   "new <MR|dir|[host/]group/project> [name]",
			Short: "scaffold a context note (never overwrites)",
			Args:  cobra.RangeArgs(1, 2),
			RunE: func(cmd *cobra.Command, args []string) error {
				host, project, err := resolveProjectArg(args[0])
				if err != nil {
					return err
				}
				name := "context"
				if len(args) == 2 {
					name = args[1]
				}
				path, created, err := scaffoldContext(host, project, name)
				if err != nil {
					return err
				}
				if created {
					fmt.Printf("created %s — it is now part of every review of %s/%s\n", path, host, project)
				} else {
					fmt.Printf("already exists (left untouched): %s\n", path)
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "edit <MR|dir|[host/]group/project> [name]",
			Short: "scaffold if missing, then open in $EDITOR",
			Args:  cobra.RangeArgs(1, 2),
			RunE: func(cmd *cobra.Command, args []string) error {
				host, project, err := resolveProjectArg(args[0])
				if err != nil {
					return err
				}
				name := "context"
				if len(args) == 2 {
					name = args[1]
				}
				path, _, err := scaffoldContext(host, project, name)
				if err != nil {
					return err
				}
				editor := os.Getenv("EDITOR")
				if editor == "" {
					editor = "vi"
				}
				c := exec.Command(editor, path)
				c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
				return c.Run()
			},
		},
		&cobra.Command{
			Use:   "show <MR|dir|[host/]group/project>",
			Short: "list the project's context notes (what the reviewer will read)",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				host, project, err := resolveProjectArg(args[0])
				if err != nil {
					return err
				}
				files := store.ContextFiles(host, project)
				if len(files) == 0 {
					fmt.Printf("no context notes for %s/%s yet — create one with: mr-review context new %s\n",
						host, project, project)
					return nil
				}
				fmt.Printf("context pack for %s/%s (injected into every review):\n", host, project)
				for _, f := range files {
					fmt.Printf("  %s (%d lines)\n", f, countContextLines(f))
				}
				return nil
			},
		},
		&cobra.Command{
			Use:   "path <MR|dir|[host/]group/project>",
			Short: "print the project's context directory",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				host, project, err := resolveProjectArg(args[0])
				if err != nil {
					return err
				}
				fmt.Println(store.ContextDir(host, project))
				return nil
			},
		},
	)
	return cmd
}

// resolveProjectArg accepts a review dir (review.json), any MR ref
// (URL/IID via resolveRef), or a bare "[host/]group/project".
func resolveProjectArg(arg string) (host, project string, err error) {
	if st, serr := os.Stat(arg); serr == nil && st.IsDir() {
		rev, lerr := store.LoadReview(arg)
		if lerr != nil {
			return "", "", fmt.Errorf("%s is not a review dir (no valid review.json)", arg)
		}
		return rev.Host, rev.Project, nil
	}
	if ref, _, rerr := resolveRef(arg); rerr == nil {
		return ref.Host, ref.Project, nil
	}
	// bare project form: "[host/]group/project" — host segment is anything
	// containing a dot (e.g. gitlab.com); everything else joins the project.
	parts := strings.Split(arg, "/")
	if len(parts) >= 3 && strings.Contains(parts[0], ".") {
		return parts[0], strings.Join(parts[1:], "/"), nil
	}
	if len(parts) == 2 {
		return store.DefaultHost, arg, nil
	}
	return "", "", fmt.Errorf("cannot resolve %q as a review dir, MR ref, or [host/]group/project", arg)
}

// scaffoldContext creates the context note if missing and returns its path
// plus whether it was created (existing files are never overwritten).
func scaffoldContext(host, project, name string) (string, bool, error) {
	name = strings.TrimSuffix(filepath.Base(name), ".md") // no traversal, no dup suffix
	cdir := store.ContextDir(host, project)
	if err := os.MkdirAll(cdir, 0o755); err != nil {
		return "", false, fmt.Errorf("create context dir: %w", err)
	}
	path := filepath.Join(cdir, name+".md")
	if _, err := os.Stat(path); err == nil {
		return path, false, nil
	}
	title := strings.NewReplacer("-", " ", "_", " ").Replace(name)
	if title != "" {
		r := []rune(title)
		r[0] = unicode.ToUpper(r[0])
		title = string(r)
	}
	body := fmt.Sprintf(contextTemplate, title, project)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return "", false, fmt.Errorf("write %s: %w", path, err)
	}
	return path, true, nil
}

func countContextLines(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return strings.Count(string(b), "\n")
}

// addContextNote appends user-provided text to a note in the project's
// context pack, scaffolding the note first if missing. With a section, the
// text becomes a bullet under the first "## " heading that contains the
// section name (case-insensitive); a missing section is created at the end.
func addContextNote(host, project, name, section, text string) (string, error) {
	path, _, err := scaffoldContext(host, project, name)
	if err != nil {
		return "", err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return path, fmt.Errorf("nothing to add (empty text)")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	var updated string
	if section == "" {
		updated = strings.TrimRight(string(body), "\n") + "\n\n" + text + "\n"
	} else {
		updated, err = insertUnderSection(string(body), section, text)
		if err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return path, nil
}

// insertUnderSection appends text as a bullet under the matching section
// heading, or creates the section at the end of the file when absent.
func insertUnderSection(content, section, text string) (string, error) {
	lines := strings.Split(content, "\n")
	want := strings.ToLower(section)
	idx := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "## ") && strings.Contains(strings.ToLower(l), want) {
			idx = i
			break
		}
	}
	bullet := bulletize(text)
	if idx < 0 {
		// new section at the end
		out := strings.TrimRight(content, "\n") + "\n\n## " + section + "\n\n" + bullet + "\n"
		return out, nil
	}
	// end of this section = next heading, else EOF; back up over trailing blanks
	end := len(lines)
	for k := idx + 1; k < len(lines); k++ {
		if strings.HasPrefix(lines[k], "## ") {
			end = k
			break
		}
	}
	for end > idx+1 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	out := append([]string{}, lines[:end]...)
	out = append(out, strings.Split(bullet, "\n")...)
	out = append(out, "")
	out = append(out, lines[end:]...)
	return strings.Join(out, "\n"), nil
}

// bulletize renders the first text line as a list bullet, indenting any
// continuation lines so the whole block stays inside the bullet.
func bulletize(text string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	lines[0] = "- " + lines[0]
	for i := 1; i < len(lines); i++ {
		if lines[i] != "" {
			lines[i] = "  " + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}

// contextTemplate is the skeleton a new note starts from. Sections mirror
// how the review prompt uses the pack: background first, then the
// reviewer's hard-won specifics.
const contextTemplate = `# %s — reviewer context for %s

> Personal reviewer notes, maintained by the human reviewer. Read before
> reviewing changes in this project. May lag the code — the checkout wins.

## Authoritative in-repo docs (read these first)

- (list the READMEs/ADRs a reviewer should read, e.g. services/<name>/README.md)

## Purpose (what this project is, in two sentences)

## Invariants (fill in as you learn)

## Sensitive areas (changes here are risky — say why)

## Review gotchas learned the hard way
`
