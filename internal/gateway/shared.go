package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/madkoding/kitties/internal/artifacts"
	"github.com/madkoding/kitties/internal/kitty"
	"github.com/madkoding/kitties/internal/shared"
)

// WHAT THE KITTIES SHARE, AND WHAT THEY KEEP.
//
// Each kitty's ~/Shared is a folder of the host: the page lists every kitty's files together
// (GET /v1/shared), opens one (GET /v1/kitties/{ref}/shared/{path}), deletes one, or keeps a copy
// in the library (POST /v1/kitties/{ref}/keep/{path}), where every kitty can read it.
//
// A kitty's files (home, workspace, Shared) are exported as a .tar.gz without its keys and
// credentials (GET /v1/kitties/{ref}/export) and put back from one (POST /v1/kitties/{ref}/import).

// Archiver exports and imports a kitty's files.
type Archiver interface {
	Export(ref string, w io.Writer) error
	Import(ctx context.Context, ref string, r io.Reader) error
}

// WithShared serves the kitties' Shared folders, each up to quotaMB (0: no limit), and, when
// the fleet can, the export and import of their files.
func (s *Server) WithShared(st *shared.Store, quotaMB int) *Server {
	s.shared, s.sharedQuota = st, quotaMB
	return s
}

var errNoShared = errors.New("this gateway has no Shared folders")

// maxImport is the biggest export a person can put back (gzip'd).
var maxImport int64 = 4 << 30

func (s *Server) sharedRoutes() {
	api := s.auth
	s.mux.Handle("GET /v1/shared", api(s.handleSharedFiles))
	s.mux.Handle("GET /v1/kitties/{ref}/shared/{path...}", api(s.handleSharedFile))
	s.mux.Handle("DELETE /v1/kitties/{ref}/shared/{path...}", api(s.handleDeleteShared))
	s.mux.Handle("POST /v1/kitties/{ref}/keep/{path...}", api(s.handleKeepShared))
	s.mux.Handle("GET /v1/kitties/{ref}/export", api(s.handleExport))
	s.mux.Handle("POST /v1/kitties/{ref}/import", api(s.handleImport))
}

// handleSharedFiles lists every kitty's shared files, the newest first.
func (s *Server) handleSharedFiles(w http.ResponseWriter, _ *http.Request) {
	if s.shared == nil {
		fail(w, http.StatusNotFound, errNoShared)
		return
	}
	var names []string
	for _, k := range s.fleet.List() {
		names = append(names, k.Name)
	}
	reply(w, http.StatusOK, map[string]any{"files": s.shared.List(names), "quota_mb": s.sharedQuota})
}

// sharedKitty is the kitty the request names, and the path inside its Shared folder.
func (s *Server) sharedKitty(w http.ResponseWriter, r *http.Request) (*kitty.Kitty, string, bool) {
	if s.shared == nil {
		fail(w, http.StatusNotFound, errNoShared)
		return nil, "", false
	}
	k, err := s.fleet.Get(r.PathValue("ref"))
	if err != nil {
		fail(w, codeFor(err), err)
		return nil, "", false
	}
	p := r.PathValue("path")
	if !shared.ValidPath(p) {
		fail(w, http.StatusBadRequest, errors.New("not a file of the Shared folder"))
		return nil, "", false
	}
	return k, p, true
}

func sharedCode(err error) int {
	if errors.Is(err, shared.ErrNotFound) {
		return http.StatusNotFound
	}
	return http.StatusInternalServerError
}

// handleSharedFile sends a shared file. The page shows it, so it is never run: a document that
// could hold a script (HTML, SVG) comes sandboxed, and nothing is guessed from its bytes.
func (s *Server) handleSharedFile(w http.ResponseWriter, r *http.Request) {
	k, p, ok := s.sharedKitty(w, r)
	if !ok {
		return
	}
	f, info, err := s.shared.Open(k.Name, p)
	if err != nil {
		fail(w, sharedCode(err), err)
		return
	}
	defer f.Close()
	name := path.Base(p)
	disposition := "inline"
	if r.URL.Query().Get("download") == "1" {
		disposition = "attachment"
	}
	h := w.Header()
	h.Set("Content-Type", artifacts.Type(name))
	h.Set("Content-Disposition", disposition+"; filename=\""+strings.ReplaceAll(name, "\"", "")+"\"")
	h.Set("Content-Security-Policy", "sandbox")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	_, _ = io.Copy(w, f)
}

func (s *Server) handleDeleteShared(w http.ResponseWriter, r *http.Request) {
	k, p, ok := s.sharedKitty(w, r)
	if !ok {
		return
	}
	if err := s.shared.Delete(k.Name, p); err != nil {
		fail(w, sharedCode(err), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleKeepShared copies a shared file into the library, saved by its kitty.
func (s *Server) handleKeepShared(w http.ResponseWriter, r *http.Request) {
	k, p, ok := s.sharedKitty(w, r)
	if !ok {
		return
	}
	if s.arts == nil {
		fail(w, http.StatusNotFound, errors.New("this gateway has no library"))
		return
	}
	f, _, err := s.shared.Open(k.Name, p)
	if err != nil {
		fail(w, sharedCode(err), err)
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 9<<20))
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	a, err := s.arts.Put(path.Base(p), data, k.Name)
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"artifact": a})
}

func (s *Server) archiver(w http.ResponseWriter) Archiver {
	a, ok := s.maintainer.(Archiver)
	if !ok {
		fail(w, http.StatusNotFound, errors.New("this gateway cannot export or import a kitty's files"))
	}
	return a
}

// handleExport sends a kitty's files as a .tar.gz, its keys and credentials left out.
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	a := s.archiver(w)
	if a == nil {
		return
	}
	k, err := s.fleet.Get(r.PathValue("ref"))
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	if !k.Desktop {
		fail(w, http.StatusBadRequest, errors.New(k.Name+" has no desktop"))
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", "attachment; filename=\"kitty-"+k.Name+".tar.gz\"")
	w.Header().Set("Cache-Control", "no-store")
	// The answer has begun: a failure now can only cut the file short.
	_ = a.Export(k.ID, w)
}

// handleImport puts back a kitty's files from a .tar.gz the export made.
func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	a := s.archiver(w)
	if a == nil {
		return
	}
	body := http.MaxBytesReader(w, r.Body, maxImport)
	if err := a.Import(r.Context(), r.PathValue("ref"), body); err != nil {
		var tooBig *http.MaxBytesError
		switch {
		case errors.As(err, &tooBig):
			fail(w, http.StatusRequestEntityTooLarge, errors.New("the file is too big"))
		case strings.Contains(err.Error(), "start the desktop"):
			fail(w, http.StatusConflict, err)
		default:
			fail(w, codeFor(err), err)
		}
		return
	}
	reply(w, http.StatusOK, map[string]any{"imported": true})
}
