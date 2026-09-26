//go:build !windows

package process

import (
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
