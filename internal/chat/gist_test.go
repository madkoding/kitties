package chat

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// fake builds a made-up secret from its pieces, so the repository's secret scan does not take
// the fixtures for real keys.
func fake(parts ...string) string { return strings.Join(parts, "") }

func TestGistMasksSecretsAndPersonalDetails(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Sure! I'll draft the launch plan for Friday.", "Sure! I'll draft the launch plan for Friday."},
		{"## Plan\n\n- **split** the work\n- ship `v2`", "Plan split the work ship v2"},
		{"See [the report](https://example.com/r/secret-path?token=abc) now", "See the report now"},
		{"Posted to https://hooks.slack.com/services/T000/B000/XXXX ok", "Posted to hooks.slack.com ok"},
		{"Logged in at https://ana:hunter2@db.example.com/admin", "Logged in at db.example.com"},
		{"Mail ana.perez+work@example.co.uk about it", "Mail ••• about it"},
		{"The key is " + fake("sk-", "proj-AbCdEf0123456789xyz") + ", keep it", "The key is •••, keep it"},
		{"token: " + fake("ghp_", "abcdefghijklmnop1234") + " and more", "token: ••• and more"},
		{"api_key=hunter2 set", "api_key=••• set"},
		{"the password is hunter2!", "the password is •••"},
		{"la contraseña es gato123", "la contraseña es •••"},
		{"Authorization: Bearer abc.def", "Authorization: Bearer •••"},
		{"jwt " + fake("eyJhbGciOiJIUzI1NiJ9", ".eyJzdWIiOiIxIn0.sig") + " here", "jwt ••• here"},
		{"card 4111 1111 1111 1111 expires", "card ••• expires"},
		{"call +56 9 1234 5678 now", "call ••• now"},
		{"your code is 482913", "your code is •••"},
		{"id 550e8400-e29b-41d4-a716-446655440000 done", "id ••• done"},
		{"due 2026-11-07, 3 tasks, 120 files", "due 2026-11-07, 3 tasks, 120 files"},
		{"edited docs/screenshots/01-empty.png", "edited docs/screenshots/01-empty.png"},
		{"```\nsecret = 1\n```\nDone with the script", "Done with the script"},
		{"-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNz\n-----END OPENSSH PRIVATE KEY-----\nhere", "••• here"},
		{"   \n ", ""},
	}
	for _, c := range cases {
		if got := Gist(c.in); got != c.want {
			t.Errorf("Gist(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestGistIsOneShortLine(t *testing.T) {
	long := strings.Repeat("the kitties are planning the launch together ", 40)
	g := Gist(long)
	if n := utf8.RuneCountInString(g); n > GistLen || !strings.HasSuffix(g, "…") || strings.Contains(g, "\n") {
		t.Errorf("Gist = %q (%d runes)", g, n)
	}
	if g := Gist(strings.Repeat("ñ", 700)); !utf8.ValidString(g) || utf8.RuneCountInString(g) > GistLen {
		t.Errorf("multibyte gist = %q", g)
	}
	// A secret the read window halves is cut on a space before it, not leaked in part.
	if g := Gist(strings.Repeat("a ", 290) + "sk-" + strings.Repeat("Ab1", 20)); strings.Contains(g, "sk-") {
		t.Errorf("halved secret leaked: %q", g)
	}
}

func TestGistFollowsTheChat(t *testing.T) {
	f, _, _, _ := fleet(t)
	s := service(t, f)
	sess, err := s.Create("", []string{"ana"})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(sess.ID); got.Gist != "" {
		t.Errorf("a new chat has no gist: %q", got.Gist)
	}
	if _, err := s.Send(context.Background(), sess.ID, "plan the launch, my pin: 1234"); err != nil {
		t.Fatal(err)
	}
	s.Wait()
	list := s.List()
	if len(list) != 1 || list[0].Gist != "ana says hi" || list[0].GistBy != "ana" {
		t.Fatalf("List gist = %q by %q", list[0].Gist, list[0].GistBy)
	}

	// The gist is saved with the chat and made again when a chat is read back, so a chat saved
	// before gists (or with a stale one) gets its own.
	path := filepath.Join(s.dir, sess.ID+".json")
	var raw map[string]any
	data, _ := os.ReadFile(path)
	_ = json.Unmarshal(data, &raw)
	if raw["gist"] != "ana says hi" {
		t.Errorf("saved gist = %v", raw["gist"])
	}
	raw["gist"], raw["gist_by"] = "stale", "nobody"
	turns := raw["turns"].([]any)
	raw["turns"] = turns[:1] // only the person's message left
	data, _ = json.Marshal(raw)
	_ = os.WriteFile(path, data, 0o600)
	again, err := New(f, s.dir)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := again.Get(sess.ID)
	if got.Gist != "plan the launch, my pin: •••" || got.GistBy != "user" {
		t.Errorf("reloaded gist = %q by %q", got.Gist, got.GistBy)
	}
}

func TestGistSkipsConnectorResultsAndEmptyTurns(t *testing.T) {
	sess := &Session{Turns: []Turn{
		{From: "user", Text: "open the browser and find flights"},
		{From: "ana", Text: "", Working: true},
		{From: "system", Text: "flight results: 4 rows"},
	}}
	if g, by := gistOf(sess); g != "open the browser and find flights" || by != "user" {
		t.Errorf("gistOf = %q by %q", g, by)
	}
	if g, by := gistOf(&Session{Summary: "They planned the launch."}); g != "They planned the launch." || by != "" {
		t.Errorf("summary gist = %q by %q", g, by)
	}
}
