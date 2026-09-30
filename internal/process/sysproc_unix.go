//go:build !windows

package process

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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

// sameProgram reports whether pid is alive and runs the given binary. It is
// conservative: without /proc it says no, so a reused PID is never killed.
func sameProgram(pid int, bin string) bool {
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil || len(data) == 0 {
		return false
	}
	first := strings.SplitN(string(data), "\x00", 2)[0]
	return filepath.Base(first) == filepath.Base(bin)
}
