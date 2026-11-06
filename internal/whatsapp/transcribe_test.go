package whatsapp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTranscriber(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/transcriptions" || r.Header.Get("Authorization") != "Bearer k" {
			http.Error(w, "nope", 401)
			return
		}
		f, h, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		data, _ := io.ReadAll(f)
		switch string(data) {
		case "garbage":
			_, _ = io.WriteString(w, "not json")
		default:
			_, _ = io.WriteString(w, `{"text":" `+r.FormValue("model")+" "+h.Filename+` "}`)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	tr := Transcriber{BaseURL: srv.URL + "/v1/", Key: "k", Model: "whisper-1"}
	if got, err := tr.Transcribe(ctx, File{Name: "a.ogg", Data: []byte("x")}); err != nil || got != "whisper-1 a.ogg" {
		t.Fatalf("got %q %v", got, err)
	}
	if got, _ := tr.Transcribe(ctx, File{Data: []byte("x")}); got != "whisper-1 voice.ogg" {
		t.Fatalf("unnamed = %q", got)
	}
	if _, err := tr.Transcribe(ctx, File{Data: []byte("garbage")}); err == nil {
		t.Fatal("not json accepted")
	}
	if _, err := (Transcriber{BaseURL: srv.URL, Key: "bad"}).Transcribe(ctx, File{Data: []byte("x")}); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("bad key = %v", err)
	}
	if _, err := (Transcriber{}).Transcribe(ctx, File{}); err == nil {
		t.Fatal("no service accepted")
	}
	if _, err := (Transcriber{BaseURL: "http://127.0.0.1:1", Key: "k"}).Transcribe(ctx, File{}); err == nil {
		t.Fatal("no server accepted")
	}
	if _, err := (Transcriber{BaseURL: "::", Key: "k"}).Transcribe(ctx, File{}); err == nil {
		t.Fatal("bad address accepted")
	}
	if TranscribeModel("https://api.groq.com/openai/v1") != "whisper-large-v3" || TranscribeModel("https://api.openai.com/v1") != "whisper-1" {
		t.Fatal("models")
	}
}
