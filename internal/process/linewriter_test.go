package process

import "testing"

func TestLineWriterSplitsAndDetectsLevels(t *testing.T) {
	type rec struct{ level, msg string }
	var got []rec
	w := &lineWriter{emit: func(l, m string) { got = append(got, rec{l, m}) }}

	w.Write([]byte("+0300 2026-09-30 12:00:00 ERROR router: boom\nplain line\n\x1b[33mWARN\x1b[0m slow"))
	w.Write([]byte("\nINFO ok\npartial"))
	w.Flush()

	want := []rec{
		{"error", "+0300 2026-09-30 12:00:00 ERROR router: boom"},
		{"info", "plain line"},
		{"warn", "WARN slow"},
		{"info", "INFO ok"},
		{"info", "partial"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: %v, want %v", i, got[i], want[i])
		}
	}
}
