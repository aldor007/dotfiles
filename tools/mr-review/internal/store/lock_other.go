//go:build !darwin

package store

import (
	"errors"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

var (
	fsetlkGuardMu sync.Mutex
	fsetlkHeldFDs sync.Map // fd (uintptr) -> struct{}
)

// doFlockTry implements advisory locking using Linux's open-file-description
// locks (fcntl(F_OFD_SETLK)), which serialize the same path across both
// processes and goroutines in the same process. F_SETLK is kept as a fallback
// for kernels or environments where OFD locks are not available.
func doFlockTry(f *os.File, d time.Duration) error {
	deadline := time.Now().Add(d)
	for {
		err := tryFlock(f, unix.F_OFD_SETLK, unix.F_WRLCK)
		if err == nil {
			return nil
		}
		if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOTSUP) {
			return doFSetLKTry(f, deadline)
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
	if _, ok := fsetlkHeldFDs.LoadAndDelete(f.Fd()); ok {
		defer fsetlkGuardMu.Unlock()
		return tryFlock(f, unix.F_SETLK, unix.F_UNLCK)
	}
	if err := tryFlock(f, unix.F_OFD_SETLK, unix.F_UNLCK); err != nil &&
		!errors.Is(err, unix.ENOSYS) && !errors.Is(err, unix.EINVAL) && !errors.Is(err, unix.ENOTSUP) {
		return err
	}
	return nil
}

func doFSetLKTry(f *os.File, deadline time.Time) error {
	fsetlkGuardMu.Lock()
	held := false
	defer func() {
		if !held {
			fsetlkGuardMu.Unlock()
		}
	}()
	for {
		err := tryFlock(f, unix.F_SETLK, unix.F_WRLCK)
		if err == nil {
			fsetlkHeldFDs.Store(f.Fd(), struct{}{})
			held = true
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

func tryFlock(f *os.File, op, typ int) error {
	return unix.FcntlFlock(f.Fd(), op, &unix.Flock_t{
		Type:   int16(typ),
		Whence: 0, // SEEK_SET
		Start:  0,
		Len:    0,
	})
}
