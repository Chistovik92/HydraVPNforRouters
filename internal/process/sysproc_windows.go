//go:build windows

package process

import (
	"errors"
	"os"
	"os/exec"
)

func setProcessGroup(cmd *exec.Cmd) {}

// Windows has no SIGTERM for arbitrary processes, so stopping means killing.
func terminate(p *os.Process) error {
	return p.Kill()
}

func killGroup(p *os.Process) error {
	return p.Kill()
}

func hangup(p *os.Process) error {
	return errors.New("reload signal is not supported on Windows")
}
