package gateway

import (
	"errors"
	"net/http"

	"github.com/madkoding/kitties/internal/vault"
)

// Vault is what the gateway needs from the store of secrets a kitty may type without reading.
type Vault interface {
	List() []vault.Info
	Put(name, value string, kitties []string) error
	SetKitties(name string, kitties []string) error
	Delete(name string) error
}

// WithVault serves the secrets: their names and who may use them, never their values.
func (s *Server) WithVault(v Vault) *Server {
	s.vault = v
	return s
}

var errNoVault = errors.New("this gateway keeps no saved secrets")

func (s *Server) vaultRoutes() {
	api := s.auth
	s.mux.Handle("GET /v1/vault", api(s.handleVault))
	s.mux.Handle("PUT /v1/vault/{name}", api(s.handleSetSecret))
	s.mux.Handle("DELETE /v1/vault/{name}", api(s.handleDeleteSecret))
}

func (s *Server) handleVault(w http.ResponseWriter, _ *http.Request) {
	if s.vault == nil {
		fail(w, http.StatusNotFound, errNoVault)
		return
	}
	reply(w, http.StatusOK, map[string]any{"secrets": s.vault.List()})
}

// handleSetSecret saves a secret (with its value) or, without one, changes which kitties may
// use it. Kitties are named by id or by name.
func (s *Server) handleSetSecret(w http.ResponseWriter, r *http.Request) {
	if s.vault == nil {
		fail(w, http.StatusNotFound, errNoVault)
		return
	}
	var body struct {
		Value   string   `json:"value"`
		Kitties []string `json:"kitties"`
	}
	if err := decode(r, &body); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	ids := make([]string, 0, len(body.Kitties))
	for _, ref := range body.Kitties {
		k, err := s.fleet.Get(ref)
		if err != nil {
			fail(w, codeFor(err), err)
			return
		}
		ids = append(ids, k.ID)
	}
	var err error
	if body.Value != "" {
		err = s.vault.Put(r.PathValue("name"), body.Value, ids)
	} else {
		err = s.vault.SetKitties(r.PathValue("name"), ids)
	}
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"secrets": s.vault.List()})
}

func (s *Server) handleDeleteSecret(w http.ResponseWriter, r *http.Request) {
	if s.vault == nil {
		fail(w, http.StatusNotFound, errNoVault)
		return
	}
	if err := s.vault.Delete(r.PathValue("name")); err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"secrets": s.vault.List()})
}
