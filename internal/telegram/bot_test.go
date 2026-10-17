package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeAPI struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeAPI) Call(ctx context.Context, who, method, path string, body interface{}) (int, []byte) {
	f.mu.Lock()
	b, _ := json.Marshal(body)
	f.calls = append(f.calls, method+" "+path+" "+string(b))
	f.mu.Unlock()
	switch path {
	case "/api/v1/status":
		return 200, []byte(`{"state":"running","version":"1.3.0","uptime":"1h"}`)
	case "/api/v1/nodes/select":
		return 200, []byte(`{"status":"selected"}`)
	}
	return 404, []byte(`{"error":"not found"}`)
}

// fakeTelegram answers getUpdates once with the given messages and records
// sendMessage calls.
func fakeTelegram(t *testing.T, token string, msgs []map[string]interface{}) (*httptest.Server, func() []map[string]interface{}) {
	var mu sync.Mutex
	var sent []map[string]interface{}
	served := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/bot"+token+"/") {
			http.Error(w, `{"ok":false,"description":"Unauthorized"}`, 401)
			return
		}
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		defer mu.Unlock()
		switch strings.TrimPrefix(r.URL.Path, "/bot"+token+"/") {
		case "getUpdates":
			if served {
				time.Sleep(20 * time.Millisecond)
				w.Write([]byte(`{"ok":true,"result":[]}`))
				return
			}
			served = true
			var ups []map[string]interface{}
			for i, m := range msgs {
				ups = append(ups, map[string]interface{}{"update_id": i + 1, "message": m})
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "result": ups})
		case "sendMessage":
			sent = append(sent, req)
			w.Write([]byte(`{"ok":true,"result":{}}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() []map[string]interface{} {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]interface{}(nil), sent...)
	}
}

func TestBotObeysOnlyAllowedChats(t *testing.T) {
	token := "123:secret"
	srv, sent := fakeTelegram(t, token, []map[string]interface{}{
		{"chat": map[string]interface{}{"id": 42}, "from": map[string]interface{}{"id": 42}, "text": "/status"},
		{"chat": map[string]interface{}{"id": -100}, "from": map[string]interface{}{"id": 42}, "text": "/select@hydra_bot main Node 1"},
		// A group listed by mistake does not let its other members in.
		{"chat": map[string]interface{}{"id": -100}, "from": map[string]interface{}{"id": 7}, "text": "/restart"},
		{"chat": map[string]interface{}{"id": -100}, "from": map[string]interface{}{"id": 7}, "text": "/restart"},
	})
	api := &fakeAPI{}
	var logMu sync.Mutex
	var logs []string
	b := New(Options{
		Settings: func() Settings { return Settings{Token: token, Chats: []int64{42, -100}} },
		API:      api, BaseURL: srv.URL, Client: srv.Client(),
		OnLog: func(level, m string) { logMu.Lock(); logs = append(logs, m); logMu.Unlock() },
	})
	ctx, cancel := context.WithCancel(context.Background())
	go b.Run(ctx)
	deadline := time.Now().Add(3 * time.Second)
	for len(sent()) < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()

	got := sent()
	if len(got) != 3 {
		t.Fatalf("sent %v", got)
	}
	if !strings.Contains(got[0]["text"].(string), "running") {
		t.Errorf("status reply: %v", got[0]["text"])
	}
	if !strings.Contains(got[1]["text"].(string), "Выбран Node 1 в main") {
		t.Errorf("select reply: %v", got[1]["text"])
	}
	if got[2]["chat_id"].(float64) != -100 || !strings.Contains(got[2]["text"].(string), "ID: 7") {
		t.Errorf("stranger reply: %v", got[2])
	}
	api.mu.Lock()
	for _, c := range api.calls {
		if strings.Contains(c, "restart") {
			t.Errorf("a stranger restarted the service: %v", api.calls)
		}
	}
	if len(api.calls) != 2 || !strings.Contains(api.calls[1], `"node":"Node 1"`) {
		t.Errorf("api calls: %v", api.calls)
	}
	api.mu.Unlock()
	logMu.Lock()
	for _, l := range logs {
		if strings.Contains(l, "secret") {
			t.Errorf("token in the journal: %s", l)
		}
	}
	logMu.Unlock()
}

func TestHelpListsCommands(t *testing.T) {
	h := helpText()
	for _, c := range []string{"/status", "/addsub", "/core", "/sendsms", "/update"} {
		if !strings.Contains(h, c) {
			t.Errorf("help misses %s", c)
		}
	}
	if cmd, args := parseCommand("/Versions@bot xray"); cmd != "versions" || len(args) != 1 {
		t.Errorf("%s %v", cmd, args)
	}
}

func TestNewTokenStartsFromItsOwnUpdates(t *testing.T) {
	var mu sync.Mutex
	token := "1:old"
	var offsetsOfNew []float64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		json.NewDecoder(r.Body).Decode(&req)
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.HasPrefix(r.URL.Path, "/bot1:old/getUpdates"):
			// The old bot has update 500; afterwards the token is replaced.
			token = "2:new"
			w.Write([]byte(`{"ok":true,"result":[{"update_id":500}]}`))
		case strings.HasPrefix(r.URL.Path, "/bot2:new/getUpdates"):
			offsetsOfNew = append(offsetsOfNew, req["offset"].(float64))
			time.Sleep(10 * time.Millisecond)
			w.Write([]byte(`{"ok":true,"result":[]}`))
		default:
			w.Write([]byte(`{"ok":true,"result":{}}`))
		}
	}))
	defer srv.Close()
	b := New(Options{
		Settings: func() Settings { mu.Lock(); defer mu.Unlock(); return Settings{Token: token} },
		API:      &fakeAPI{}, BaseURL: srv.URL, Client: srv.Client(),
	})
	ctx, cancel := context.WithCancel(context.Background())
	go b.Run(ctx)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(offsetsOfNew)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	mu.Lock()
	defer mu.Unlock()
	if len(offsetsOfNew) == 0 || offsetsOfNew[0] != 0 {
		t.Errorf("the new bot was polled with offsets %v, want 0 first", offsetsOfNew)
	}
}
