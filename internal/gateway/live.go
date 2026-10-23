package gateway

import (
	"bufio"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/madkoding/kitties/internal/container"
)

// THE DISPLAY, LIVE, AND ITS CLIPBOARD.
//
// GET /v1/kitties/{ref}/stream is the kitty's display as motion JPEG (multipart/x-mixed-replace):
// an <img> shows it as it changes, at up to ?fps= pictures a second (8 by default). The
// clipboard goes both ways: GET takes the kitty's text, PUT gives it the person's.

func (s *Server) liveRoutes() {
	api := s.auth
	s.mux.Handle("GET /v1/kitties/{ref}/stream", api(s.handleStream))
	s.mux.Handle("GET /v1/kitties/{ref}/clipboard", api(s.handleClipboard))
	s.mux.Handle("PUT /v1/kitties/{ref}/clipboard", api(s.handleSetClipboard))
}

// streamBoundary separates the pictures of the stream.
const streamBoundary = "kittyframe"

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	fps := bounded(r.URL.Query().Get("fps"), 8, 1, 15)
	quality := bounded(r.URL.Query().Get("quality"), 70, 30, 95)
	flusher, ok := w.(http.Flusher)
	if !ok {
		fail(w, http.StatusInternalServerError, errors.New("streaming is not supported here"))
		return
	}
	rc, err := s.fleet.Stream(r.Context(), r.PathValue("ref"), fps, quality)
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	defer rc.Close()
	frames := bufio.NewReaderSize(rc, 256<<10)
	first, err := container.ReadFrame(frames)
	if err != nil {
		fail(w, http.StatusBadGateway, fmt.Errorf("the desktop did not answer: %w", err))
		return
	}
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary="+streamBoundary)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	// Each picture is followed at once by the boundary of the next: a browser shows a part only
	// when it sees where the part ends, and a still display sends no next picture for a while.
	if _, err := fmt.Fprintf(w, "--%s\r\n", streamBoundary); err != nil {
		return
	}
	for frame := first; ; {
		if _, err := fmt.Fprintf(w, "Content-Type: %s\r\nContent-Length: %d\r\n\r\n", http.DetectContentType(frame), len(frame)); err != nil {
			return
		}
		if _, err := w.Write(frame); err != nil {
			return
		}
		if _, err := fmt.Fprintf(w, "\r\n--%s\r\n", streamBoundary); err != nil {
			return
		}
		flusher.Flush()
		if frame, err = container.ReadFrame(frames); err != nil {
			return
		}
	}
}

// bounded is a number of the query, or def when it is missing or out of [lo, hi].
func bounded(v string, def, lo, hi int) int {
	n, err := strconv.Atoi(v)
	if err != nil || n < lo || n > hi {
		return def
	}
	return n
}

func (s *Server) handleClipboard(w http.ResponseWriter, r *http.Request) {
	text, err := s.fleet.Clipboard(r.Context(), r.PathValue("ref"))
	if err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"text": text})
}

func (s *Server) handleSetClipboard(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text string `json:"text"`
	}
	if err := decode(r, &body); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if err := s.fleet.SetClipboard(r.Context(), r.PathValue("ref"), body.Text); err != nil {
		fail(w, codeFor(err), err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"ok": true})
}
