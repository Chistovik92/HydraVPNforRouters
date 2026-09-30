//go:build !windows

package process

// KillChildrenOnExit is only needed on Windows. On Linux the daemon stops its
// children on SIGTERM/SIGINT and reaps leftovers from a killed daemon at the
// next start (Supervisor.PidFile).
func KillChildrenOnExit() error { return nil }
