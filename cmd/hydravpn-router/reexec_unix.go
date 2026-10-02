//go:build !windows

package main

import (
	"os"
	"syscall"
)

// reexec replaces this process with a fresh copy of its (updated) binary.
// The PID stays the same, so procd, systemd and the Entware rc script keep
// tracking the service; stdout/stderr (the procd log pipe) are inherited.
func reexec(bin string) error {
	return syscall.Exec(bin, os.Args, os.Environ())
}
