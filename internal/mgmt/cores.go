package mgmt

import (
	"context"
	"errors"
	"net/http"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/Chistovik92/hydravpn-router/internal/components"
	"github.com/Chistovik92/hydravpn-router/internal/config"
	"github.com/Chistovik92/hydravpn-router/internal/cores"
)

// CoreInstaller downloads and switches core versions (cores.Installer).
type CoreInstaller interface {
	Releases(ctx context.Context, k cores.Kind) ([]cores.Release, error)
	Start(k cores.Kind, version string) error
	Status() map[cores.Kind]cores.InstallStatus
	Versions(k cores.Kind) []string
	Link(k cores.Kind) string
	SetInstalled(fn func(k cores.Kind, version, bin string))
}

// coreRefresher is implemented by the engine: re-render sing-box and the
// core processes even when the config did not change (a version switched
// behind the same link).
type coreRefresher interface{ RefreshCores() error }

func (s *Server) coreRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/cores", s.handleCores)
	mux.HandleFunc("GET /api/v1/cores/{name}/versions", s.handleCoreVersions)
	mux.HandleFunc("POST /api/v1/cores/{name}/install", s.handleCoreInstall)
	if s.opts.Cores != nil {
		s.opts.Cores.SetInstalled(s.coreInstalled)
	}
}

// coreInstalled switches the service to a version that became active.
func (s *Server) coreInstalled(k cores.Kind, version, bin string) {
	var err error
	if k == cores.SingBox && s.opts.Engine.GetConfig().Settings.SingBoxBinary != bin {
		// Point the service at the managed link once; later switches only
		// move the link.
		err = s.mutate(func(c *config.Config) error {
			c.Settings.SingBoxBinary = bin
			return nil
		})
	} else if r, ok := s.opts.Engine.(coreRefresher); ok {
		err = r.RefreshCores()
	}
	if err != nil && !errors.Is(err, errApplying) {
		s.log("error", "switching to %s %s: %v", k, version, err)
	}
}

type coreInfo struct {
	Name       cores.Kind           `json:"name"`
	Binary     string               `json:"binary,omitempty"`
	Installed  string               `json:"installed,omitempty"`
	Managed    bool                 `json:"managed"`
	Selectable bool                 `json:"selectable"`
	Kept       []string             `json:"kept_versions,omitempty"`
	Install    *cores.InstallStatus `json:"install,omitempty"`
}

// handleCores lists sing-box and the node cores: where each one is, its
// version, the versions kept for a rollback and the install progress.
func (s *Server) handleCores(w http.ResponseWriter, r *http.Request) {
	st := s.opts.Engine.GetConfig().Settings
	configured := map[cores.Kind]string{cores.SingBox: st.SingBoxBinary, cores.Xray: st.XrayBinary,
		cores.OpenFlux: st.OpenFluxBinary, cores.OlcRTC: st.OlcRTCBinary, cores.AmneziaWG: st.AmneziaWGBinary}
	var progress map[cores.Kind]cores.InstallStatus
	if s.opts.Cores != nil {
		progress = s.opts.Cores.Status()
	}
	out := []coreInfo{}
	for _, k := range append([]cores.Kind{cores.SingBox}, cores.Kinds...) {
		info := coreInfo{Name: k, Binary: configured[k]}
		link := ""
		if s.opts.Cores != nil {
			link = s.opts.Cores.Link(k)
			if p, ok := progress[k]; ok {
				info.Selectable, info.Install = true, &p
				info.Kept = s.opts.Cores.Versions(k)
			}
		}
		if k != cores.SingBox {
			info.Binary = cores.FindBinary(k, configured[k], filepath.Dir(link))
		}
		info.Managed = link != "" && info.Binary != "" && cores.Resolve(info.Binary) == cores.Resolve(link)
		info.Installed = binaryVersion(r.Context(), info.Binary)
		out = append(out, info)
	}
	writeJSON(w, 200, out)
}

// binaryVersion asks a core for its version ("" if it does not run).
func binaryVersion(ctx context.Context, bin string) string {
	if bin == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "version").CombinedOutput()
	if err != nil {
		return ""
	}
	return components.Parse(string(out))
}

func (s *Server) installableCore(w http.ResponseWriter, r *http.Request) (cores.Kind, bool) {
	if s.opts.Cores == nil {
		writeErr(w, http.StatusNotImplemented, errors.New("installing cores is not available here"))
		return "", false
	}
	k := cores.Kind(r.PathValue("name"))
	for _, c := range cores.Installable {
		if c == k {
			return k, true
		}
	}
	writeErr(w, 404, errors.New("the version of this core cannot be chosen (only sing-box and xray)"))
	return "", false
}

func (s *Server) handleCoreVersions(w http.ResponseWriter, r *http.Request) {
	k, ok := s.installableCore(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	rels, err := s.opts.Cores.Releases(ctx, k)
	if err != nil {
		writeErr(w, 502, err)
		return
	}
	writeJSON(w, 200, rels)
}

func (s *Server) handleCoreInstall(w http.ResponseWriter, r *http.Request) {
	k, ok := s.installableCore(w, r)
	if !ok {
		return
	}
	var req struct {
		Version string `json:"version"`
	}
	if err := readJSON(r, &req); err != nil || req.Version == "" {
		writeErr(w, 400, errors.New(`{"version":"x.y.z"} is required`))
		return
	}
	switch err := s.opts.Cores.Start(k, req.Version); {
	case errors.Is(err, cores.ErrInstallBusy):
		writeErr(w, 409, err)
	case err != nil:
		writeErr(w, 400, err)
	default:
		writeJSON(w, 202, map[string]string{"status": "installing"})
	}
}
