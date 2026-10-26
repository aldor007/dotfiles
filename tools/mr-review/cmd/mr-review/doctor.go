package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"mr-review/internal/store"
)

// cmdDoctor verifies the toolchain the workflow depends on.
func cmdDoctor() error {
	type check struct {
		name string
		fn   func() (string, error)
	}
	shell := func(name string, args ...string) func() (string, error) {
		return func() (string, error) {
			out, err := exec.Command(name, args...).CombinedOutput()
			return strings.TrimSpace(string(out)), err
		}
	}
	glabBin := os.Getenv("MR_REVIEW_GLAB")
	if glabBin == "" {
		glabBin = "glab"
	}
	agentBin := os.Getenv("MR_REVIEW_AGENT")
	if agentBin == "" {
		agentBin = "pi"
	}
	checks := []check{
		{"git", shell("git", "--version")},
		{glabBin, shell(glabBin, "--version")},
		{glabBin + " auth", shell(glabBin, "auth", "status")},
		{agentBin + " (headless -p)", func() (string, error) {
			out, err := exec.Command(agentBin, "--version").CombinedOutput()
			return strings.TrimSpace(string(out)), err
		}},
		{"nvim", shell("nvim", "--version")},
		{"herdr (optional)", shell("herdr", "--version")},
		{"state dir writable", func() (string, error) {
			p := store.Home()
			if err := os.MkdirAll(p, 0o700); err != nil {
				return "", err
			}
			probe := filepath.Join(p, ".probe")
			if err := os.WriteFile(probe, []byte("x"), 0o600); err != nil {
				return "", err
			}
			_ = os.Remove(probe)
			return p, nil
		}},
	}
	failed := 0
	for _, c := range checks {
		out, err := c.fn()
		status := "ok"
		if err != nil {
			status = "FAIL"
			failed++
		}
		short := out
		if i := strings.IndexByte(short, '\n'); i > 0 {
			short = short[:i]
		}
		if len(short) > 60 {
			short = short[:57] + "..."
		}
		fmt.Printf("%-24s %-5s %s\n", c.name, status, short)
	}
	if failed > 0 {
		return fmt.Errorf("%d check(s) failed", failed)
	}
	fmt.Println("\nall checks passed")
	return nil
}
