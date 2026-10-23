package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

// MUSIC, FILES AND THE MAC'S TASK MANAGERS.
//
// Spotify plays and finds music and keeps playlists; TIDAL finds music and keeps playlists (its
// public API has no remote playback); Dropbox holds files; Things and OmniFocus are where many
// people keep their tasks on a Mac.

// ── Spotify ────────────────────────────────────────────────────────────────────────────────

const spotifyAPI = "https://api.spotify.com/v1"

// spotifyCheck explains Spotify's usual refusals: no device playing, or no Premium.
func spotifyCheck(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "NO_ACTIVE_DEVICE"):
		return errors.New("no Spotify device is playing: open Spotify on a phone, a computer or a speaker first")
	case strings.Contains(msg, "PREMIUM_REQUIRED"):
		return errors.New("controlling playback needs Spotify Premium")
	}
	return err
}

func makeSpotify(s Settings) Connector {
	c := newAPI("spotify", s, map[string]actionFn{
		"now_playing": func(s Settings, a map[string]any) (request, error) {
			return request{method: "GET", url: spotifyAPI + "/me/player/currently-playing"}, nil
		},
		"search": func(s Settings, a map[string]any) (request, error) {
			q, err := need(a, "query", "q")
			if err != nil {
				return request{}, err
			}
			typ := str(a, "type")
			if typ == "" {
				typ = "track"
			}
			return request{method: "GET", url: spotifyAPI + "/search?type=" + url.QueryEscape(typ) + "&limit=" + strconv.Itoa(limitOf(a)) + "&q=" + url.QueryEscape(q)}, nil
		},
		"play": func(s Settings, a map[string]any) (request, error) {
			if uri := str(a, "uri"); uri != "" {
				return spotifyPlay(uri), nil
			}
			q := str(a, "query", "q")
			if q == "" {
				return request{method: "PUT", url: spotifyAPI + "/me/player/play"}, nil // resume
			}
			// The best match of the query, then play it.
			return request{method: "GET", url: spotifyAPI + "/search?type=track&limit=1&q=" + url.QueryEscape(q),
				follow: func(ctx context.Context, raw []byte, h map[string]string) (string, error) {
					var r struct {
						Tracks struct {
							Items []struct {
								URI     string                  `json:"uri"`
								Name    string                  `json:"name"`
								Artists []struct{ Name string } `json:"artists"`
							} `json:"items"`
						} `json:"tracks"`
					}
					if json.Unmarshal(raw, &r) != nil || len(r.Tracks.Items) == 0 {
						return "", fmt.Errorf("nothing on Spotify matches %q", q)
					}
					t := r.Tracks.Items[0]
					req := spotifyPlay(t.URI)
					req.headers = h
					if _, err := do(ctx, req); err != nil {
						return "", spotifyCheck(err)
					}
					by := ""
					if len(t.Artists) > 0 {
						by = " by " + t.Artists[0].Name
					}
					return "playing " + t.Name + by, nil
				}}, nil
		},
		"pause": func(Settings, map[string]any) (request, error) {
			return request{method: "PUT", url: spotifyAPI + "/me/player/pause"}, nil
		},
		"next": func(Settings, map[string]any) (request, error) {
			return request{method: "POST", url: spotifyAPI + "/me/player/next"}, nil
		},
		"previous": func(Settings, map[string]any) (request, error) {
			return request{method: "POST", url: spotifyAPI + "/me/player/previous"}, nil
		},
		"queue": func(s Settings, a map[string]any) (request, error) {
			uri, err := need(a, "uri")
			if err != nil {
				return request{}, err
			}
			return request{method: "POST", url: spotifyAPI + "/me/player/queue?uri=" + url.QueryEscape(uri)}, nil
		},
		"playlists": func(s Settings, a map[string]any) (request, error) {
			return request{method: "GET", url: spotifyAPI + "/me/playlists?limit=" + strconv.Itoa(limitOf(a))}, nil
		},
		"create_playlist": func(s Settings, a map[string]any) (request, error) {
			name, err := need(a, "name", "title")
			if err != nil {
				return request{}, err
			}
			body := map[string]any{"name": name, "public": str(a, "public") == "true", "description": str(a, "description")}
			return request{method: "GET", url: spotifyAPI + "/me", follow: func(ctx context.Context, raw []byte, h map[string]string) (string, error) {
				var me struct {
					ID string `json:"id"`
				}
				if json.Unmarshal(raw, &me) != nil || me.ID == "" {
					return "", errors.New("spotify did not say who the account is")
				}
				out, err := do(ctx, request{method: "POST", url: spotifyAPI + "/users/" + url.PathEscape(me.ID) + "/playlists", headers: h, body: body})
				if err != nil {
					return "", err
				}
				return summary(out), nil
			}}, nil
		},
		"add_to_playlist": func(s Settings, a map[string]any) (request, error) {
			id, err := need(a, "playlist", "id")
			if err != nil {
				return request{}, err
			}
			uris := strings.FieldsFunc(str(a, "uris", "uri"), func(r rune) bool { return r == ',' || r == ' ' })
			if len(uris) == 0 {
				return request{}, errors.New("uri is missing (spotify:track:…)")
			}
			return request{method: "POST", url: spotifyAPI + "/playlists/" + url.PathEscape(id) + "/tracks", body: map[string]any{"uris": uris}}, nil
		},
	}, "now_playing")
	c.render = func(action string, data []byte) string {
		switch action {
		case "now_playing":
			var r struct {
				Playing bool `json:"is_playing"`
				Item    struct {
					Name    string                  `json:"name"`
					URI     string                  `json:"uri"`
					Artists []struct{ Name string } `json:"artists"`
					Album   struct{ Name string }   `json:"album"`
				} `json:"item"`
			}
			if json.Unmarshal(data, &r) != nil || r.Item.Name == "" {
				return "nothing is playing"
			}
			state := "playing"
			if !r.Playing {
				state = "paused"
			}
			var artists []string
			for _, a := range r.Item.Artists {
				artists = append(artists, a.Name)
			}
			return fmt.Sprintf("%s: %s by %s (%s) %s", state, r.Item.Name, strings.Join(artists, ", "), r.Item.Album.Name, r.Item.URI)
		case "search":
			for _, root := range []string{"tracks", "albums", "artists", "playlists"} {
				if strings.Contains(string(data), `"`+root+`"`) {
					return listing(data, root+".items", 30, "uri", "name", "artists.0.name", "album.name")
				}
			}
		case "playlists":
			return listing(data, "items", 50, "id", "name", "tracks.total")
		}
		return summary(data)
	}
	return c
}

func spotifyPlay(uri string) request {
	body := map[string]any{"uris": []string{uri}}
	if !strings.Contains(uri, ":track:") && !strings.Contains(uri, ":episode:") {
		body = map[string]any{"context_uri": uri} // an album, a playlist or an artist
	}
	return request{method: "PUT", url: spotifyAPI + "/me/player/play", body: body}
}

// ── TIDAL ──────────────────────────────────────────────────────────────────────────────────

const tidalAPI = "https://openapi.tidal.com/v2"

// tidalHeaders are the JSON:API types TIDAL speaks.
func tidalHeaders() map[string]string {
	return map[string]string{"Accept": "application/vnd.api+json"}
}

func makeTidal(s Settings) Connector {
	country := func(s Settings) string {
		if c := strings.ToUpper(strings.TrimSpace(s["country"])); len(c) == 2 {
			return c
		}
		return "US"
	}
	c := newAPI("tidal", s, map[string]actionFn{
		"search": func(s Settings, a map[string]any) (request, error) {
			q, err := need(a, "query", "q")
			if err != nil {
				return request{}, err
			}
			typ := str(a, "type")
			switch typ {
			case "", "track":
				typ = "tracks"
			case "album", "artist", "playlist":
				typ += "s"
			}
			return request{method: "GET", headers: tidalHeaders(), url: tidalAPI + "/searchResults?countryCode=" + country(s) + "&include=" + url.QueryEscape(typ) + "&filter%5Bquery%5D=" + url.QueryEscape(q)}, nil
		},
		"playlists": func(s Settings, a map[string]any) (request, error) {
			return request{method: "GET", headers: tidalHeaders(), url: tidalAPI + "/playlists?countryCode=" + country(s) + "&filter%5Bowners.id%5D=me"}, nil
		},
		"create_playlist": func(s Settings, a map[string]any) (request, error) {
			name, err := need(a, "name", "title")
			if err != nil {
				return request{}, err
			}
			access := "UNLISTED"
			if str(a, "public") == "true" {
				access = "PUBLIC"
			}
			h := tidalHeaders()
			return request{method: "POST", headers: h, ctype: "application/vnd.api+json", url: tidalAPI + "/playlists?countryCode=" + country(s),
				body: map[string]any{"data": map[string]any{"type": "playlists", "attributes": map[string]any{"name": name, "description": str(a, "description"), "accessType": access}}}}, nil
		},
		"add_to_playlist": func(s Settings, a map[string]any) (request, error) {
			id, err := need(a, "playlist", "id")
			if err != nil {
				return request{}, err
			}
			tracks := strings.FieldsFunc(str(a, "tracks", "track"), func(r rune) bool { return r == ',' || r == ' ' })
			if len(tracks) == 0 {
				return request{}, errors.New("tracks is missing (TIDAL track ids, from search)")
			}
			items := make([]any, 0, len(tracks))
			for _, t := range tracks {
				items = append(items, map[string]any{"id": t, "type": "tracks"})
			}
			return request{method: "POST", headers: tidalHeaders(), ctype: "application/vnd.api+json", url: tidalAPI + "/playlists/" + url.PathEscape(id) + "/relationships/items",
				body: map[string]any{"data": items}}, nil
		},
	}, "search")
	c.render = func(action string, data []byte) string {
		switch action {
		case "search":
			// JSON:API: the matches are in included, by type.
			return listing(data, "included", 30, "type", "id", "attributes.title", "attributes.name", "attributes.duration")
		case "playlists":
			return listing(data, "data", 50, "id", "attributes.name", "attributes.numberOfItems")
		}
		return summary(data)
	}
	return c
}

// ── Dropbox ────────────────────────────────────────────────────────────────────────────────

const (
	dropboxAPI     = "https://api.dropboxapi.com/2"
	dropboxContent = "https://content.dropboxapi.com/2"
)

// dropboxPath is a path as Dropbox wants it: from the root, "" for the root itself.
func dropboxPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" || p == "/" {
		return ""
	}
	return "/" + strings.Trim(p, "/")
}

func makeDropbox(s Settings) Connector {
	auth := func(s Settings) map[string]string {
		if s["token"] != "" {
			return bearer(s["token"])
		}
		return map[string]string{}
	}
	arg := func(h map[string]string, v any) map[string]string {
		raw, _ := json.Marshal(v)
		h["Dropbox-API-Arg"] = string(raw)
		return h
	}
	c := newAPI("dropbox", s, map[string]actionFn{
		"list": func(s Settings, a map[string]any) (request, error) {
			return request{method: "POST", url: dropboxAPI + "/files/list_folder", headers: auth(s), body: map[string]any{"path": dropboxPath(str(a, "path", "folder")), "limit": limitOf(a) * 5}}, nil
		},
		"search": func(s Settings, a map[string]any) (request, error) {
			q, err := need(a, "query", "q")
			if err != nil {
				return request{}, err
			}
			return request{method: "POST", url: dropboxAPI + "/files/search_v2", headers: auth(s), body: map[string]any{"query": q, "options": map[string]any{"max_results": limitOf(a)}}}, nil
		},
		"read": func(s Settings, a map[string]any) (request, error) {
			p, err := need(a, "path", "name")
			if err != nil {
				return request{}, err
			}
			return request{method: "POST", url: dropboxContent + "/files/download", headers: arg(auth(s), map[string]any{"path": dropboxPath(p)}), file: path.Base(p)}, nil
		},
		"upload": func(s Settings, a map[string]any) (request, error) {
			name, err := need(a, "name", "path")
			if err != nil {
				return request{}, err
			}
			target := dropboxPath(name)
			if !strings.Contains(strings.Trim(name, "/"), "/") {
				target = "/Kitties/" + strings.Trim(name, "/")
			}
			return request{method: "POST", url: dropboxContent + "/files/upload", ctype: "application/octet-stream", raw: []byte(str(a, "content", "text")),
				headers: arg(auth(s), map[string]any{"path": target, "mode": "add", "autorename": true})}, nil
		},
		"share_link": func(s Settings, a map[string]any) (request, error) {
			p, err := need(a, "path")
			if err != nil {
				return request{}, err
			}
			return request{method: "POST", url: dropboxAPI + "/sharing/create_shared_link_with_settings", headers: auth(s), body: map[string]any{"path": dropboxPath(p)}}, nil
		},
	}, "list")
	c.render = func(action string, data []byte) string {
		switch action {
		case "list":
			// A folder has no size nor date: its line is its path.
			return listing(data, "entries", 50, "path_display", "size", "server_modified")
		case "search":
			return listing(data, "matches", 30, "metadata.metadata.path_display", "metadata.metadata.size", "metadata.metadata.server_modified")
		case "upload":
			var r struct {
				Path string `json:"path_display"`
			}
			if json.Unmarshal(data, &r) == nil && r.Path != "" {
				return "saved as " + r.Path
			}
		}
		return summary(data)
	}
	return c
}

// ── Things and OmniFocus (the Mac) ─────────────────────────────────────────────────────────

const thingsScript = `
function run(argv) {
  const a = JSON.parse(argv[0]);
  const t = Application("Things3");
  const iso = (d) => (d ? d.toISOString() : "");
  switch (a.action) {
    case "todos": {
      const list = t.lists.byName(a.list || "Today");
      return JSON.stringify(list.toDos().slice(0, a.limit).map((x) => {
        let project = "";
        try { project = x.project() ? x.project().name() : ""; } catch (e) {}
        return { id: x.id(), name: x.name(), due: iso(x.dueDate()), project: project };
      }));
    }
    case "add_todo": {
      const props = { name: a.title };
      if (a.notes) props.notes = a.notes;
      if (a.due) props.dueDate = new Date(a.due);
      const x = t.ToDo(props);
      if (a.project) t.projects.byName(a.project).toDos.push(x);
      else t.lists.byName(a.list || "Inbox").toDos.push(x);
      return JSON.stringify({ id: x.id(), name: x.name() });
    }
    case "complete": {
      const x = t.toDos.byId(a.id);
      x.status = "completed";
      return JSON.stringify({ name: x.name() });
    }
  }
  throw new Error("unknown action " + a.action);
}`

const omnifocusScript = `
function run(argv) {
  const a = JSON.parse(argv[0]);
  const of = Application("OmniFocus");
  const doc = of.defaultDocument;
  const iso = (d) => (d ? d.toISOString() : "");
  switch (a.action) {
    case "tasks": {
      const soon = new Date(Date.now() + a.days * 86400000), seen = {}, out = [];
      const add = (x) => {
        if (seen[x.id()]) return;
        seen[x.id()] = true;
        let project = "";
        try { project = x.containingProject() ? x.containingProject().name() : ""; } catch (e) {}
        out.push({ id: x.id(), name: x.name(), due: iso(x.dueDate()), flagged: x.flagged(), project: project });
      };
      doc.flattenedTasks.whose({ _and: [{ completed: false }, { flagged: true }] })().forEach(add);
      doc.flattenedTasks.whose({ _and: [{ completed: false }, { dueDate: { _lessThan: soon } }] })().forEach(add);
      return JSON.stringify(out.slice(0, a.limit));
    }
    case "add_task": {
      const props = { name: a.title };
      if (a.notes) props.note = a.notes;
      if (a.due) props.dueDate = new Date(a.due);
      if (a.flagged) props.flagged = true;
      let x;
      if (a.project) { x = of.Task(props); doc.flattenedProjects.byName(a.project).tasks.push(x); }
      else { x = of.InboxTask(props); doc.inboxTasks.push(x); }
      return JSON.stringify({ id: x.id(), name: x.name() });
    }
    case "complete": {
      const x = doc.flattenedTasks.byId(a.id);
      of.markComplete(x);
      return JSON.stringify({ name: x.name() });
    }
  }
  throw new Error("unknown action " + a.action);
}`

// macTasks is a task manager on the Mac: Things or OmniFocus, through its own script.
type macTasks struct {
	kind, app, script string
	s                 Settings
}

func makeThings(s Settings) Connector {
	return &macTasks{kind: "things", app: "Things", script: thingsScript, s: s}
}

func makeOmniFocus(s Settings) Connector {
	return &macTasks{kind: "omnifocus", app: "OmniFocus", script: omnifocusScript, s: s}
}

func (m *macTasks) Describe() string {
	k, _ := LookupKind(m.kind)
	return k.Help + " " + actionsHelp(k)
}

func (m *macTasks) Call(ctx context.Context, args map[string]any) (string, error) {
	k, _ := LookupKind(m.kind)
	action := strings.ToLower(str(args, "action"))
	if action == "" {
		action = k.Actions[0].Name
	}
	switch action {
	case "todos", "tasks":
		var items []struct {
			ID, Name, Due, Project string
			Flagged                bool
		}
		req := map[string]any{"action": action, "list": pick(args, m.s, "list"), "limit": intArg(args, "limit", 30, 100), "days": intArg(args, "days", 7, 60)}
		if err := askMac(ctx, m.script, m.app, req, &items); err != nil {
			return "", err
		}
		var out []string
		for _, it := range items {
			line := "- " + it.ID + " | " + it.Name
			if it.Due != "" {
				line += " | due " + localTime(it.Due, false)
			}
			if it.Flagged {
				line += " | flagged"
			}
			if it.Project != "" {
				line += " | " + it.Project
			}
			out = append(out, line)
		}
		return capLines(out, 100, "nothing to do there"), nil
	case "add_todo", "add_task":
		title, err := need(args, "title", "name", "text")
		if err != nil {
			return "", err
		}
		req := map[string]any{"action": action, "title": title, "notes": str(args, "notes"), "project": str(args, "project"),
			"list": pick(args, m.s, "list"), "flagged": str(args, "flagged") == "true"}
		if v := str(args, "due"); v != "" {
			t, _, err := parseWhen(v)
			if err != nil {
				return "", err
			}
			req["due"] = t.Format(time.RFC3339)
		}
		var r struct{ ID, Name string }
		if err := askMac(ctx, m.script, m.app, req, &r); err != nil {
			return "", err
		}
		return fmt.Sprintf("added %q to %s (id %s)", r.Name, m.app, r.ID), nil
	case "complete":
		id, err := need(args, "id")
		if err != nil {
			return "", err
		}
		var r struct{ Name string }
		if err := askMac(ctx, m.script, m.app, map[string]any{"action": "complete", "id": id}, &r); err != nil {
			return "", err
		}
		return "done: " + r.Name, nil
	}
	return "", fmt.Errorf("%s has no action %q; %s", m.kind, action, actionsHelp(k))
}

// mediaKinds are the music, file and Mac task kinds of the catalogue.
func mediaKinds() []Kind {
	return []Kind{
		{Name: "spotify", Category: CatMusic, Label: "Spotify", Help: "Spotify: what is playing, play, pause and skip (Premium), search music, keep playlists",
			Fields: fApp("developer.spotify.com (redirect: this gateway's /v1/oauth/callback)"),
			Actions: []Action{
				{Name: "now_playing", Help: "what is playing now"},
				{Name: "play", Help: "play the best match of a query, a spotify: uri, or resume", Args: []string{"query or uri (optional)"}},
				{Name: "pause", Help: "pause"}, {Name: "next", Help: "skip to the next track"}, {Name: "previous", Help: "back to the previous track"},
				{Name: "queue", Help: "add a track to the queue", Args: []string{"uri"}},
				{Name: "search", Help: "find music", Args: []string{"query", "type (track, album, artist, playlist)", fLimit}},
				{Name: "playlists", Help: "the account's playlists", Args: []string{fLimit}},
				{Name: "create_playlist", Help: "make a playlist (private unless public is true)", Args: []string{"name", "description (optional)", "public (optional)"}},
				{Name: "add_to_playlist", Help: "add tracks to a playlist", Args: []string{"playlist (id)", "uris (spotify:track:…, separated by commas)"}},
			}, make: makeSpotify, oauth: "spotify"},
		{Name: "tidal", Category: CatMusic, Label: "TIDAL", Help: "TIDAL: search the catalogue and keep playlists (TIDAL's public API has no remote playback)",
			Fields: append([]Field{f("country", "Country", "two letters for the catalogue: US, CL, ES…", false, false)}, fApp("developer.tidal.com (redirect: this gateway's /v1/oauth/callback)")...),
			Actions: []Action{
				{Name: "search", Help: "find music; the ids go into playlists", Args: []string{"query", "type (track, album, artist, playlist)"}},
				{Name: "playlists", Help: "the account's playlists"},
				{Name: "create_playlist", Help: "make a playlist (unlisted unless public is true)", Args: []string{"name", "description (optional)", "public (optional)"}},
				{Name: "add_to_playlist", Help: "add tracks to a playlist", Args: []string{"playlist (id)", "tracks (ids, separated by commas)"}},
			}, make: makeTidal, oauth: "tidal"},
		{Name: "dropbox", Category: CatOffice, Label: "Dropbox", Help: "files in Dropbox: list, search, read, save text files and share a link",
			Fields: signIn(nil, f("token", "Access token", "a token from your Dropbox app's settings", false, true), "dropbox.com/developers"),
			Actions: []Action{
				{Name: "list", Help: "what is in a folder", Args: []string{"path (optional; the root)"}},
				{Name: "search", Help: "find files by name or content", Args: []string{"query", fLimit}},
				{Name: "read", Help: "read a file (it is kept as an artifact)", Args: []string{"path"}},
				{Name: "upload", Help: "save a text file (in /Kitties unless a folder is named)", Args: []string{"name", fContent}},
				{Name: "share_link", Help: "a link anyone can open to a file", Args: []string{"path"}},
			}, make: makeDropbox, oauth: "dropbox"},
		{Name: "things", Category: CatProductivity, Label: "Things (Mac)", Help: "to-dos in Things 3 on the Mac (when the gateway runs on it): what is on today, new to-dos, ticking them off",
			Fields: []Field{f("list", "List", "Today, Inbox, Upcoming, Anytime or Someday; empty: Today to read, Inbox to add", false, false)},
			Actions: []Action{
				{Name: "todos", Help: "the to-dos of a list", Args: []string{"list (optional)", fLimit}},
				{Name: "add_todo", Help: "add a to-do", Args: []string{fTitle, "notes (optional)", "due (optional)", "project (optional)", "list (optional)"}},
				{Name: "complete", Help: "tick a to-do off", Args: []string{"id"}},
			}, make: makeThings},
		{Name: "omnifocus", Category: CatProductivity, Label: "OmniFocus (Mac)", Help: "tasks in OmniFocus on the Mac (when the gateway runs on it): flagged and due soon, new tasks, completing them",
			Actions: []Action{
				{Name: "tasks", Help: "flagged tasks and the ones due soon", Args: []string{"days (optional, 7)", fLimit}},
				{Name: "add_task", Help: "add a task (to the inbox, or a project)", Args: []string{fTitle, "notes (optional)", "due (optional)", "project (optional)", "flagged (optional)"}},
				{Name: "complete", Help: "complete a task", Args: []string{"id"}},
			}, make: makeOmniFocus},
	}
}
