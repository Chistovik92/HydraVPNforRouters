//go:build windows
// +build windows

package singbox

import (
	"os/exec"
)

func setProcessGroup(cmd *exec.Cmd) {
	// No-op on Windows
}
