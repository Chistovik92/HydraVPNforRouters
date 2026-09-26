//go:build windows

package main

import (
	"errors"
	"os"
	"syscall"
)

func processAlive(pid int) bool {
	const processQueryLimitedInformation = 0x1000
	const stillActive = 259
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}

func terminateProcess(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}

func reloadProcess(pid int) error {
	return errors.New("reload is not supported on Windows; restart the service instead")
}
