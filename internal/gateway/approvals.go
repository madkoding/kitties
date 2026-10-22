package gateway

import (
	"context"
	"net/http"

	"github.com/madkoding/kitties/internal/approvals"
)

// Approver is what the gateway needs from whoever holds actions for the person's yes.
type Approver interface {
	ListApprovals(status string) ([]approvals.Approval, error)
	DecideApproval(ctx context.Context, id string, approve bool, reason string, always bool) (*approvals.Approval, error)
	ApprovalRules() ([]approvals.Rule, error)
	RemoveApprovalRule(id string) error
}

// WithApprovals serves the actions held for approval.
func (s *Server) WithApprovals(a Approver) *Server {
	s.approver = a
	return s
}

func (s *Server) approvalRoutes() {
	api := s.auth
	s.mux.Handle("GET /v1/approvals", api(s.handleApprovals))
	s.mux.Handle("GET /v1/approvals/rules", api(s.handleApprovalRules))
	s.mux.Handle("DELETE /v1/approvals/rules/{id}", api(s.handleRemoveApprovalRule))
	s.mux.Handle("POST /v1/approvals/{id}/approve", api(s.handleApprove))
	s.mux.Handle("POST /v1/approvals/{id}/reject", api(s.handleReject))
}

func (s *Server) handleApprovals(w http.ResponseWriter, r *http.Request) {
	if s.approver == nil {
		fail(w, http.StatusNotFound, errNoApprovals)
		return
	}
	list, err := s.approver.ListApprovals(r.URL.Query().Get("status"))
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	if list == nil {
		list = []approvals.Approval{}
	}
	reply(w, http.StatusOK, map[string]any{"approvals": list})
}

func (s *Server) handleApprovalRules(w http.ResponseWriter, _ *http.Request) {
	if s.approver == nil {
		fail(w, http.StatusNotFound, errNoApprovals)
		return
	}
	rules, err := s.approver.ApprovalRules()
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	if rules == nil {
		rules = []approvals.Rule{}
	}
	reply(w, http.StatusOK, map[string]any{"rules": rules})
}

func (s *Server) handleRemoveApprovalRule(w http.ResponseWriter, r *http.Request) {
	if s.approver == nil {
		fail(w, http.StatusNotFound, errNoApprovals)
		return
	}
	if err := s.approver.RemoveApprovalRule(r.PathValue("id")); err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	s.decide(w, r, true)
}

func (s *Server) handleReject(w http.ResponseWriter, r *http.Request) {
	s.decide(w, r, false)
}

func (s *Server) decide(w http.ResponseWriter, r *http.Request, approve bool) {
	if s.approver == nil {
		fail(w, http.StatusNotFound, errNoApprovals)
		return
	}
	var body struct {
		Reason string `json:"reason"`
		Always bool   `json:"always"`
	}
	if err := decode(r, &body); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	ap, err := s.approver.DecideApproval(r.Context(), r.PathValue("id"), approve, body.Reason, body.Always)
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"approval": ap})
}
