//go:build windows
// +build windows

package byedpi

import (
	"os/exec"
)

func setProcessGroup(cmd *exec.Cmd) {
	// No-op on Windows
}