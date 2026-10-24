package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// WatchDesktop follows a kitty's display live (the gateway's motion JPEG) and hands each picture
// to the page as the runtime event "kitties:desktop:<ref>", a data: URL: the webview cannot load
// the gateway's address itself. Watching again restarts the stream.
func (a *App) WatchDesktop(ref string) error {
	return a.watch(ref, "desktop:", "kitties:desktop:", a.streamDesktop)
}

// UnwatchDesktop stops following a kitty's display.
func (a *App) UnwatchDesktop(ref string) { a.unwatch("desktop:" + ref) }

// WatchRects follows a kitty's display as what changed on it (the gateway's /rects, like VNC) and
// hands the page the runtime events "kitties:rects:<ref>": the display's size, and the
// rectangles to draw over the last picture, each a data: URL. Watching again restarts it.
func (a *App) WatchRects(ref string) error {
	return a.watch(ref, "rects:", "kitties:rects:", a.streamRects)
}

// UnwatchRects stops following a kitty's display as rectangles.
func (a *App) UnwatchRects(ref string) { a.unwatch("rects:" + ref) }

func (a *App) unwatch(key string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if cancel, ok := a.watches[key]; ok {
		cancel()
		delete(a.watches, key)
	}
}

// watch runs a stream of a kitty's display until it is stopped, starting it again after a
// pause when it ends, and says why it ended on the event as {"error": ...}.
func (a *App) watch(ref, key, event string, stream func(ctx context.Context, ref string) error) error {
	if strings.ContainsAny(ref, "/?#") || ref == "" {
		return errors.New("not a kitty")
	}
	a.unwatch(key + ref)
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	a.watches[key+ref] = cancel
	a.mu.Unlock()
	go func() {
		for ctx.Err() == nil {
			if err := stream(ctx, ref); err != nil && ctx.Err() == nil {
				if a.emit != nil {
					a.emit(event+ref, map[string]string{"error": err.Error()})
				}
				select {
				case <-ctx.Done():
				case <-time.After(reconnectAfter):
				}
			}
		}
	}()
	return nil
}

// streamRects reads the gateway's stream of rectangles (SCREEN w h, UPDATE n, then n times
// RECT x y w h bytes and the picture) and emits it to the page.
func (a *App) streamRects(ctx context.Context, ref string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.conn.URL+"/v1/kitties/"+ref+"/rects?fps=20", nil)
	if err != nil {
		return err
	}
	if a.conn.Token != "" {
		req.Header.Set("Authorization", "Bearer "+a.conn.Token)
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the gateway answered HTTP %d", resp.StatusCode)
	}
	event := "kitties:rects:" + ref
	r := bufio.NewReaderSize(resp.Body, 256<<10)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return err
		}
		f := strings.Fields(line)
		switch {
		case len(f) == 3 && f[0] == "SCREEN":
			w, h := atoi(f[1]), atoi(f[2])
			if a.emit != nil {
				a.emit(event, map[string]any{"size": []int{w, h}})
			}
		case len(f) == 2 && f[0] == "UPDATE":
			n := atoi(f[1])
			if n < 0 || n > 1024 {
				return errors.New("an update of a size out of bounds")
			}
			var rects []map[string]any
			for i := 0; i < n; i++ {
				head, err := r.ReadString('\n')
				if err != nil {
					return err
				}
				h := strings.Fields(head)
				if len(h) != 6 || h[0] != "RECT" || atoi(h[5]) < 0 || atoi(h[5]) > 8<<20 {
					return fmt.Errorf("not a rectangle: %.40q", head)
				}
				data := make([]byte, atoi(h[5]))
				if _, err := io.ReadFull(r, data); err != nil {
					return err
				}
				rects = append(rects, map[string]any{
					"x": atoi(h[1]), "y": atoi(h[2]), "w": atoi(h[3]), "h": atoi(h[4]),
					"data": "data:" + http.DetectContentType(data) + ";base64," + base64.StdEncoding.EncodeToString(data),
				})
			}
			if a.emit != nil {
				a.emit(event, map[string]any{"rects": rects})
			}
		default:
			return fmt.Errorf("not a stream of rectangles: %.40q", line)
		}
	}
}

// atoi is a number of the stream, or -1 when it is not one.
func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return -1
	}
	return n
}

func (a *App) streamDesktop(ctx context.Context, ref string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.conn.URL+"/v1/kitties/"+ref+"/stream", nil)
	if err != nil {
		return err
	}
	if a.conn.Token != "" {
		req.Header.Set("Authorization", "Bearer "+a.conn.Token)
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the gateway answered HTTP %d", resp.StatusCode)
	}
	_, params, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || params["boundary"] == "" {
		return errors.New("the gateway did not send a stream")
	}
	parts := multipart.NewReader(resp.Body, params["boundary"])
	for {
		p, err := parts.NextPart()
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(p, 8<<20))
		if err != nil {
			return err
		}
		if a.emit != nil {
			a.emit("kitties:desktop:"+ref, map[string]string{
				"frame": "data:" + p.Header.Get("Content-Type") + ";base64," + base64.StdEncoding.EncodeToString(data),
			})
		}
	}
}
