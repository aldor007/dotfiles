package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"mr-review/internal/store"
)

// cmdStatus walks all local reviews and prints a compact report.
func cmdStatus() error {
	root := store.Home()
	projects, err := os.ReadDir(root)
	if err != nil {
		return nil // no reviews yet
	}
	type row struct {
		project, iid, title, state, counts, path string
	}
	var rows []row
	for _, p := range projects {
		if !p.IsDir() {
			continue
		}
		mrs, err := os.ReadDir(filepath.Join(root, p.Name()))
		if err != nil {
			continue
		}
		for _, m := range mrs {
			if !m.IsDir() || !strings.HasPrefix(m.Name(), "mr") {
				continue
			}
			dir := filepath.Join(root, p.Name(), m.Name())
			rev, err := store.LoadReview(dir)
			if err != nil {
				continue
			}
			fs, err := store.LoadFindings(dir)
			counts := "?"
			if err == nil {
				var ap, pe, rj, pub int
				for _, f := range fs {
					switch {
					case f.Publication != nil:
						pub++
					case f.Decision == "approved":
						ap++
					case f.Decision == "rejected":
						rj++
					default:
						pe++
					}
				}
				counts = statusCounts(ap, pe, rj, pub, len(fs))
			}
			rows = append(rows, row{
				project: rev.Project, iid: "!" + itoa(rev.IID), title: trunc(rev.Title, 50),
				state: rev.State, counts: counts, path: dir,
			})
		}
	}
	if len(rows) == 0 {
		fmt.Println("no local reviews")
		return nil
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].project != rows[j].project {
			return rows[i].project < rows[j].project
		}
		return rows[i].iid < rows[j].iid
	})
	for _, r := range rows {
		fmt.Printf("%-40s %-6s %-9s %-9s %s\n  %s\n", r.project, r.iid, r.state, r.counts, r.title, r.path)
	}
	return nil
}

func statusCounts(approved, pending, rejected, published, total int) string {
	return itoa(approved) + "ok " + itoa(pending) + "pend " + itoa(rejected) + "rej " + itoa(published) + "pub /" + itoa(total)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
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
