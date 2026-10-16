// Package telegram is a Telegram bot that manages the router through the
// same handlers as the management API (in process, no network) and forwards
// SMS from the router's modem.
//
// Only chats listed in telegram_chat_ids may use it; any other chat gets
// its own id back, to be added to the list.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/selfupdate"
	"github.com/Chistovik92/hydravpn-router/internal/sms"
)

const (
	pollTimeout  = 50 // seconds, Telegram long polling
	smsInterval  = 10 * time.Second
	maxMessage   = 4000 // Telegram allows 4096 characters
	keepSMS      = 20
	retryOnError = 15 * time.Second
)

// API runs management API requests in process (mgmt.Server.Call).
type API interface {
	Call(ctx context.Context, who, method, path string, body interface{}) (int, []byte)
}

// Settings are read from the current config on every use, so changes
// made in the app apply without a restart.
type Settings struct {
	Token      string
	Chats      []int64
	Proxy      string // "" or the local proxy Telegram is reached through
	SMSForward bool   // send new SMS to the chats
	SMSDelete  bool   // delete forwarded SMS from the modem
}

// Options configure a Bot.
type Options struct {
	Settings func() Settings
	API      API
	SMS      []sms.Source
	OnLog    func(level, message string)

	BaseURL string // https://api.telegram.org (tests)
	Client  *http.Client
}

// Bot is the Telegram bot.
type Bot struct {
	o Options

	mu        sync.Mutex
	recent    []sms.Message
	httpc     *http.Client
	httpProxy string
	answered  map[int64]time.Time // strangers already told their id
}

// New creates a Bot; Run starts it.
func New(o Options) *Bot {
	if o.BaseURL == "" {
		o.BaseURL = "https://api.telegram.org"
	}
	return &Bot{o: o}
}

func (b *Bot) settings() Settings { return b.o.Settings() }

// client is kept while the proxy setting stays the same, so long polling
// reuses one connection instead of opening a new one per call.
func (b *Bot) client(proxy string) *http.Client {
	if b.o.Client != nil {
		return b.o.Client
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.httpc == nil || b.httpProxy != proxy {
		b.httpc, b.httpProxy = selfupdate.NewHTTPClient(proxy, (pollTimeout+15)*time.Second), proxy
	}
	return b.httpc
}

// call invokes a Bot API method.
func (b *Bot) call(ctx context.Context, method string, req, resp interface{}) error {
	st := b.settings()
	if st.Token == "" {
		return errNoToken
	}
	data, _ := json.Marshal(req)
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, b.o.BaseURL+"/bot"+st.Token+"/"+method, bytes.NewReader(data))
	if err != nil {
		return errors.New("telegram: bad request")
	}
	hr.Header.Set("Content-Type", "application/json")
	r, err := b.client(st.Proxy).Do(hr)
	if err != nil {
		// The URL holds the token: never log the request error as is.
		return fmt.Errorf("telegram %s: %s", method, redact(err.Error(), st.Token))
	}
	defer r.Body.Close()
	var env struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&env); err != nil {
		return fmt.Errorf("telegram %s: HTTP %d", method, r.StatusCode)
	}
	if !env.OK {
		return fmt.Errorf("telegram %s: %s", method, env.Description)
	}
	if resp != nil {
		return json.Unmarshal(env.Result, resp)
	}
	return nil
}

var errNoToken = errors.New("telegram_bot_token is not set")

func redact(s, secret string) string {
	if secret == "" {
		return s
	}
	return strings.ReplaceAll(strings.ReplaceAll(s, url.PathEscape(secret), "***"), secret, "***")
}

// Send writes text to one chat (cut to Telegram's limit).
func (b *Bot) Send(ctx context.Context, chat int64, text string) error {
	if r := []rune(text); len(r) > maxMessage {
		text = string(r[:maxMessage]) + "\n…"
	}
	return b.call(ctx, "sendMessage", map[string]interface{}{"chat_id": chat, "text": text, "disable_web_page_preview": true}, nil)
}

// Notify writes text to every allowed chat; it reports whether at least
// one chat got it.
func (b *Bot) Notify(ctx context.Context, text string) bool {
	delivered := false
	for _, id := range b.settings().Chats {
		if err := b.Send(ctx, id, text); err != nil {
			b.log("warn", "notify %d: %v", id, err)
			continue
		}
		delivered = true
	}
	return delivered
}

type update struct {
	ID      int64 `json:"update_id"`
	Message *struct {
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
		From *struct {
			ID int64 `json:"id"`
		} `json:"from"`
		Text string `json:"text"`
	} `json:"message"`
}

// Run polls Telegram and the modem until ctx ends.
func (b *Bot) Run(ctx context.Context) {
	if len(b.o.SMS) > 0 {
		go b.smsLoop(ctx)
	}
	var offset int64
	for ctx.Err() == nil {
		if b.settings().Token == "" {
			sleep(ctx, retryOnError) // set later from the app
			continue
		}
		var ups []update
		err := b.call(ctx, "getUpdates", map[string]interface{}{"offset": offset, "timeout": pollTimeout,
			"allowed_updates": []string{"message"}}, &ups)
		if err != nil {
			if ctx.Err() == nil {
				b.log("warn", "%v", err)
				sleep(ctx, retryOnError)
			}
			continue
		}
		for _, u := range ups {
			offset = u.ID + 1
			if u.Message != nil && u.Message.Text != "" && u.Message.From != nil {
				b.handle(ctx, u.Message.Chat.ID, u.Message.From.ID, u.Message.Text)
			}
		}
	}
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// handle runs a command. The sender is checked, not the chat: in a group
// every member could otherwise manage the router.
func (b *Bot) handle(ctx context.Context, chat, from int64, text string) {
	allowed := false
	for _, id := range b.settings().Chats {
		allowed = allowed || id == from
	}
	if !allowed {
		// Answer a stranger once an hour: spam must not flood the journal
		// or make the bot hit Telegram limits.
		b.mu.Lock()
		if b.answered == nil {
			b.answered = map[int64]time.Time{}
		}
		recent := time.Since(b.answered[from]) < time.Hour
		if !recent {
			b.answered[from] = time.Now()
		}
		b.mu.Unlock()
		if !recent {
			b.log("warn", "message from user %d, who is not in telegram_chat_ids", from)
			b.Send(ctx, chat, fmt.Sprintf("Вы не можете управлять этим роутером. Ваш ID: %d — добавьте его в telegram_chat_ids (веб-интерфейс, приложение Hydra или config.yaml).", from))
		}
		return
	}
	cmd, args := parseCommand(text)
	reply := b.run(ctx, cmd, args)
	if err := b.Send(ctx, chat, reply); err != nil {
		b.log("warn", "reply: %v", err)
	}
}

// parseCommand splits "/select@bot main Node 1" into "select" and args.
func parseCommand(text string) (string, []string) {
	f := strings.Fields(strings.TrimSpace(text))
	if len(f) == 0 || !strings.HasPrefix(f[0], "/") {
		return "", nil
	}
	cmd, _, _ := strings.Cut(strings.TrimPrefix(f[0], "/"), "@")
	return strings.ToLower(cmd), f[1:]
}

func (b *Bot) log(level, format string, args ...interface{}) {
	if b.o.OnLog != nil {
		b.o.OnLog(level, "[telegram] "+redact(fmt.Sprintf(format, args...), b.settings().Token))
	}
}

// ---------------------------------------------------------------- SMS

func (b *Bot) smsLoop(ctx context.Context) {
	for ctx.Err() == nil {
		st := b.settings()
		// Without a bot to deliver to, messages stay on the modem.
		if !st.SMSForward || st.Token == "" || len(st.Chats) == 0 {
			sleep(ctx, smsInterval)
			continue
		}
		for _, src := range b.o.SMS {
			msgs, err := src.Poll(ctx)
			if err != nil {
				b.log("warn", "sms %s: %v", src.Name(), err)
			}
			for _, m := range msgs {
				if m.From == "" && m.Text == "" {
					// Unexpected modem output: never delete what could not be read.
					b.log("warn", "sms %s from %s could not be read; it stays on the modem", m.ID, src.Name())
					src.Done(m)
					continue
				}
				if !b.Notify(ctx, formatSMS(m)) {
					// Not Done: the next poll tries again.
					b.log("warn", "sms from %s was not delivered to Telegram; it stays on the modem", m.From)
					continue
				}
				b.remember(m)
				src.Done(m)
				if st.SMSDelete {
					if err := src.Delete(ctx, m); err != nil {
						b.log("warn", "sms %s: delete %s: %v", src.Name(), m.ID, err)
					}
				}
			}
		}
		sleep(ctx, smsInterval)
	}
}

func (b *Bot) remember(m sms.Message) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.recent = append(b.recent, m)
	if len(b.recent) > keepSMS {
		b.recent = b.recent[len(b.recent)-keepSMS:]
	}
}

// Recent returns the last SMS, newest last.
func (b *Bot) Recent() []sms.Message {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]sms.Message(nil), b.recent...)
}

func formatSMS(m sms.Message) string {
	var s strings.Builder
	s.WriteString("📩 SMS от " + firstOf(m.From, "неизвестного"))
	if m.Modem != "" {
		s.WriteString(" (" + m.Modem + ")")
	}
	if m.Time != "" {
		s.WriteString("\n" + m.Time)
	}
	s.WriteString("\n\n" + m.Text)
	return s.String()
}

// Modems names the SMS sources found on the router.
func (b *Bot) Modems() []string {
	out := make([]string, 0, len(b.o.SMS))
	for _, s := range b.o.SMS {
		out = append(out, s.Name())
	}
	return out
}

// SendSMS sends an SMS through the first modem that can.
func (b *Bot) SendSMS(ctx context.Context, to, text string) error {
	if !sms.ValidNumber(to) {
		return fmt.Errorf("номер %q не похож на телефонный", to)
	}
	if len(b.o.SMS) == 0 {
		return sms.ErrNoModem
	}
	var errs []string
	for _, src := range b.o.SMS {
		err := src.Send(ctx, to, text)
		if err == nil {
			return nil
		}
		errs = append(errs, src.Name()+": "+err.Error())
	}
	return errors.New(strings.Join(errs, "; "))
}

func firstOf(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}
