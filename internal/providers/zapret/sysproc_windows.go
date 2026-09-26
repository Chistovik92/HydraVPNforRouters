//go:build windows
// +build windows

package zapret

import (
	"os/exec"
)

func setProcessGroup(cmd *exec.Cmd) {
	// No-op on Windows
}