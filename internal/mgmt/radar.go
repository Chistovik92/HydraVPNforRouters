package mgmt

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/Chistovik92/hydravpn-router/internal/radar"
)

// radarSyncEvery is how often a linked router re-reads its subscriptions
// from the bot. Access is issued in the bot, so a new subscription shows up
// here without anyone touching the router.
var radarSyncEvery = 12 * time.Hour

// errRadarUnlinked means the bot no longer accepts this device: it was
// disconnected there. The link is dropped; a new code is needed.
var errRadarUnlinked = errors.New("this router was disconnected in the bot; link it again with a new code")

func (s *Server) radarClient() *radar.Client {
	if s.opts.Radar != nil {
		return s.opts.Radar
	}
	return radar.NewClient()
}

// radarSync reads the subscriptions and puts the new ones into the config.
func (s *Server) radarSync(ctx context.Context, st *radar.State, section string) (radar.Result, error) {
	subs, err := s.radarClient().Subscriptions(ctx, st.Server, st.Token)
	if err != nil {
		if radar.IsAuth(err) {
			_ = radar.ClearState(s.opts.RuntimeDir)
			return radar.Result{}, errRadarUnlinked
		}
		return radar.Result{}, err
	}
	var res radar.Result
	err = s.mutate(func(c *config.Config) error {
		name, err := radar.PickSection(c, section)
		if err != nil {
			return err
		}
		add, r := radar.Plan(c.SubscriptionURLs, name, subs)
		res = r
		c.SubscriptionURLs = append(c.SubscriptionURLs, add...)
		return nil
	})
	return res, err
}

func (s *Server) radarLoop(stop <-chan struct{}) {
	ticker := time.NewTicker(radarSyncEvery)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
		st, err := radar.LoadState(s.opts.RuntimeDir)
		if err != nil || st == nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		res, err := s.radarSync(ctx, st, "")
		cancel()
		switch {
		case err == nil && res.Added > 0:
			s.log("info", "bot account: %d new subscription(s) added to section %q", res.Added, res.Section)
		case err != nil && !errors.Is(err, errApplying):
			s.log("warn", "bot account sync failed: %v", err)
		}
	}
}

func (s *Server) handleRadarStatus(w http.ResponseWriter, r *http.Request) {
	st, err := radar.LoadState(s.opts.RuntimeDir)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	if st == nil {
		writeJSON(w, 200, map[string]interface{}{"linked": false})
		return
	}
	// The token is never returned.
	writeJSON(w, 200, map[string]interface{}{
		"linked": true, "server": st.Server, "username": st.Username, "linked_at": st.Linked,
	})
}

func (s *Server) handleRadarLink(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Server  string `json:"server"`
		Code    string `json:"code"`
		Section string `json:"section"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	server, err := radar.NormalizeServer(req.Server)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	if req.Code == "" {
		writeErr(w, 400, errors.New("code is required"))
		return
	}
	host, _ := os.Hostname()
	if host == "" {
		host = "router"
	}
	ctx := r.Context()
	token, err := s.radarClient().Link(ctx, server, req.Code, host)
	if err != nil {
		writeErr(w, radarStatus(err), err)
		return
	}
	name, _ := s.radarClient().Username(ctx, server, token)
	st := &radar.State{Server: server, Token: token, Username: name, Linked: time.Now().UTC()}
	if err := radar.SaveState(s.opts.RuntimeDir, st); err != nil {
		// The device is linked in the bot but the token is lost: disconnect it there.
		_ = s.radarClient().Logout(ctx, server, token)
		writeErr(w, 500, err)
		return
	}
	s.log("info", "router linked to a bot account")
	res, err := s.radarSync(ctx, st, req.Section)
	s.radarAnswer(w, res, err, 201, "linked")
}

func (s *Server) handleRadarSync(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Section string `json:"section"`
	}
	if r.ContentLength != 0 {
		if err := readJSON(r, &req); err != nil {
			writeErr(w, 400, err)
			return
		}
	}
	st, err := radar.LoadState(s.opts.RuntimeDir)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	if st == nil {
		writeErr(w, 409, errors.New("not linked to a bot account"))
		return
	}
	res, err := s.radarSync(r.Context(), st, req.Section)
	s.radarAnswer(w, res, err, 200, "synced")
}

func (s *Server) handleRadarUnlink(w http.ResponseWriter, r *http.Request) {
	st, err := radar.LoadState(s.opts.RuntimeDir)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	if st != nil {
		// Best effort: if the bot is unreachable the device is forgotten here anyway,
		// and the person can disconnect it in the bot.
		_ = s.radarClient().Logout(r.Context(), st.Server, st.Token)
	}
	if err := radar.ClearState(s.opts.RuntimeDir); err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "unlinked"})
}

func (s *Server) radarAnswer(w http.ResponseWriter, res radar.Result, err error, okCode int, status string) {
	switch {
	case err == nil:
		writeJSON(w, okCode, map[string]interface{}{"status": status, "result": res})
	case errors.Is(err, errApplying):
		writeJSON(w, http.StatusAccepted, map[string]interface{}{"status": "applying", "result": res})
	case errors.Is(err, errRadarUnlinked):
		writeErr(w, 409, err)
	default:
		writeErr(w, radarStatus(err), err)
	}
}

// radarStatus maps a bot failure to a status of this API. 401 is not passed
// through: here it would read as "wrong router token". A code the bot does not
// accept is 422; an unreachable or broken bot is a bad gateway.
func radarStatus(err error) int {
	var e *radar.Error
	if errors.As(err, &e) {
		switch e.Code {
		case 401:
			return http.StatusUnprocessableEntity
		case 429:
			return http.StatusTooManyRequests
		}
	}
	return http.StatusBadGateway
}
