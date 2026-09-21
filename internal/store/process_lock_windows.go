//go:build windows

package store

import (
	"errors"

	"golang.org/x/sys/windows"
)

func lockProcessFile(file interface{ Fd() uintptr }) error {
	overlapped := new(windows.Overlapped)
	return windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, overlapped)
}

func unlockProcessFile(file interface{ Fd() uintptr }) error {
	overlapped := new(windows.Overlapped)
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, overlapped)
}

func isProcessLockBusy(err error) bool {
	return errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}
