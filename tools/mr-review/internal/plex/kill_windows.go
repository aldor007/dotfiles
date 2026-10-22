//go:build windows

package plex

import "os"

// syscallKillGroup falls back to killing the direct child on Windows.
func syscallKillGroup(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}
