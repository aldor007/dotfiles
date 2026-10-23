//go:build !darwin

package store

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// doFlockTry implements advisory locking using fcntl(F_SETLK), which is the
// portable Linux locking primitive available in the x/sys/unix package.
// The open-file-description variant is not available here, but this still
// serializes access to the review state file across processes without
// relying on a platform-specific syscall fallback.
func doFlockTry(f *os.File, d time.Duration) error {
	deadline := time.Now().Add(d)
	for {
		err := unix.FcntlFlock(f.Fd(), unix.F_SETLK, &unix.Flock_t{
			Type:   unix.F_WRLCK,
			Whence: 0, // SEEK_SET
			Start:  0,
			Len:    0,
		})
		if err == nil {
			return nil
		}
		if !errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.EACCES) {
			return err
		}
		if time.Now().After(deadline) {
			return errors.New("timeout waiting for lock (5s)")
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func doFlockUnlock(f *os.File) error {
	return unix.FcntlFlock(f.Fd(), unix.F_SETLK, &unix.Flock_t{
		Type:   unix.F_UNLCK,
		Whence: 0,
		Start:  0,
		Len:    0,
	})
}
