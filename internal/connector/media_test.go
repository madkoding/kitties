package connector

import (
	"context"
	"strings"
	"testing"
)

var spot = Settings{"client_id": "cid"}

func TestSpotify(t *testing.T) {
	out, err, cap := run(t, "spotify", spot, map[string]any{},
		`{"is_playing":true,"item":{"name":"Gracias a la vida","uri":"spotify:track:1","artists":[{"name":"Violeta Parra"}],"album":{"name":"Las últimas composiciones"}}}`)
	if err != nil || out != "playing: Gracias a la vida by Violeta Parra (Las últimas composiciones) spotify:track:1" ||
		!strings.HasSuffix(cap.reqs[0].URL.Path, "/me/player/currently-playing") || cap.reqs[0].Header.Get("Authorization") != "Bearer tok" {
		t.Errorf("now playing = %q %v", out, err)
	}
	if out, _, _ := run(t, "spotify", spot, map[string]any{}, ""); out != "nothing is playing" {
		t.Errorf("silence = %q", out)
	}
	if out, _, _ := run(t, "spotify", spot, map[string]any{}, `{"is_playing":false,"item":{"name":"x","artists":[]}}`); !strings.HasPrefix(out, "paused: x") {
		t.Errorf("paused = %q", out)
	}
	// Play the best match: a search, then the play.
	out, err, cap = run(t, "spotify", spot, map[string]any{"action": "play", "query": "violeta parra"},
		`{"tracks":{"items":[{"uri":"spotify:track:1","name":"Gracias a la vida","artists":[{"name":"Violeta Parra"}]}]}}`, "")
	if err != nil || out != "playing Gracias a la vida by Violeta Parra" || cap.reqs[1].Method != "PUT" || cap.bodies[1] != `{"uris":["spotify:track:1"]}` ||
		cap.reqs[1].Header.Get("Authorization") != "Bearer tok" {
		t.Errorf("play = %q %v %s", out, err, cap.bodies[1])
	}
	if _, err, _ := run(t, "spotify", spot, map[string]any{"action": "play", "query": "zzz"}, `{"tracks":{"items":[]}}`); err == nil || !strings.Contains(err.Error(), "nothing on Spotify") {
		t.Errorf("no match = %v", err)
	}
	_, _, cap = run(t, "spotify", spot, map[string]any{"action": "play", "uri": "spotify:playlist:9"})
	if cap.bodies[0] != `{"context_uri":"spotify:playlist:9"}` {
		t.Errorf("a playlist plays as a context: %s", cap.bodies[0])
	}
	if _, _, cap := run(t, "spotify", spot, map[string]any{"action": "play"}); cap.bodies[0] != "" || cap.reqs[0].Method != "PUT" {
		t.Errorf("resume = %s", cap.bodies[0])
	}
	for action, want := range map[string]string{"pause": "PUT /v1/me/player/pause", "next": "POST /v1/me/player/next", "previous": "POST /v1/me/player/previous"} {
		_, _, cap := run(t, "spotify", spot, map[string]any{"action": action})
		if got := cap.reqs[0].Method + " " + cap.reqs[0].URL.Path; got != want {
			t.Errorf("%s = %s", action, got)
		}
	}
	_, _, cap = run(t, "spotify", spot, map[string]any{"action": "queue", "uri": "spotify:track:2"})
	if !strings.Contains(cap.reqs[0].URL.RawQuery, "uri=spotify%3Atrack%3A2") {
		t.Errorf("queue = %s", cap.reqs[0].URL)
	}
	out, _, _ = run(t, "spotify", spot, map[string]any{"action": "search", "query": "parra"}, `{"tracks":{"items":[{"uri":"spotify:track:1","name":"Run Run","artists":[{"name":"Violeta Parra"}],"album":{"name":"Album"}}]}}`)
	if out != "- spotify:track:1 | Run Run | Violeta Parra | Album" {
		t.Errorf("search = %q", out)
	}
	if out, _, _ := run(t, "spotify", spot, map[string]any{"action": "search", "query": "x", "type": "album"}, `{"albums":{"items":[{"uri":"spotify:album:1","name":"A"}]}}`); !strings.Contains(out, "spotify:album:1 | A") {
		t.Errorf("album search = %q", out)
	}
	if out, _, _ := run(t, "spotify", spot, map[string]any{"action": "playlists"}, `{"items":[{"id":"p1","name":"Road","tracks":{"total":12}}]}`); out != "- p1 | Road | 12" {
		t.Errorf("playlists = %q", out)
	}
	out, err, cap = run(t, "spotify", spot, map[string]any{"action": "create_playlist", "name": "Sunday"}, `{"id":"me1"}`, `{"id":"p2"}`)
	if err != nil || !strings.Contains(out, "id: p2") || cap.reqs[1].URL.Path != "/v1/users/me1/playlists" || !strings.Contains(cap.bodies[1], `"public":false`) {
		t.Errorf("create = %q %v %s", out, err, cap.reqs[1].URL)
	}
	if _, err, _ := run(t, "spotify", spot, map[string]any{"action": "create_playlist", "name": "x"}, `{}`); err == nil {
		t.Error("an unknown account fails")
	}
	_, _, cap = run(t, "spotify", spot, map[string]any{"action": "add_to_playlist", "playlist": "p2", "uris": "spotify:track:1, spotify:track:2"})
	if cap.bodies[0] != `{"uris":["spotify:track:1","spotify:track:2"]}` {
		t.Errorf("add = %s", cap.bodies[0])
	}
	for _, args := range []map[string]any{{"action": "search"}, {"action": "queue"}, {"action": "create_playlist"}, {"action": "add_to_playlist", "playlist": "p"}, {"action": "add_to_playlist", "uris": "u"}} {
		if _, err, _ := run(t, "spotify", spot, args); err == nil {
			t.Errorf("%v passes", args)
		}
	}
	if spotifyCheck(nil) != nil || !strings.Contains(spotifyCheck(errString("HTTP 404: NO_ACTIVE_DEVICE")).Error(), "open Spotify") ||
		!strings.Contains(spotifyCheck(errString("HTTP 403: PREMIUM_REQUIRED")).Error(), "Premium") || spotifyCheck(errString("x")).Error() != "x" {
		t.Error("spotify's refusals are explained")
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestTidal(t *testing.T) {
	td := Settings{"client_id": "cid", "country": "cl"}
	out, err, cap := run(t, "tidal", td, map[string]any{"query": "los jaivas"},
		`{"data":{"id":"los jaivas","type":"searchResults"},"included":[{"id":"123","type":"tracks","attributes":{"title":"Mira niñita","duration":"PT4M2S"}}]}`)
	if err != nil || out != "- tracks | 123 | Mira niñita | PT4M2S" {
		t.Errorf("search = %q %v", out, err)
	}
	u := cap.reqs[0].URL.String()
	if !strings.HasPrefix(u, "https://openapi.tidal.com/v2/searchResults?countryCode=CL&include=tracks&filter%5Bquery%5D=los+jaivas") || cap.reqs[0].Header.Get("Accept") != "application/vnd.api+json" {
		t.Errorf("url = %s %v", u, cap.reqs[0].Header)
	}
	if _, _, cap := run(t, "tidal", Settings{"client_id": "c"}, map[string]any{"query": "x", "type": "album"}, `{}`); !strings.Contains(cap.reqs[0].URL.String(), "countryCode=US&include=albums") {
		t.Errorf("album = %s", cap.reqs[0].URL)
	}
	out, _, cap = run(t, "tidal", td, map[string]any{"action": "playlists"}, `{"data":[{"id":"pl1","attributes":{"name":"Cueca","numberOfItems":8}}]}`)
	if out != "- pl1 | Cueca | 8" || !strings.Contains(cap.reqs[0].URL.String(), "filter%5Bowners.id%5D=me") {
		t.Errorf("playlists = %q %s", out, cap.reqs[0].URL)
	}
	_, _, cap = run(t, "tidal", td, map[string]any{"action": "create_playlist", "name": "Fonda", "public": "true"}, `{"data":{"id":"pl2"}}`)
	if !strings.Contains(cap.bodies[0], `"accessType":"PUBLIC"`) || !strings.Contains(cap.bodies[0], `"type":"playlists"`) || cap.reqs[0].Header.Get("Content-Type") != "application/vnd.api+json" {
		t.Errorf("create = %s %v", cap.bodies[0], cap.reqs[0].Header)
	}
	_, _, cap = run(t, "tidal", td, map[string]any{"action": "add_to_playlist", "playlist": "pl2", "tracks": "123,456"}, "")
	if cap.bodies[0] != `{"data":[{"id":"123","type":"tracks"},{"id":"456","type":"tracks"}]}` || !strings.HasSuffix(cap.reqs[0].URL.Path, "/playlists/pl2/relationships/items") {
		t.Errorf("add = %s %s", cap.bodies[0], cap.reqs[0].URL)
	}
	for _, args := range []map[string]any{{"action": "search"}, {"action": "create_playlist"}, {"action": "add_to_playlist", "playlist": "p"}, {"action": "add_to_playlist", "tracks": "1"}} {
		if _, err, _ := run(t, "tidal", td, args); err == nil {
			t.Errorf("%v passes", args)
		}
	}
	if k, _ := LookupKind("tidal"); k.OAuth != "tidal" || !strings.Contains(k.Help, "no remote playback") {
		t.Errorf("tidal = %+v", k)
	}
}

func TestDropbox(t *testing.T) {
	db := Settings{"token": "sl.tok"}
	out, err, cap := run(t, "dropbox", db, map[string]any{"path": "/Docs/"}, `{"entries":[{".tag":"file","path_display":"/Docs/a.txt","size":12,"server_modified":"2026-10-22T10:00:00Z"}]}`)
	if err != nil || out != "- /Docs/a.txt | 12 | 2026-10-22T10:00:00Z" || cap.bodies[0] != `{"limit":50,"path":"/Docs"}` || cap.reqs[0].Header.Get("Authorization") != "Bearer sl.tok" {
		t.Errorf("list = %q %v %s", out, err, cap.bodies[0])
	}
	if _, _, cap := run(t, "dropbox", db, map[string]any{"path": "/"}, `{}`); !strings.Contains(cap.bodies[0], `"path":""`) {
		t.Errorf("root = %s", cap.bodies[0])
	}
	out, _, _ = run(t, "dropbox", db, map[string]any{"action": "search", "query": "invoice"}, `{"matches":[{"metadata":{"metadata":{"path_display":"/Invoices/1.pdf","size":3}}}]}`)
	if !strings.Contains(out, "/Invoices/1.pdf | 3") {
		t.Errorf("search = %q", out)
	}
	c, _ := Build("dropbox", db, "")
	(&capture{answer: []string{"hello file"}}).install(t)
	text, name, data, err := c.(Filer).CallFile(context.Background(), map[string]any{"action": "read", "path": "/Docs/a.txt"})
	if err != nil || name != "a.txt" || string(data) != "hello file" || !strings.Contains(text, "saved as a.txt") {
		t.Errorf("read = %q %q %q %v", text, name, data, err)
	}
	out, _, cap = run(t, "dropbox", db, map[string]any{"action": "upload", "name": "notes.txt", "content": "hi"}, `{"path_display":"/Kitties/notes.txt"}`)
	if out != "saved as /Kitties/notes.txt" || cap.bodies[0] != "hi" || !strings.Contains(cap.reqs[0].Header.Get("Dropbox-API-Arg"), `"path":"/Kitties/notes.txt"`) || cap.reqs[0].URL.Host != "content.dropboxapi.com" {
		t.Errorf("upload = %q %v", out, cap.reqs[0].Header)
	}
	if _, _, cap := run(t, "dropbox", db, map[string]any{"action": "upload", "name": "/Work/plan.md", "content": "x"}, `{}`); !strings.Contains(cap.reqs[0].Header.Get("Dropbox-API-Arg"), `"path":"/Work/plan.md"`) {
		t.Errorf("into a folder = %v", cap.reqs[0].Header)
	}
	out, _, _ = run(t, "dropbox", db, map[string]any{"action": "share_link", "path": "/Docs/a.txt"}, `{"url":"https://www.dropbox.com/s/x"}`)
	if !strings.Contains(out, "https://www.dropbox.com/s/x") || RiskOf("dropbox", "share_link") != RiskAsk {
		t.Errorf("share = %q", out)
	}
	// Signed in instead of a token: the account's token.
	if _, _, cap := run(t, "dropbox", Settings{"client_id": "app"}, map[string]any{}, `{}`); cap.reqs[0].Header.Get("Authorization") != "Bearer tok" {
		t.Errorf("signed in = %v", cap.reqs[0].Header)
	}
	for _, args := range []map[string]any{{"action": "search"}, {"action": "read"}, {"action": "upload"}, {"action": "share_link"}} {
		if _, err, _ := run(t, "dropbox", db, args); err == nil {
			t.Errorf("%v passes", args)
		}
	}
}

func TestThingsAndOmniFocus(t *testing.T) {
	call := func(kind string, args map[string]any) (string, error) {
		c, _ := Build(kind, Settings{}, "")
		return c.Call(context.Background(), args)
	}
	got := fakeMac(t, `[{"id":"T1","name":"Call Ana","due":"2026-10-23T12:00:00Z","project":"Home"},{"id":"T2","name":"Buy milk"}]`)
	out, err := call("things", map[string]any{})
	if err != nil || !strings.Contains(out, "- T1 | Call Ana | due ") || !strings.Contains(out, "| Home") || !strings.Contains(out, "- T2 | Buy milk") || !strings.Contains((*got)[0], `"action":"todos"`) {
		t.Errorf("things = %q %v %v", out, err, *got)
	}
	got = fakeMac(t, `{"id":"T3","name":"Pay rent"}`)
	if out, err := call("things", map[string]any{"action": "add_todo", "title": "Pay rent", "due": "2026-10-30", "project": "Home"}); err != nil || out != `added "Pay rent" to Things (id T3)` || !strings.Contains((*got)[0], `"project":"Home"`) {
		t.Errorf("add = %q %v", out, err)
	}
	fakeMac(t, `{"name":"Pay rent"}`)
	if out, _ := call("things", map[string]any{"action": "complete", "id": "T3"}); out != "done: Pay rent" {
		t.Errorf("complete = %q", out)
	}
	got = fakeMac(t, `[{"id":"o1","name":"Review","flagged":true,"project":"Work"}]`)
	if out, _ := call("omnifocus", map[string]any{"days": "3"}); out != "- o1 | Review | flagged | Work" || !strings.Contains((*got)[0], `"days":3`) {
		t.Errorf("omnifocus = %q %v", out, *got)
	}
	got = fakeMac(t, `{"id":"o2","name":"Plan"}`)
	if out, _ := call("omnifocus", map[string]any{"action": "add_task", "title": "Plan", "flagged": "true"}); out != `added "Plan" to OmniFocus (id o2)` || !strings.Contains((*got)[0], `"flagged":true`) {
		t.Errorf("add task = %q", out)
	}
	fakeMac(t, `[]`)
	if out, _ := call("omnifocus", map[string]any{"action": "tasks"}); out != "nothing to do there" {
		t.Errorf("empty = %q", out)
	}
	fakeMac(t, "ERR:execution error: Can’t get application \"Things3\". (-2700)")
	if _, err := call("things", map[string]any{}); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("missing app = %v", err)
	}
	for _, args := range []map[string]any{{"action": "add_todo"}, {"action": "add_todo", "title": "x", "due": "nope"}, {"action": "complete"}, {"action": "dance"}} {
		fakeMac(t, `{}`)
		if _, err := call("things", args); err == nil {
			t.Errorf("%v passes", args)
		}
	}
	for _, a := range []string{"todos", "add_todo", "complete"} {
		fakeMac(t, "nope")
		if _, err := call("things", map[string]any{"action": a, "title": "x", "id": "1"}); err == nil {
			t.Errorf("%s with a broken answer passes", a)
		}
	}
	if c, _ := Build("omnifocus", Settings{}, ""); !strings.Contains(c.Describe(), "add_task") {
		t.Error("describe")
	}
	if !strings.Contains(thingsScript, "Things3") || !strings.Contains(omnifocusScript, "markComplete") {
		t.Error("scripts")
	}
}
