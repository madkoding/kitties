package gateway

import (
	"errors"
	"net/http"
	"time"

	"github.com/madkoding/kitties/internal/config"
)

// ExposureInfo is where the gateway can be reached from: Mode is local (this machine alone),
// lan (the local network) or wan (every interface, 0.0.0.0).
type ExposureInfo struct {
	Mode      string `json:"mode"`
	Listen    string `json:"listen"`
	LANIP     string `json:"lan_ip,omitempty"`
	CanChange bool   `json:"can_change"`
	Why       string `json:"why,omitempty"`
}

// Exposing is what the gateway needs to show and change its exposure; Relaunch restarts it so the
// new address takes effect.
type Exposing interface {
	Exposure() ExposureInfo
	SetExposure(mode string) error
	Relaunch() error
}

// WithExposure lets Settings read and change where the gateway listens.
func (s *Server) WithExposure(e Exposing) *Server {
	s.exposure = e
	return s
}

func (s *Server) exposureRoutes() {
	s.mux.Handle("GET /v1/exposure", s.auth(s.handleExposure))
	s.mux.Handle("PUT /v1/exposure", s.auth(s.handleSetExposure))
}

func (s *Server) handleExposure(w http.ResponseWriter, _ *http.Request) {
	if s.exposure == nil {
		fail(w, http.StatusNotFound, errors.New("this gateway cannot change where it listens"))
		return
	}
	reply(w, http.StatusOK, s.exposure.Exposure())
}

// handleSetExposure keeps the choice and restarts once it has answered. Opening to every interface
// must be accepted explicitly (accept_risk): the token travels over plain http unless https is in front.
func (s *Server) handleSetExposure(w http.ResponseWriter, r *http.Request) {
	if s.exposure == nil {
		fail(w, http.StatusNotFound, errors.New("this gateway cannot change where it listens"))
		return
	}
	var in struct {
		Mode       string `json:"mode"`
		AcceptRisk bool   `json:"accept_risk"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if !config.ValidExposure(in.Mode) {
		fail(w, http.StatusBadRequest, errors.New("mode must be local, lan or wan"))
		return
	}
	if in.Mode == config.ExposureWAN && !in.AcceptRisk {
		fail(w, http.StatusBadRequest, errors.New("wan listens on every interface, the internet too when the machine is public: confirm with accept_risk"))
		return
	}
	if cur := s.exposure.Exposure(); cur.Mode == in.Mode {
		reply(w, http.StatusOK, map[string]any{"restarting": false, "exposure": cur})
		return
	}
	if err := s.exposure.SetExposure(in.Mode); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	reply(w, http.StatusAccepted, map[string]any{"restarting": true, "mode": in.Mode})
	go func() {
		time.Sleep(restartAfter)
		_ = s.exposure.Relaunch()
	}()
}
