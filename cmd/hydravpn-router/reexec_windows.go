//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// reexec starts the updated binary with the same arguments and lets this
// process exit. The new process breaks away from the job object that kills
// this process's children (process.KillChildrenOnExit).
func reexec(bin string) error {
	cmd := exec.Command(bin, os.Args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_BREAKAWAY_FROM_JOB | windows.CREATE_NEW_PROCESS_GROUP,
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}
