//go:build !windows

package plex

import "syscall"

// syscallKillGroup kills the whole process group (children included).
func syscallKillGroup(pid int) error {
	return syscall.Kill(-pid, syscall.SIGKILL)
}
