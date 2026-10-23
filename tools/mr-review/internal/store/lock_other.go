//go:build !darwin

package store

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// doFlockTry implements advisory locking using Linux's open-file-description
// locks (fcntl(F_OFD_SETLK)), which serialize the same path across both
// processes and goroutines in the same process. F_SETLK is kept as a fallback
// for kernels or environments where OFD locks are not available.
func doFlockTry(f *os.File, d time.Duration) error {
	deadline := time.Now().Add(d)
	for {
		if err := tryFlock(f, unix.F_OFD_SETLK, unix.F_WRLCK); err == nil {
			return nil
		} else if !errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.EACCES) &&
			!errors.Is(err, unix.ENOSYS) && !errors.Is(err, unix.EINVAL) && !errors.Is(err, unix.ENOTSUP) {
			return err
		}
		if err := tryFlock(f, unix.F_SETLK, unix.F_WRLCK); err == nil {
			return nil
		} else if !errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.EACCES) {
			return err
		}
		if time.Now().After(deadline) {
			return errors.New("timeout waiting for lock (5s)")
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func doFlockUnlock(f *os.File) error {
	if err := tryFlock(f, unix.F_OFD_SETLK, unix.F_UNLCK); err == nil ||
		errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOTSUP) {
		return tryFlock(f, unix.F_SETLK, unix.F_UNLCK)
	}
	return nil
}

func tryFlock(f *os.File, op, typ int) error {
	return unix.FcntlFlock(f.Fd(), op, &unix.Flock_t{
		Type:   int16(typ),
		Whence: 0, // SEEK_SET
		Start:  0,
		Len:    0,
	})
}
