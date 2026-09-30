//go:build !windows

package process

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestStopAndRespawn(t *testing.T) {
	s := &Supervisor{Name: "test", RespawnDelay: 50 * time.Millisecond}
	if err := s.Start("sleep", []string{"30"}); err != nil {
		t.Skip("sleep not available:", err)
	}
	pid := s.Status().PID

	// Unexpected exit is respawned.
	if err := killGroup(s.cmd.Process); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		st := s.Status()
		if st.Running && st.PID != pid {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st := s.Status(); !st.Running || st.PID == pid || st.Restarts != 1 {
		t.Fatalf("not respawned: %+v", st)
	}

	// Stop terminates and does not respawn.
	done := make(chan struct{})
	go func() { s.Stop(2 * time.Second); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop hung")
	}
	time.Sleep(150 * time.Millisecond)
	if s.Status().Running {
		t.Fatal("respawned after Stop")
	}
}

func TestPidFileLifecycleAndLeftoverReaping(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("leftover detection needs /proc")
	}
	pidFile := filepath.Join(t.TempDir(), "run", "child.pid")

	// A child of a "previous daemon": running, recorded in the PID file.
	old := exec.Command("sleep", "60")
	if err := old.Start(); err != nil {
		t.Skip("sleep not available:", err)
	}
	oldPid := old.Process.Pid
	go old.Wait()
	os.MkdirAll(filepath.Dir(pidFile), 0755)
	os.WriteFile(pidFile, []byte(strconv.Itoa(oldPid)+"\n"), 0644)

	s := &Supervisor{Name: "test", PidFile: pidFile}
	if err := s.Start("sleep", []string{"30"}); err != nil {
		t.Fatal(err)
	}
	defer s.Stop(2 * time.Second)

	// The leftover is gone, the new child is recorded.
	deadline := time.Now().Add(3 * time.Second)
	for syscall.Kill(oldPid, 0) == nil && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if syscall.Kill(oldPid, 0) == nil {
		t.Error("leftover child is still running")
	}
	data, _ := os.ReadFile(pidFile)
	if strings.TrimSpace(string(data)) != strconv.Itoa(s.Status().PID) {
		t.Errorf("pid file %q, want %d", data, s.Status().PID)
	}

	// Stopping removes the file.
	s.Stop(2 * time.Second)
	if _, err := os.Stat(pidFile); err == nil {
		t.Error("pid file left behind after Stop")
	}
}

func TestReapIgnoresUnrelatedProcess(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("needs /proc")
	}
	// A PID file pointing at some other program must not kill it.
	other := exec.Command("sleep", "60")
	if err := other.Start(); err != nil {
		t.Skip("sleep not available:", err)
	}
	defer other.Process.Kill()
	go other.Wait()

	pidFile := filepath.Join(t.TempDir(), "child.pid")
	os.WriteFile(pidFile, []byte(strconv.Itoa(other.Process.Pid)+"\n"), 0644)

	s := &Supervisor{Name: "test", PidFile: pidFile}
	s.reapStale("/usr/bin/sing-box")
	time.Sleep(700 * time.Millisecond)
	if syscall.Kill(other.Process.Pid, 0) != nil {
		t.Error("an unrelated process was killed")
	}
}
