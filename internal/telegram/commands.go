package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// Every command is a call of the management API, so the bot can do exactly
// what the app and the web UI can, with the same checks.

type command struct {
	usage string
	help  string
	run   func(b *Bot, ctx context.Context, args []string) string
}

var commands map[string]command

func init() {
	commands = map[string]command{
		"status":   {"/status", "состояние службы", cmdStatus},
		"nodes":    {"/nodes", "группы и выбранные узлы", cmdNodes},
		"select":   {"/select <группа> <узел>", "выбрать узел в группе", cmdSelect},
		"test":     {"/test <узел>", "замерить задержку узла", cmdTest},
		"subs":     {"/subs", "подписки", cmdSubs},
		"addsub":   {"/addsub <раздел> <ссылка>", "добавить подписку (её выдаёт бот «Радар» или приложение Hydra)", cmdAddSub},
		"refresh":  {"/refresh <номер>", "обновить подписку сейчас", cmdRefresh},
		"delsub":   {"/delsub <номер>", "удалить подписку", cmdDelSub},
		"update":   {"/update [check|install [версия]]", "обновление службы", cmdUpdate},
		"cores":    {"/cores", "ядра и их версии", cmdCores},
		"versions": {"/versions <ядро>", "доступные версии ядра (с бетами)", cmdVersions},
		"core":     {"/core <ядро> <версия|latest|beta>", "установить ядро: sing-box, xray, openflux, olcrtc, amneziawg", cmdCoreInstall},
		"reload":   {"/reload", "перечитать конфиг", cmdPost("/api/v1/reload", "Конфиг перечитан")},
		"restart":  {"/restart", "перезапустить службу", cmdPost("/api/v1/restart", "Служба перезапускается")},
		"logs":     {"/logs [N]", "последние предупреждения и ошибки", cmdLogs},
		"check":    {"/check <global|dns|singbox|nft|proxy>", "диагностика", cmdCheck},
		"set":      {"/set <ключ> <значение>", "изменить настройку (значение — JSON или текст)", cmdSet},
		"sms":      {"/sms", "последние SMS", cmdSMS},
		"sendsms":  {"/sendsms <номер> <текст>", "отправить SMS с модема роутера", cmdSendSMS},
	}
}

func (b *Bot) run(ctx context.Context, cmd string, args []string) string {
	if c, ok := commands[cmd]; ok {
		return c.run(b, ctx, args)
	}
	return helpText()
}

func helpText() string {
	names := make([]string, 0, len(commands))
	for n := range commands {
		names = append(names, n)
	}
	sort.Strings(names)
	var s strings.Builder
	s.WriteString("HydraVPN for Routers — команды:\n")
	for _, n := range names {
		fmt.Fprintf(&s, "%s — %s\n", commands[n].usage, commands[n].help)
	}
	return s.String()
}

// api calls the management API and decodes the JSON answer into v; the
// returned text is an error message for the chat ("" on success).
func (b *Bot) api(ctx context.Context, method, path string, body, v interface{}) string {
	code, data := b.o.API.Call(ctx, "telegram", method, path, body)
	if code >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &e) == nil && e.Error != "" {
			return "Ошибка: " + e.Error
		}
		return fmt.Sprintf("Ошибка: HTTP %d", code)
	}
	if v != nil {
		if err := json.Unmarshal(data, v); err != nil {
			return "Ошибка: " + err.Error()
		}
	}
	return ""
}

func usage(cmd string) string { return "Использование: " + commands[cmd].usage }

func cmdStatus(b *Bot, ctx context.Context, _ []string) string {
	var st map[string]interface{}
	if e := b.api(ctx, http.MethodGet, "/api/v1/status", nil, &st); e != "" {
		return e
	}
	var s strings.Builder
	fmt.Fprintf(&s, "Служба: %v, версия %v\n", st["state"], st["version"])
	if up, _ := st["uptime"].(string); up != "" {
		fmt.Fprintf(&s, "Работает: %s\n", up)
	}
	if e, _ := st["last_error"].(string); e != "" {
		fmt.Fprintf(&s, "Последняя ошибка: %s\n", e)
	}
	if u, ok := st["update"].(map[string]interface{}); ok && u["update_available"] == true {
		fmt.Fprintf(&s, "Доступно обновление: %v (/update install)\n", u["latest"])
	}
	if c, ok := st["cores"].(map[string]interface{}); ok {
		if nodes, ok := c["nodes"].([]interface{}); ok && len(nodes) > 0 {
			running := 0
			for _, n := range nodes {
				if m, ok := n.(map[string]interface{}); ok && m["running"] == true {
					running++
				}
			}
			fmt.Fprintf(&s, "Узлы на ядрах Xray/OpenFlux/olcRTC/AWG: %d, работают %d\n", len(nodes), running)
		}
	}
	return s.String()
}

type node struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Now     string   `json:"now"`
	Members []string `json:"members"`
	DelayMS int      `json:"delay_ms"`
}

func cmdNodes(b *Bot, ctx context.Context, _ []string) string {
	var nodes []node
	if e := b.api(ctx, http.MethodGet, "/api/v1/nodes", nil, &nodes); e != "" {
		return e
	}
	var s strings.Builder
	for _, n := range nodes {
		if len(n.Members) == 0 {
			continue
		}
		fmt.Fprintf(&s, "%s (%s): %s\n", n.Name, n.Type, n.Now)
	}
	if s.Len() == 0 {
		return "Групп нет: добавьте подписку (/addsub) — её выдаёт бот «Радар» или приложение Hydra."
	}
	return s.String()
}

func cmdSelect(b *Bot, ctx context.Context, args []string) string {
	if len(args) < 2 {
		return usage("select")
	}
	body := map[string]string{"group": args[0], "node": strings.Join(args[1:], " ")}
	if e := b.api(ctx, http.MethodPost, "/api/v1/nodes/select", body, nil); e != "" {
		return e
	}
	return "Выбран " + body["node"] + " в " + body["group"]
}

func cmdTest(b *Bot, ctx context.Context, args []string) string {
	if len(args) == 0 {
		return usage("test")
	}
	var r struct {
		DelayMS int `json:"delay_ms"`
	}
	n := strings.Join(args, " ")
	if e := b.api(ctx, http.MethodPost, "/api/v1/nodes/test", map[string]string{"node": n}, &r); e != "" {
		return e
	}
	return fmt.Sprintf("%s: %d мс", n, r.DelayMS)
}

func cmdSubs(b *Bot, ctx context.Context, _ []string) string {
	var subs []struct {
		Index   int    `json:"index"`
		Section string `json:"section"`
		URL     string `json:"url"`
	}
	if e := b.api(ctx, http.MethodGet, "/api/v1/subscriptions", nil, &subs); e != "" {
		return e
	}
	if len(subs) == 0 {
		return "Подписок нет. Получите ссылку в боте «Радар» или в приложении Hydra и добавьте: /addsub <раздел> <ссылка>"
	}
	var s strings.Builder
	for _, v := range subs {
		fmt.Fprintf(&s, "%d. [%s] %s\n", v.Index, v.Section, v.URL)
	}
	return s.String()
}

func cmdAddSub(b *Bot, ctx context.Context, args []string) string {
	if len(args) != 2 {
		return usage("addsub")
	}
	if e := b.api(ctx, http.MethodPost, "/api/v1/subscriptions", map[string]string{"section": args[0], "url": args[1]}, nil); e != "" {
		return e
	}
	return "Подписка добавлена в раздел " + args[0]
}

func indexArg(cmd string, args []string) (string, string) {
	if len(args) != 1 {
		return "", usage(cmd)
	}
	if _, err := fmt.Sscanf(args[0], "%d", new(int)); err != nil {
		return "", usage(cmd)
	}
	return url.PathEscape(args[0]), ""
}

func cmdRefresh(b *Bot, ctx context.Context, args []string) string {
	i, msg := indexArg("refresh", args)
	if msg != "" {
		return msg
	}
	if e := b.api(ctx, http.MethodPost, "/api/v1/subscriptions/"+i+"/refresh", nil, nil); e != "" {
		return e
	}
	return "Подписка обновлена"
}

func cmdDelSub(b *Bot, ctx context.Context, args []string) string {
	i, msg := indexArg("delsub", args)
	if msg != "" {
		return msg
	}
	if e := b.api(ctx, http.MethodDelete, "/api/v1/subscriptions/"+i, nil, nil); e != "" {
		return e
	}
	return "Подписка удалена"
}

func cmdUpdate(b *Bot, ctx context.Context, args []string) string {
	var st struct {
		Current   string `json:"current"`
		Latest    string `json:"latest"`
		Available bool   `json:"update_available"`
		State     string `json:"state"`
		Error     string `json:"error"`
	}
	switch {
	case len(args) > 0 && args[0] == "check":
		if e := b.api(ctx, http.MethodPost, "/api/v1/update/check", nil, &st); e != "" {
			return e
		}
	case len(args) > 0 && args[0] == "install":
		body := map[string]string{}
		if len(args) > 1 {
			body["version"] = args[1]
		}
		if e := b.api(ctx, http.MethodPost, "/api/v1/update", body, nil); e != "" {
			return e
		}
		return "Обновление началось: служба перезапустится сама. Проверить — /update"
	default:
		if e := b.api(ctx, http.MethodGet, "/api/v1/update", nil, &st); e != "" {
			return e
		}
	}
	s := fmt.Sprintf("Установлена %s, последняя %s, состояние: %s", st.Current, firstOf(st.Latest, "?"), st.State)
	if st.Available {
		s += "\nЕсть обновление: /update install"
	}
	if st.Error != "" {
		s += "\nОшибка: " + st.Error
	}
	return s
}

func cmdCores(b *Bot, ctx context.Context, _ []string) string {
	var list []struct {
		Name      string `json:"name"`
		Installed string `json:"installed"`
		Binary    string `json:"binary"`
		Install   *struct {
			State string `json:"state"`
			Error string `json:"error"`
		} `json:"install"`
	}
	if e := b.api(ctx, http.MethodGet, "/api/v1/cores", nil, &list); e != "" {
		return e
	}
	var s strings.Builder
	for _, c := range list {
		v := firstOf(c.Installed, "не установлено")
		fmt.Fprintf(&s, "%s: %s", c.Name, v)
		if c.Install != nil && c.Install.State != "idle" {
			fmt.Fprintf(&s, " (установка: %s %s)", c.Install.State, c.Install.Error)
		}
		s.WriteString("\n")
	}
	s.WriteString("Сменить версию: /versions sing-box, /core xray 26.10.10")
	return s.String()
}

func cmdVersions(b *Bot, ctx context.Context, args []string) string {
	if len(args) != 1 {
		return usage("versions")
	}
	var rels []struct {
		Version    string `json:"version"`
		Prerelease bool   `json:"prerelease"`
	}
	if e := b.api(ctx, http.MethodGet, "/api/v1/cores/"+url.PathEscape(args[0])+"/versions", nil, &rels); e != "" {
		return e
	}
	var s strings.Builder
	for i, r := range rels {
		if i == 15 {
			break
		}
		s.WriteString(r.Version)
		if r.Prerelease {
			s.WriteString(" (бета)")
		}
		s.WriteString("\n")
	}
	return s.String()
}

func cmdCoreInstall(b *Bot, ctx context.Context, args []string) string {
	if len(args) != 2 {
		return usage("core")
	}
	if e := b.api(ctx, http.MethodPost, "/api/v1/cores/"+url.PathEscape(args[0])+"/install", map[string]string{"version": args[1]}, nil); e != "" {
		return e
	}
	return "Устанавливаю " + args[0] + " " + args[1] + ". Проверить — /cores"
}

func cmdPost(path, ok string) func(b *Bot, ctx context.Context, _ []string) string {
	return func(b *Bot, ctx context.Context, _ []string) string {
		if e := b.api(ctx, http.MethodPost, path, nil, nil); e != "" {
			return e
		}
		return ok
	}
}

func cmdLogs(b *Bot, ctx context.Context, args []string) string {
	n := "20"
	if len(args) == 1 {
		if _, err := fmt.Sscanf(args[0], "%d", new(int)); err == nil {
			n = args[0]
		}
	}
	var entries []struct {
		Time    string `json:"time"`
		Level   string `json:"level"`
		Message string `json:"message"`
	}
	if e := b.api(ctx, http.MethodGet, "/api/v1/logs?level=warn&n="+n, nil, &entries); e != "" {
		return e
	}
	if len(entries) == 0 {
		return "Предупреждений и ошибок нет"
	}
	var s strings.Builder
	for _, e := range entries {
		t := e.Time
		if len(t) >= 19 {
			t = t[11:19]
		}
		fmt.Fprintf(&s, "%s %s %s\n", t, e.Level, e.Message)
	}
	return s.String()
}

func cmdCheck(b *Bot, ctx context.Context, args []string) string {
	if len(args) != 1 {
		return usage("check")
	}
	var res interface{}
	if e := b.api(ctx, http.MethodGet, "/api/v1/check/"+url.PathEscape(args[0]), nil, &res); e != "" {
		return e
	}
	out, _ := json.MarshalIndent(res, "", " ")
	return string(out)
}

// cmdSet changes one setting; the value is JSON when it parses as JSON
// (true, 6, ["1.1.1.1"]) and a string otherwise ("6h").
func cmdSet(b *Bot, ctx context.Context, args []string) string {
	if len(args) < 2 {
		return usage("set")
	}
	raw := strings.Join(args[1:], " ")
	var v interface{}
	if json.Unmarshal([]byte(raw), &v) != nil {
		v = raw
	}
	if e := b.api(ctx, http.MethodPatch, "/api/v1/settings", map[string]interface{}{args[0]: v}, nil); e != "" {
		return e
	}
	return args[0] + " изменён"
}

func cmdSMS(b *Bot, _ context.Context, _ []string) string {
	if len(b.o.SMS) == 0 {
		return "Модем с SMS не найден"
	}
	recent := b.Recent()
	if len(recent) == 0 {
		return "SMS не приходили"
	}
	var s strings.Builder
	for _, m := range recent {
		s.WriteString(formatSMS(m) + "\n\n")
	}
	return s.String()
}

func cmdSendSMS(b *Bot, ctx context.Context, args []string) string {
	if len(args) < 2 {
		return usage("sendsms")
	}
	if err := b.SendSMS(ctx, args[0], strings.Join(args[1:], " ")); err != nil {
		return "Ошибка: " + err.Error()
	}
	return "SMS отправлено на " + args[0]
}
