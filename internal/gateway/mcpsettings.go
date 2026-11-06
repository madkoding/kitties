package gateway

import (
	"errors"
	"net/http"
	"time"

	"github.com/madkoding/kitties/internal/config"
)

// MCPInfo is the connector as Settings shows it. It never carries the token: the command reads it
// from its file, and the token itself leaves only through GET /v1/mcp/token, an explicit action.
type MCPInfo struct {
	config.MCPSettings
	// URL is where /mcp answers; TokenFile where its token is kept.
	URL       string `json:"url"`
	TokenFile string `json:"token_file"`
	// Command is the `claude mcp add` line for it (it reads the token file, it does not contain it).
	Command string `json:"command"`
	// TokenManaged is true when the token comes from the configuration or the environment, so it
	// cannot be replaced from here.
	TokenManaged bool   `json:"token_managed"`
	CanChange    bool   `json:"can_change"`
	Why          string `json:"why,omitempty"`
}

// MCPControl is what the gateway needs to show and change the connector; Relaunch restarts it so
// the change takes effect.
type MCPControl interface {
	MCP() MCPInfo
	SetMCP(config.MCPSettings) error
	Token() (string, error)
	RotateToken() error
	Relaunch() error
}

// WithMCPControl lets Settings read and change the MCP connector.
func (s *Server) WithMCPControl(c MCPControl) *Server {
	s.mcpControl = c
	return s
}

func (s *Server) mcpControlRoutes() {
	s.mux.Handle("GET /v1/mcp", s.auth(s.handleMCP))
	s.mux.Handle("PUT /v1/mcp", s.auth(s.handleSetMCP))
	s.mux.Handle("GET /v1/mcp/token", s.auth(s.handleMCPToken))
	s.mux.Handle("POST /v1/mcp/token", s.auth(s.handleRotateMCPToken))
}

var errNoMCPControl = errors.New("this gateway cannot change the MCP connector")

func (s *Server) handleMCP(w http.ResponseWriter, _ *http.Request) {
	if s.mcpControl == nil {
		fail(w, http.StatusNotFound, errNoMCPControl)
		return
	}
	reply(w, http.StatusOK, s.mcpControl.MCP())
}

// handleSetMCP keeps the switches and restarts once it has answered. Exec and approve give Claude
// real power, so turning either on must be accepted explicitly (accept_risk).
func (s *Server) handleSetMCP(w http.ResponseWriter, r *http.Request) {
	if s.mcpControl == nil {
		fail(w, http.StatusNotFound, errNoMCPControl)
		return
	}
	var in struct {
		config.MCPSettings
		AcceptRisk bool `json:"accept_risk"`
	}
	if err := decode(r, &in); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	cur := s.mcpControl.MCP()
	if in.MCPSettings == cur.MCPSettings {
		reply(w, http.StatusOK, map[string]any{"restarting": false, "mcp": cur})
		return
	}
	if ((in.AllowExec && !cur.AllowExec) || (in.AllowApprove && !cur.AllowApprove)) && !in.AcceptRisk {
		fail(w, http.StatusBadRequest, errors.New("allow_exec and allow_approve let Claude run commands and say yes for you: confirm with accept_risk"))
		return
	}
	if err := s.mcpControl.SetMCP(in.MCPSettings); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	reply(w, http.StatusAccepted, map[string]any{"restarting": true})
	s.relaunchSoon(s.mcpControl.Relaunch)
}

// handleMCPToken hands out the connector's token, for the copy button: the person asked for it.
func (s *Server) handleMCPToken(w http.ResponseWriter, _ *http.Request) {
	if s.mcpControl == nil {
		fail(w, http.StatusNotFound, errNoMCPControl)
		return
	}
	tok, err := s.mcpControl.Token()
	if err != nil {
		fail(w, http.StatusNotFound, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	reply(w, http.StatusOK, map[string]string{"token": tok})
}

// handleRotateMCPToken replaces the connector's token and restarts: every connection that used the
// old one (OAuth clients too) stops working.
func (s *Server) handleRotateMCPToken(w http.ResponseWriter, _ *http.Request) {
	if s.mcpControl == nil {
		fail(w, http.StatusNotFound, errNoMCPControl)
		return
	}
	if err := s.mcpControl.RotateToken(); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	reply(w, http.StatusAccepted, map[string]any{"restarting": true})
	s.relaunchSoon(s.mcpControl.Relaunch)
}

func (s *Server) relaunchSoon(relaunch func() error) {
	go func() {
		time.Sleep(restartAfter)
		_ = relaunch()
	}()
}
