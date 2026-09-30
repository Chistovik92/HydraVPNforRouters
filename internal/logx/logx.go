// Package logx is the application journal: leveled lines to stdout and an
// optional size-rotated file, plus a ring buffer that the management API
// serves to clients.
package logx

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Levels in increasing severity.
var levels = map[string]int{"debug": 0, "info": 1, "warn": 2, "error": 3}

// Entry is one journal line.
type Entry struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
}

// Options configure a Logger.
type Options struct {
	Level    string    // debug, info, warn, error (default info)
	Out      io.Writer // default os.Stdout
	File     string    // optional log file
	MaxSize  int64     // bytes before rotation (default 5 MiB)
	Keep     int       // rotated files to keep (default 3)
	BufferSz int       // entries kept in memory (default 1000)
}

// Logger is safe for concurrent use.
type Logger struct {
	mu    sync.Mutex
	level int
	out   io.Writer
	file  *rotatingFile
	ring  []Entry
	next  int
	full  bool
	subs  map[chan Entry]struct{}
}

// New creates a Logger.
func New(o Options) (*Logger, error) {
	l := &Logger{level: Level(o.Level), out: o.Out, subs: map[chan Entry]struct{}{}}
	if l.out == nil {
		l.out = os.Stdout
	}
	size := o.BufferSz
	if size <= 0 {
		size = 1000
	}
	l.ring = make([]Entry, size)
	if o.File != "" {
		max, keep := o.MaxSize, o.Keep
		if max <= 0 {
			max = 5 << 20
		}
		if keep <= 0 {
			keep = 3
		}
		f, err := openRotating(o.File, max, keep)
		if err != nil {
			return l, err
		}
		l.file = f
	}
	return l, nil
}

// Level converts a level name to its rank; unknown names mean info.
func Level(name string) int {
	if v, ok := levels[strings.ToLower(name)]; ok {
		return v
	}
	return levels["info"]
}

// SetLevel changes the threshold at runtime.
func (l *Logger) SetLevel(name string) {
	l.mu.Lock()
	l.level = Level(name)
	l.mu.Unlock()
}

// Log records a line if its level passes the threshold.
func (l *Logger) Log(level, msg string) {
	level = strings.ToLower(level)
	l.mu.Lock()
	defer l.mu.Unlock()
	if Level(level) < l.level {
		return
	}
	e := Entry{Time: time.Now(), Level: level, Message: msg}
	line := fmt.Sprintf("%s [%s] %s\n", e.Time.Format("2006-01-02 15:04:05"), level, msg)
	io.WriteString(l.out, line)
	if l.file != nil {
		l.file.Write([]byte(line))
	}
	l.ring[l.next] = e
	l.next = (l.next + 1) % len(l.ring)
	if l.next == 0 {
		l.full = true
	}
	for ch := range l.subs {
		select {
		case ch <- e:
		default: // slow subscriber: drop instead of blocking the service
		}
	}
}

// Recent returns up to n newest entries (oldest first) at or above level.
func (l *Logger) Recent(n int, level string) []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	var all []Entry
	if l.full {
		all = append(all, l.ring[l.next:]...)
	}
	all = append(all, l.ring[:l.next]...)
	min := Level(level)
	out := all[:0:0]
	for _, e := range all {
		if Level(e.Level) >= min {
			out = append(out, e)
		}
	}
	if n > 0 && len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

// Subscribe returns a channel of new entries and a cancel function.
func (l *Logger) Subscribe() (<-chan Entry, func()) {
	ch := make(chan Entry, 64)
	l.mu.Lock()
	l.subs[ch] = struct{}{}
	l.mu.Unlock()
	return ch, func() {
		l.mu.Lock()
		delete(l.subs, ch)
		l.mu.Unlock()
	}
}

// Close closes the log file.
func (l *Logger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		l.file.Close()
	}
}

type rotatingFile struct {
	path string
	max  int64
	keep int
	f    *os.File
	size int64
}

func openRotating(path string, max int64, keep int) (*rotatingFile, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	r := &rotatingFile{path: path, max: max, keep: keep}
	return r, r.open()
}

func (r *rotatingFile) open() error {
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	r.f, r.size = f, st.Size()
	return nil
}

func (r *rotatingFile) Write(p []byte) (int, error) {
	if r.f == nil {
		return 0, os.ErrClosed
	}
	if r.size+int64(len(p)) > r.max {
		r.rotate()
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

// rotate shifts file -> file.1 -> file.2 ... dropping the oldest.
func (r *rotatingFile) rotate() {
	r.f.Close()
	os.Remove(fmt.Sprintf("%s.%d", r.path, r.keep))
	for i := r.keep - 1; i >= 1; i-- {
		os.Rename(fmt.Sprintf("%s.%d", r.path, i), fmt.Sprintf("%s.%d", r.path, i+1))
	}
	os.Rename(r.path, r.path+".1")
	r.open()
}

func (r *rotatingFile) Close() {
	if r.f != nil {
		r.f.Close()
		r.f = nil
	}
}
