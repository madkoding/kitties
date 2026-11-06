package whatsapp

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Speaker turns a kitty's answer into a voice note with an OpenAI-compatible speech API
// (POST <base>/audio/speech), asking for Ogg Opus: the format WhatsApp plays as a voice note.
type Speaker struct {
	BaseURL string
	Key     string
	Model   string
	// Voice is the speaker's voice (alloy, nova, …); empty is DefaultVoice.
	Voice string
	HTTP  *http.Client
}

// The speech model and voice when the configuration names none.
const (
	SpeakModel   = "gpt-4o-mini-tts"
	DefaultVoice = "alloy"
)

// VoiceMIME is the type of a voice note: WhatsApp shows only Ogg Opus as one.
const VoiceMIME = "audio/ogg; codecs=opus"

// The voice modes of Settings.
const (
	VoiceOff    = ""       // the kitties write
	VoiceMirror = "mirror" // a voice note is answered with one
	VoiceAlways = "always" // every answer is a voice note
)

// maxSpoken is the longest answer read aloud (in runes): a longer one, with lists or code in
// it, is easier to read than to hear, so it goes as text.
const maxSpoken = 1200

// Speak implements Options.Speak: voice ("" is s.Voice) is the speaker's voice.
func (s Speaker) Speak(ctx context.Context, text, voice string) (File, error) {
	if s.Key == "" || s.BaseURL == "" {
		return File{}, errors.New("no speech service is set up")
	}
	model := s.Model
	if voice == "" {
		voice = s.Voice
	}
	if model == "" {
		model = SpeakModel
	}
	if voice == "" {
		voice = DefaultVoice
	}
	body, _ := json.Marshal(map[string]string{"model": model, "input": text, "voice": voice, "response_format": "opus"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(s.BaseURL, "/")+"/audio/speech", bytes.NewReader(body))
	if err != nil {
		return File{}, err
	}
	req.Header.Set("Authorization", "Bearer "+s.Key)
	req.Header.Set("Content-Type", "application/json")
	hc := s.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 2 * time.Minute}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return File{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxFile+1))
	if err != nil {
		return File{}, err
	}
	if resp.StatusCode/100 != 2 {
		if len(data) > 300 {
			data = data[:300]
		}
		return File{}, fmt.Errorf("the speech service answered %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	if len(data) > MaxFile {
		return File{}, errors.New("the voice note is too long")
	}
	if !bytes.HasPrefix(data, []byte("OggS")) {
		return File{}, errors.New("the speech service did not answer with Ogg Opus audio")
	}
	return File{Name: "voice.ogg", MIME: VoiceMIME, Data: data, Voice: true}, nil
}

// OggSeconds is how long an Ogg Opus audio plays, from the granule position of its last page
// (48 000 a second); 0 when it cannot tell.
func OggSeconds(data []byte) uint32 {
	i := bytes.LastIndex(data, []byte("OggS"))
	if i < 0 || len(data) < i+14 {
		return 0
	}
	g := binary.LittleEndian.Uint64(data[i+6 : i+14])
	if g == ^uint64(0) {
		return 0
	}
	sec := (g + 47999) / 48000
	if sec > 1<<20 {
		return 0
	}
	return uint32(sec)
}

// unmark takes WhatsApp's marks off a text read aloud.
var unmark = strings.NewReplacer("*", "", "_", "", "`", "", "~", "")

// spoken is the text read aloud for an answer, or "" when it should go as text.
func spoken(text string) string {
	if strings.Contains(text, "```") {
		return ""
	}
	p := strings.TrimSpace(unmark.Replace(Plain(text)))
	if p == "" || IsEmoji(p) || len([]rune(p)) > maxSpoken {
		return ""
	}
	return p
}
