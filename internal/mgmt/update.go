package mgmt

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/selfupdate"
)

// Updater updates the service binary (see internal/selfupdate).
type Updater interface {
	Status() selfupdate.Status
	Check(ctx context.Context) (selfupdate.Status, error)
	Start(version string) error
}

func (s *Server) updateRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/update", s.handleUpdateStatus)
	mux.HandleFunc("POST /api/v1/update/check", s.handleUpdateCheck)
	mux.HandleFunc("POST /api/v1/update", s.handleUpdateStart)
}

func (s *Server) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	if s.opts.Updater == nil {
		writeErr(w, http.StatusNotImplemented, selfupdate.ErrUnsupported)
		return
	}
	writeJSON(w, 200, s.opts.Updater.Status())
}

func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if s.opts.Updater == nil {
		writeErr(w, http.StatusNotImplemented, selfupdate.ErrUnsupported)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	st, err := s.opts.Updater.Check(ctx)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, st)
		return
	}
	writeJSON(w, 200, st)
}

// handleUpdateStart starts an update in the background and answers 202 at
// once: the service restarts when the new binary is in place. The body is
// optional: {"version":"1.2.4"} picks a release (also an older one to roll
// back); without it the latest release is installed.
func (s *Server) handleUpdateStart(w http.ResponseWriter, r *http.Request) {
	if s.opts.Updater == nil {
		writeErr(w, http.StatusNotImplemented, selfupdate.ErrUnsupported)
		return
	}
	var req struct {
		Version string `json:"version"`
	}
	if err := readJSON(r, &req); err != nil && !errors.Is(err, io.EOF) {
		writeErr(w, 400, err)
		return
	}
	switch err := s.opts.Updater.Start(req.Version); {
	case err == nil:
		writeJSON(w, http.StatusAccepted, s.opts.Updater.Status())
	case errors.Is(err, selfupdate.ErrBusy):
		writeErr(w, http.StatusConflict, err)
	case errors.Is(err, selfupdate.ErrUnsupported):
		writeErr(w, http.StatusNotImplemented, err)
	default:
		writeErr(w, 400, err)
	}
}
