package main

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/Chistovik92/hydravpn-router/internal/core"
	"github.com/Chistovik92/hydravpn-router/internal/mgmt"
	"github.com/Chistovik92/hydravpn-router/internal/sms"
	"github.com/Chistovik92/hydravpn-router/internal/telegram"
)

// lateAPI lets the bot exist before the API server it calls: the server
// needs the bot as its SMS box.
type lateAPI struct {
	mu sync.Mutex
	s  *mgmt.Server
}

func (l *lateAPI) set(s *mgmt.Server) {
	l.mu.Lock()
	l.s = s
	l.mu.Unlock()
}

func (l *lateAPI) Call(ctx context.Context, who, method, path string, body interface{}) (int, []byte) {
	l.mu.Lock()
	s := l.s
	l.mu.Unlock()
	if s == nil {
		return 503, []byte(`{"error":"starting"}`)
	}
	return s.Call(ctx, who, method, path, body)
}

// newBot creates the Telegram bot; it reads its settings from the running
// config, so the app can set the token and chats without a restart. SMS
// come from the modems found now; the Keenetic hook spools into
// <runtime-dir>/sms.
func newBot(engine *core.Engine, runtimeDir string, logLine func(level, message string)) (*telegram.Bot, *lateAPI) {
	api := &lateAPI{}
	bot := telegram.New(telegram.Options{
		Settings: func() telegram.Settings {
			s := engine.GetConfig().Settings
			proxy := ""
			if s.TelegramViaProxy {
				proxy = fmt.Sprintf("http://127.0.0.1:%d", config.MixedProxyPort)
			}
			return telegram.Settings{Token: s.TelegramBotToken, Chats: s.TelegramChatIDs, Proxy: proxy,
				SMSForward: s.SMSForward, SMSDelete: s.SMSDeleteForwarded}
		},
		API:   api,
		SMS:   sms.Detect(filepath.Join(runtimeDir, "sms")),
		OnLog: logLine,
	})
	return bot, api
}
