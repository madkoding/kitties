package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

// WatchDesktop follows a kitty's display live (the gateway's motion JPEG) and hands each picture
// to the page as the runtime event "kitties:desktop:<ref>", a data: URL: the webview cannot load
// the gateway's address itself. Watching again restarts the stream.
func (a *App) WatchDesktop(ref string) error {
	if strings.ContainsAny(ref, "/?#") || ref == "" {
		return errors.New("not a kitty")
	}
	a.UnwatchDesktop(ref)
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	a.watches["desktop:"+ref] = cancel
	a.mu.Unlock()
	go func() {
		for ctx.Err() == nil {
			if err := a.streamDesktop(ctx, ref); err != nil && ctx.Err() == nil {
				if a.emit != nil {
					a.emit("kitties:desktop:"+ref, map[string]string{"error": err.Error()})
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

// UnwatchDesktop stops following a kitty's display.
func (a *App) UnwatchDesktop(ref string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if cancel, ok := a.watches["desktop:"+ref]; ok {
		cancel()
		delete(a.watches, "desktop:"+ref)
	}
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
