package mgmt

import (
	"context"
	"errors"
	"net/http"

	"github.com/Chistovik92/hydravpn-router/internal/sms"
)

// SMSBox gives the API the SMS of the router's modem (the Telegram bot keeps
// the recent ones and sends through the modem).
type SMSBox interface {
	Recent() []sms.Message
	Modems() []string
	SendSMS(ctx context.Context, to, text string) error
}

func (s *Server) smsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/sms", s.handleSMSList)
	mux.HandleFunc("POST /api/v1/sms", s.handleSMSSend)
}

func (s *Server) handleSMSList(w http.ResponseWriter, r *http.Request) {
	if s.opts.SMS == nil {
		writeJSON(w, 200, map[string]interface{}{"modems": []string{}, "messages": []sms.Message{}})
		return
	}
	msgs := s.opts.SMS.Recent()
	if msgs == nil {
		msgs = []sms.Message{}
	}
	writeJSON(w, 200, map[string]interface{}{"modems": s.opts.SMS.Modems(), "messages": msgs})
}

func (s *Server) handleSMSSend(w http.ResponseWriter, r *http.Request) {
	if s.opts.SMS == nil {
		writeErr(w, http.StatusNotImplemented, sms.ErrNoModem)
		return
	}
	var req struct {
		To   string `json:"to"`
		Text string `json:"text"`
	}
	if err := readJSON(r, &req); err != nil || req.To == "" || req.Text == "" {
		writeErr(w, 400, errors.New(`{"to":"+7…","text":"…"} is required`))
		return
	}
	if err := s.opts.SMS.SendSMS(r.Context(), req.To, req.Text); err != nil {
		writeErr(w, 502, err)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "sent"})
}
