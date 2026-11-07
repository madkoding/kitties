package llmauth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// fakeGoogle answers Google's token endpoint: "good" is a code, "nolasting" one without a
// refresh token, anything else refused.
func fakeGoogle(t *testing.T) (*httptest.Server, *url.Values) {
	t.Helper()
	var mu sync.Mutex
	last := &url.Values{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		q, _ := url.ParseQuery(string(data))
		mu.Lock()
		*last = q
		mu.Unlock()
		switch {
		case q.Get("grant_type") == "refresh_token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "ya29-new", "expires_in": 3600})
		case q.Get("code") == "good":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "ya29", "refresh_token": "rt", "expires_in": 3599,
				"id_token": jwt(map[string]any{"email": "cat@gmail.com"})})
		case q.Get("code") == "noemail":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "ya29", "refresh_token": "rt"})
		case q.Get("code") == "nolasting":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "ya29"})
		default:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"invalid_grant","error_description":"Bad Request"}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, last
}

func TestGeminiSignsInInTheBrowser(t *testing.T) {
	r := newRig(t)
	g, last := fakeGoogle(t)
	r.m.ep.GoogleToken, r.m.ep.GoogleAuth = g.URL, "https://accounts.example/auth"
	if a := r.m.Accounts(); a[3].Kind != "gemini" || a[3].Browser {
		t.Fatalf("no app = %+v", a[3])
	}
	r.m.googleApp = func() (string, string) { return "cid.apps.googleusercontent.com", "sec" }
	if a := r.m.Accounts(); !a[3].Browser {
		t.Fatalf("app = %+v", a[3])
	}
	if _, err := r.m.Start("gemini", StartOptions{}); !errors.Is(err, ErrNoProject) {
		t.Fatalf("no project = %v", err)
	}
	f, err := r.m.Start("gemini", StartOptions{Project: " my-proj "})
	if err != nil || f.Method != "browser" || !f.Paste || f.Status != Pending {
		t.Fatalf("start = %+v, %v", f, err)
	}
	addr := <-r.addr
	u, _ := url.Parse(f.URL)
	q := u.Query()
	if !strings.HasPrefix(f.URL, "https://accounts.example/auth?") || q.Get("client_id") != "cid.apps.googleusercontent.com" ||
		q.Get("redirect_uri") != "http://"+addr+"/auth/callback" || q.Get("access_type") != "offline" ||
		!strings.Contains(q.Get("scope"), "generative-language") || q.Get("code_challenge_method") != "S256" {
		t.Fatalf("link = %s", f.URL)
	}
	// From a browser on another machine: the address it landed on, pasted back.
	if err := r.m.Paste(f.ID, q.Get("redirect_uri")+"?code=good&state="+q.Get("state")); err != nil {
		t.Fatal(err)
	}
	done := r.wait(t, f.ID)
	if done.Status != Connected || done.Who != "cat@gmail.com, project my-proj" {
		t.Fatalf("done = %+v", done)
	}
	if last.Get("client_secret") != "sec" || last.Get("code_verifier") == "" || last.Get("redirect_uri") != "http://"+addr+"/auth/callback" {
		t.Errorf("exchange = %v", *last)
	}
	acc, err := r.m.Source("gemini")(context.Background())
	if err != nil || acc.Headers["Authorization"] != "Bearer ya29" || acc.Headers["x-goog-user-project"] != "my-proj" {
		t.Fatalf("account = %+v, %v", acc, err)
	}
	// Via gcloud still takes gcloud's login.
	if _, err := r.m.Start("gemini", StartOptions{Project: "p", Via: "gcloud"}); err == nil || !strings.Contains(err.Error(), "gcloud") {
		t.Errorf("via gcloud = %v", err)
	}
}

func TestGeminiBrowserCallbackAndRefusals(t *testing.T) {
	r := newRig(t)
	g, _ := fakeGoogle(t)
	r.m.ep.GoogleToken = g.URL
	r.m.googleApp = func() (string, string) { return "cid", "" }
	f, err := r.m.Start("gemini", StartOptions{Project: "p"})
	if err != nil {
		t.Fatal(err)
	}
	addr := <-r.addr
	resp, err := http.Get("http://" + addr + "/auth/callback?code=noemail&state=" + stateOf(t, f.URL))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "Google account") {
		t.Errorf("page = %s", body)
	}
	if done := r.wait(t, f.ID); done.Status != Connected || done.Who != "Google, project p" {
		t.Fatalf("done = %+v", done)
	}
	for code, want := range map[string]string{"bad": "Google refused: invalid_grant Bad Request", "nolasting": "no lasting token"} {
		f, err := r.m.Start("gemini", StartOptions{Project: "p"})
		if err != nil {
			t.Fatal(err)
		}
		<-r.addr
		_ = r.m.Paste(f.ID, code+"#"+stateOf(t, f.URL))
		if done := r.wait(t, f.ID); done.Status != Failed || !strings.Contains(done.Error, want) {
			t.Errorf("%s = %+v", code, done)
		}
	}
	// A port that cannot be opened says so.
	r.m.listen = func(string) (net.Listener, error) { return nil, errors.New("busy") }
	if _, err := r.m.Start("gemini", StartOptions{Project: "p"}); err == nil || !strings.Contains(err.Error(), "busy") {
		t.Errorf("listen = %v", err)
	}
}
