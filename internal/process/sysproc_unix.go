//go:build !windows

package process

import (
	"os"
	"os/exec"
	"syscall"
)

func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminate(p *os.Process) error {
	return p.Signal(syscall.SIGTERM)
}

// killGroup kills the whole process group created by setProcessGroup.
func killGroup(p *os.Process) error {
	if err := syscall.Kill(-p.Pid, syscall.SIGKILL); err != nil {
		return p.Kill()
	}
	return nil
}

func hangup(p *os.Process) error {
	return p.Signal(syscall.SIGHUP)
}
