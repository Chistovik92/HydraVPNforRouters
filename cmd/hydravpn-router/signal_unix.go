//go:build !windows

package main

import "syscall"

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func terminateProcess(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) }

func reloadProcess(pid int) error { return syscall.Kill(pid, syscall.SIGHUP) }
