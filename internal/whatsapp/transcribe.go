package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

// Transcriber turns voice notes into text with an OpenAI-compatible speech API
// (POST <base>/audio/transcriptions): OpenAI's whisper-1, or Groq's whisper-large-v3.
type Transcriber struct {
	BaseURL string
	Key     string
	Model   string
	HTTP    *http.Client
}

// TranscribeModel is the speech model of a provider's address.
func TranscribeModel(baseURL string) string {
	if strings.Contains(baseURL, "groq.com") {
		return "whisper-large-v3"
	}
	return "whisper-1"
}

// Transcribe implements Options.Transcribe.
func (t Transcriber) Transcribe(ctx context.Context, f File) (string, error) {
	if t.Key == "" || t.BaseURL == "" {
		return "", errors.New("no speech service is set up")
	}
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("model", t.Model)
	name := f.Name
	if name == "" {
		name = "voice.ogg"
	}
	part, _ := w.CreateFormFile("file", name)
	_, _ = part.Write(f.Data)
	_ = w.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(t.BaseURL, "/")+"/audio/transcriptions", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+t.Key)
	req.Header.Set("Content-Type", w.FormDataContentType())
	hc := t.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 2 * time.Minute}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("the speech service answered %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("the speech service answered something else: %w", err)
	}
	return strings.TrimSpace(out.Text), nil
}
