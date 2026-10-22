//go:build darwin

package store

import (
	"errors"
	"os"
	"syscall"
	"time"
)

// doFlockTry implements advisory locking via flock(2). macOS does not
// support fcntl F_OFD_SETLK (the file-description locks the Linux
// variant uses), so we fall back to flock with LOCK_NB + retry. flock
// is closed on any fd close in the process, so it self-cleans if a
// process crashes mid-write — but the lockfile remains; cleanup is
// the operator's job (it's fine: flock ignores the file's contents).
func doFlockTry(f *os.File, d time.Duration) error {
	deadline := time.Now().Add(d)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			return err
		}
		if time.Now().After(deadline) {
			return errors.New("timeout waiting for lock (5s)")
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func doFlockUnlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
