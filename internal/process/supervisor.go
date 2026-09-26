// Package process supervises external helper binaries (sing-box, nfqws, ciadpi).
package process

import (
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

// Supervisor runs a single child process, restarts it when it exits
// unexpectedly and stops it gracefully.
//
// Only the monitor goroutine calls cmd.Wait, so Stop and Restart never race
// with it; they wait on the done channel instead.
type Supervisor struct {
	Name         string
	RespawnDelay time.Duration
	OnLog        func(level, message string)

	mu           sync.Mutex
	bin          string
	args         []string
	cmd          *exec.Cmd
	done         chan struct{}
	stopping     bool
	respawnTimer *time.Timer
	startTime    time.Time
	restarts     int
	lastError    string
}

// Status is a snapshot of the supervised process.
type Status struct {
	Running   bool
	PID       int
	Uptime    time.Duration
	Restarts  int
	LastError string
}

// Start launches bin with args. It is a no-op if the process is already running.
func (s *Supervisor) Start(bin string, args []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopping = false
	return s.startLocked(bin, args)
}

func (s *Supervisor) startLocked(bin string, args []string) error {
	if s.cmd != nil {
		return nil
	}
	if bin == "" {
		return errors.New("binary path is empty")
	}

	cmd := exec.Command(bin, args...)
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		s.lastError = err.Error()
		return fmt.Errorf("start %s: %w", s.Name, err)
	}

	s.bin, s.args = bin, append([]string(nil), args...)
	s.cmd = cmd
	s.done = make(chan struct{})
	s.startTime = time.Now()
	s.log("info", "started (PID: %d)", cmd.Process.Pid)

	go s.monitor(cmd, s.done)
	return nil
}

// Stop terminates the process, waiting up to timeout before killing it.
func (s *Supervisor) Stop(timeout time.Duration) {
	s.mu.Lock()
	s.stopping = true
	if s.respawnTimer != nil {
		s.respawnTimer.Stop()
		s.respawnTimer = nil
	}
	cmd, done := s.cmd, s.done
	s.mu.Unlock()

	if cmd == nil {
		return
	}

	if err := terminate(cmd.Process); err != nil {
		_ = cmd.Process.Kill()
	}
	select {
	case <-done:
	case <-time.After(timeout):
		s.log("warn", "did not exit in %s, killing", timeout)
		_ = killGroup(cmd.Process)
		<-done
	}
	s.log("info", "stopped")
}

// Restart stops the process and starts it again with new arguments.
func (s *Supervisor) Restart(bin string, args []string, timeout time.Duration) error {
	s.Stop(timeout)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopping = false
	s.restarts++
	return s.startLocked(bin, args)
}

// Reload asks the process to reload its configuration (SIGHUP on Unix).
func (s *Supervisor) Reload() error {
	s.mu.Lock()
	cmd := s.cmd
	s.mu.Unlock()
	if cmd == nil {
		return errors.New("process is not running")
	}
	return hangup(cmd.Process)
}

// Status returns the current process state.
func (s *Supervisor) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := Status{Restarts: s.restarts, LastError: s.lastError}
	if s.cmd != nil {
		st.Running = true
		st.PID = s.cmd.Process.Pid
		st.Uptime = time.Since(s.startTime)
	}
	return st
}

func (s *Supervisor) monitor(cmd *exec.Cmd, done chan struct{}) {
	err := cmd.Wait()

	s.mu.Lock()
	if s.cmd == cmd {
		s.cmd = nil
	}
	stopping := s.stopping
	if err != nil && !stopping {
		s.lastError = err.Error()
	}
	if !stopping && s.RespawnDelay > 0 {
		s.respawnTimer = time.AfterFunc(s.RespawnDelay, s.respawn)
	}
	s.mu.Unlock()
	close(done)

	switch {
	case stopping:
	case err != nil:
		s.log("error", "exited: %v", err)
	default:
		s.log("warn", "exited unexpectedly")
	}
}

func (s *Supervisor) respawn() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.respawnTimer = nil
	if s.stopping || s.cmd != nil {
		return
	}
	s.restarts++
	if err := s.startLocked(s.bin, s.args); err != nil {
		s.log("error", "respawn failed: %v", err)
		s.respawnTimer = time.AfterFunc(s.RespawnDelay, s.respawn)
	}
}

func (s *Supervisor) log(level, format string, args ...interface{}) {
	if s.OnLog != nil {
		s.OnLog(level, "["+s.Name+"] "+fmt.Sprintf(format, args...))
	}
}
