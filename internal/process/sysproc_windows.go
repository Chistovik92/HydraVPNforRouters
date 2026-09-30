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

// sameProgram: Windows children are cleaned up by the job object (see
// KillChildrenOnExit), so there are no leftovers to identify by PID.
func sameProgram(pid int, bin string) bool { return false }
