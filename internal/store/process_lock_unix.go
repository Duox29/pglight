//go:build !windows

package store

import (
	"errors"
	"syscall"

	"golang.org/x/sys/unix"
)

func lockProcessFile(file interface{ Fd() uintptr }) error {
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err != nil {
		return err
	}
	return nil
}

func unlockProcessFile(file interface{ Fd() uintptr }) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}

func isProcessLockBusy(err error) bool {
	return errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN)
}
