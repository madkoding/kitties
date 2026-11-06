package gateway

import (
	"context"
	"errors"
	"net/http"

	"github.com/madkoding/kitties/internal/whatsapp"
)

// WhatsApp is the link to a WhatsApp number as a linked device (internal/whatsapp), as Settings
// shows and changes it.
type WhatsApp interface {
	Status() whatsapp.Status
	Apply(whatsapp.Settings) error
	Link() error
	Reconnect() error
	Unlink(ctx context.Context) error
	Test(ctx context.Context, to string) error
}

// WithWhatsApp lets Settings link a WhatsApp number and choose who may write.
func (s *Server) WithWhatsApp(w WhatsApp) *Server {
	s.whatsapp = w
	return s
}

func (s *Server) whatsappRoutes() {
	s.mux.Handle("GET /v1/whatsapp", s.auth(s.handleWhatsApp))
	s.mux.Handle("PUT /v1/whatsapp", s.auth(s.handleSetWhatsApp))
	s.mux.Handle("POST /v1/whatsapp/link", s.auth(s.handleWhatsAppLink))
	s.mux.Handle("POST /v1/whatsapp/reconnect", s.auth(s.handleWhatsAppReconnect))
	s.mux.Handle("POST /v1/whatsapp/unlink", s.auth(s.handleWhatsAppUnlink))
	s.mux.Handle("POST /v1/whatsapp/test", s.auth(s.handleWhatsAppTest))
}

var errNoWhatsApp = errors.New("this gateway has no WhatsApp link")

// handleWhatsApp is the link now, the QR code to scan included while it waits for one.
func (s *Server) handleWhatsApp(w http.ResponseWriter, _ *http.Request) {
	if s.whatsapp == nil {
		fail(w, http.StatusNotFound, errNoWhatsApp)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	reply(w, http.StatusOK, s.whatsapp.Status())
}

// handleSetWhatsApp keeps the settings. WhatsApp Web is not an official API and the number can
// be banned, so turning it on must be accepted explicitly (accept_risk).
func (s *Server) handleSetWhatsApp(w http.ResponseWriter, r *http.Request) {
	if s.whatsapp == nil {
		fail(w, http.StatusNotFound, errNoWhatsApp)
		return
	}
	var in struct {
		whatsapp.Settings
		AcceptRisk bool `json:"accept_risk"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if in.Enabled && !s.whatsapp.Status().Enabled && !in.AcceptRisk {
		fail(w, http.StatusBadRequest, errors.New("WhatsApp Web is not an official API and WhatsApp may ban the number: confirm with accept_risk"))
		return
	}
	if err := s.whatsapp.Apply(in.Settings); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	reply(w, http.StatusOK, s.whatsapp.Status())
}

// handleWhatsAppLink asks for new QR codes.
func (s *Server) handleWhatsAppLink(w http.ResponseWriter, _ *http.Request) {
	if s.whatsapp == nil {
		fail(w, http.StatusNotFound, errNoWhatsApp)
		return
	}
	if err := s.whatsapp.Link(); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	reply(w, http.StatusOK, s.whatsapp.Status())
}

// handleWhatsAppReconnect connects again with the kept session, with no new QR code.
func (s *Server) handleWhatsAppReconnect(w http.ResponseWriter, _ *http.Request) {
	if s.whatsapp == nil {
		fail(w, http.StatusNotFound, errNoWhatsApp)
		return
	}
	if err := s.whatsapp.Reconnect(); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	reply(w, http.StatusOK, s.whatsapp.Status())
}

// handleWhatsAppUnlink removes the device from the phone and forgets its session.
func (s *Server) handleWhatsAppUnlink(w http.ResponseWriter, r *http.Request) {
	if s.whatsapp == nil {
		fail(w, http.StatusNotFound, errNoWhatsApp)
		return
	}
	if err := s.whatsapp.Unlink(r.Context()); err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	reply(w, http.StatusOK, s.whatsapp.Status())
}

// handleWhatsAppTest sends a short message to a number on the list.
func (s *Server) handleWhatsAppTest(w http.ResponseWriter, r *http.Request) {
	if s.whatsapp == nil {
		fail(w, http.StatusNotFound, errNoWhatsApp)
		return
	}
	var in struct {
		To string `json:"to"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if err := s.whatsapp.Test(r.Context(), in.To); err != nil {
		reply(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	reply(w, http.StatusOK, map[string]any{"ok": true})
}
