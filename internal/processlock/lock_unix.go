//go:build !windows

package processlock

import (
	"errors"
	"os"
	"syscall"
)

var errLockBusy = syscall.EWOULDBLOCK

func lockFile(file *os.File) error {
	err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EAGAIN) {
		return errLockBusy
	}
	return err
}

func unlockFile(file *os.File) error { return syscall.Flock(int(file.Fd()), syscall.LOCK_UN) }
