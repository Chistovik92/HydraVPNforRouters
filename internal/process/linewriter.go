package process

import (
	"bytes"
	"regexp"
	"strings"
	"sync"
)

// lineWriter turns the output of a child process into journal lines.
type lineWriter struct {
	mu    sync.Mutex
	buf   []byte
	emit  func(level, msg string)
	prefx string
}

var (
	ansiRe  = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	levelRe = regexp.MustCompile(`(?i)\b(fatal|panic|error|warn(?:ing)?|info|debug|trace)\b`)
)

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		w.line(string(w.buf[:i]))
		w.buf = w.buf[i+1:]
	}
	// A child that never prints a newline must not grow the buffer forever.
	if len(w.buf) > 64<<10 {
		w.line(string(w.buf))
		w.buf = nil
	}
	return len(p), nil
}

func (w *lineWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) > 0 {
		w.line(string(w.buf))
		w.buf = nil
	}
}

func (w *lineWriter) line(s string) {
	s = strings.TrimSpace(ansiRe.ReplaceAllString(s, ""))
	if s == "" || w.emit == nil {
		return
	}
	w.emit(detectLevel(s), s)
}

// detectLevel maps the level word found in a line to a journal level;
// lines without one are "info".
func detectLevel(s string) string {
	head := s
	if len(head) > 80 {
		head = head[:80] // the level comes near the start; do not match message text
	}
	m := levelRe.FindString(head)
	switch strings.ToLower(m) {
	case "fatal", "panic", "error":
		return "error"
	case "warn", "warning":
		return "warn"
	case "debug", "trace":
		return "debug"
	}
	return "info"
}
