//go:build !darwin

package store

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// doFlockTry implements advisory locking via fcntl(F_OFD_SETLK), the
// Linux/POSIX-2008 variant that operates on the file description (not
// just the open file table entry) — so the lock survives fork/dup and
// is released automatically on process exit (no leakable lockfile
// when a process crashes mid-write).
func doFlockTry(f *os.File, d time.Duration) error {
	deadline := time.Now().Add(d)
	// F_OFD_SETLK / F_OFD_SETLKW are open-file-description locks — they
	// are the modern Linux fcntl() command and the only variant that
	// gives us clean semantics without per-process flock(2) weirdness.
	for {
		lk := &unix.Flock_t{
			Type:   unix.F_WRLCK,
			Whence: int16(unix.SEEK_SET),
			Start:  0,
			Len:    0,
		}
		err := unix.FcntlFlock(f.Fd(), unix.F_OFD_SETLK, lk)
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
	lk := &unix.Flock_t{
		Type:   unix.F_UNLCK,
		Whence: int16(unix.SEEK_SET),
		Start:  0,
		Len:    0,
	}
	return unix.FcntlFlock(f.Fd(), unix.F_OFD_SETLK, lk)
}
