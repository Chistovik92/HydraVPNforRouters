package sms

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParsers(t *testing.T) {
	n := ParseNdmc("        id: nv-1\n      from: +79001234567\n timestamp: 2026-10-17T10:00:00\n      text: Ваш код 1234.\n            Никому не сообщайте\n")
	if n.From != "+79001234567" || n.Text != "Ваш код 1234. Никому не сообщайте" || n.Time == "" {
		t.Errorf("ndmc: %+v", n)
	}
	m := ParseMmcli("sms.content.number : 900\nsms.content.text : Баланс 100 р.\nsms.properties.timestamp : 2026-10-17T10:00:00+03\nsms.properties.state : received\n")
	if m.From != "900" || m.Text != "Баланс 100 р." {
		t.Errorf("mmcli: %+v", m)
	}
	s := ParseSMSTools("From: 79001234567\nReceived: 26-10-17 10:00:00\nAlphabet: UCS2\n\nПривет\nмир\n")
	if s.From != "79001234567" || s.Text != "Привет\nмир" {
		t.Errorf("smstools: %+v", s)
	}
}

type fakeNdmc struct{ cmds []string }

func (f *fakeNdmc) Run(ctx context.Context, cmd string) (string, error) {
	f.cmds = append(f.cmds, cmd)
	return "from: +7900\ntext: hi\n", nil
}

func TestKeeneticSpool(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "UsbQmi0-nv-1"), []byte("UsbQmi0 nv-1\n"), 0644)
	os.WriteFile(filepath.Join(dir, "bad"), []byte("UsbQmi0;reboot nv-1\n"), 0644)
	nd := &fakeNdmc{}
	k := &Keenetic{Spool: dir, Ndmc: nd}
	msgs, err := k.Poll(context.Background())
	if err != nil || len(msgs) != 1 || msgs[0].Modem != "UsbQmi0" || msgs[0].ID != "nv-1" || msgs[0].Text != "hi" {
		t.Fatalf("%v %+v", err, msgs)
	}
	if len(nd.cmds) != 1 || nd.cmds[0] != "sms UsbQmi0 list id nv-1" {
		t.Errorf("ndmc commands: %v (an unsafe entry must not reach ndmc)", nd.cmds)
	}
	// Not delivered yet: the entry stays and the SMS comes again.
	if again, _ := k.Poll(context.Background()); len(again) != 1 {
		t.Errorf("an SMS not marked Done must be returned again: %+v", again)
	}
	k.Done(msgs[0])
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Errorf("spool not emptied after Done: %v", left)
	}
}

type failingNdmc struct{ calls int }

func (f *failingNdmc) Run(ctx context.Context, cmd string) (string, error) {
	f.calls++
	if strings.Contains(cmd, "gone") {
		return "", os.ErrNotExist
	}
	return "from: 900\ntext: ok\n", nil
}

func TestKeeneticBadEntryDoesNotBlockTheRest(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "UsbQmi0-gone"), []byte("UsbQmi0 gone\n"), 0644)
	os.WriteFile(filepath.Join(dir, "UsbQmi0-nv-2"), []byte("UsbQmi0 nv-2\n"), 0644)
	k := &Keenetic{Spool: dir, Ndmc: &failingNdmc{}}
	for i := 0; i < maxReadFailures; i++ {
		msgs, err := k.Poll(context.Background())
		if err == nil || len(msgs) != 1 || msgs[0].ID != "nv-2" {
			t.Fatalf("poll %d: %v %+v", i, err, msgs)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "UsbQmi0-gone")); !os.IsNotExist(err) {
		t.Error("an entry that keeps failing must be dropped")
	}
}

func TestNdmcTextCannotReplaceTheSender(t *testing.T) {
	m := ParseNdmc("      from: +79001234567\n      text: Код 1234\n            from: Bank\n")
	if m.From != "+79001234567" || !strings.Contains(m.Text, "from: Bank") {
		t.Errorf("%+v", m)
	}
}

func TestModemManagerPollsOnce(t *testing.T) {
	var calls []string
	m := &ModemManager{run: func(ctx context.Context, args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		if args[0] == "-K" {
			return "sms.content.number : 900\nsms.content.text : x\n", nil
		}
		return "    /org/freedesktop/ModemManager1/SMS/3 (received)\n    /org/freedesktop/ModemManager1/SMS/4 (sent)\n", nil
	}}
	m.seen = newSeen(filepath.Join(t.TempDir(), ".seen"))
	first, _ := m.Poll(context.Background())
	if len(first) != 1 || first[0].ID != "3" {
		t.Fatalf("first %+v calls %v", first, calls)
	}
	m.Done(first[0])
	if second, _ := m.Poll(context.Background()); len(second) != 0 {
		t.Errorf("a Done SMS came again: %+v", second)
	}
	// After a restart of the service the handled ids are still known.
	restarted := &ModemManager{run: m.run, seen: newSeen(m.seen.path)}
	if again, _ := restarted.Poll(context.Background()); len(again) != 0 {
		t.Errorf("forwarded again after a restart: %+v", again)
	}
}

func TestSMSToolsSendAndDelete(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "incoming"), 0755)
	os.MkdirAll(filepath.Join(dir, "outgoing"), 0755)
	os.WriteFile(filepath.Join(dir, "incoming", "GSM1.abc"), []byte("From: 900\n\nhello\n"), 0644)
	s := &SMSTools{Dir: dir}
	msgs, _ := s.Poll(context.Background())
	if len(msgs) != 1 {
		t.Fatalf("poll: %+v", msgs)
	}
	s.Done(msgs[0])
	if again, _ := s.Poll(context.Background()); len(again) != 0 {
		t.Fatalf("a Done SMS came again: %+v", again)
	}
	if err := s.Delete(context.Background(), Message{ID: "../x"}); err == nil {
		t.Error("path traversal accepted")
	}
	if err := s.Delete(context.Background(), msgs[0]); err != nil {
		t.Error(err)
	}
	if err := s.Send(context.Background(), "+79001234567", "text"); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadDir(filepath.Join(dir, "outgoing"))
	if len(out) != 1 {
		t.Fatalf("outgoing: %v", out)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "outgoing", out[0].Name()))
	if !strings.HasPrefix(string(data), "To: 79001234567\n\ntext") {
		t.Errorf("file: %q", data)
	}
	if ValidNumber("+7900; rm") || !ValidNumber("+79001234567") {
		t.Error("ValidNumber")
	}
}
