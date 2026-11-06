package oauth

import (
	"errors"
	"net/url"
	"strings"
)

// Trello's authorize page is OAuth's implicit flow: it hands the token back in the return
// address's fragment (#token=…), which only the browser sees. The callback serves a page that
// reads the fragment and posts it to Deliver with the flow's state, so the token still lands
// on the gateway and never in the page's address bar history beyond that one load.
// https://developer.atlassian.com/cloud/trello/guides/rest-api/authorization/

// startImplicit returns the authorize page of an implicit flow, with the state in the return
// address (the page keeps the query, and adds the token after #).
func (m *Manager) startImplicit(connector string, p Provider, app App, redirect, authURL, key string) string {
	state := m.rand(24)
	m.remember(state, flow{connector: connector, provider: p, redirect: redirect, started: m.now(), clientID: key})
	back, _ := url.Parse(redirect)
	q := back.Query()
	q.Set("state", state)
	back.RawQuery = q.Encode()
	v := url.Values{"key": {key}, "return_url": {back.String()}}
	if len(p.Scopes) > 0 {
		v.Set("scope", strings.Join(p.Scopes, ","))
	}
	for k, x := range p.Extra {
		v.Set(k, x)
	}
	return authURL + "?" + v.Encode()
}

// Implicit reports whether state is a flow whose token comes back in the fragment: the
// callback then serves FragmentHTML instead of exchanging a code.
func (m *Manager) Implicit(state string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.pending[state]
	return ok && f.provider.Implicit
}

// Deliver finishes an implicit flow with the token the browser read from the fragment.
func (m *Manager) Deliver(state, token string) (string, error) {
	m.mu.Lock()
	f, ok := m.pending[state]
	if ok && f.provider.Implicit {
		delete(m.pending, state)
	}
	m.mu.Unlock()
	if !ok || !f.provider.Implicit {
		return "", errors.New("unknown or expired sign-in; start again from Settings")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", errors.New(f.provider.Label + " returned no token: allow Kitties on its page")
	}
	app, err := m.store.OAuthApp(f.connector)
	if err != nil {
		return "", err
	}
	// The calls need the key next to the token: Kitties' own, when the connection named none.
	learned := map[string]string{}
	if app.Settings[f.provider.AppKey] == "" {
		learned[f.provider.AppKey] = f.clientID
	}
	if strings.TrimSpace(app.Settings["token"]) != "" {
		learned["token"] = ""
	}
	if err := m.store.SaveOAuthSettings(f.connector, learned); err != nil {
		return "", err
	}
	return f.connector, m.store.SaveOAuthCredentials(f.connector, Credentials{AccessToken: token, Expiry: m.now().Add(forever)})
}

// FragmentHTML is the callback page of an implicit flow: it posts the fragment's token to the
// gateway and says how it went. %s is the state, as a JavaScript string.
const FragmentHTML = `<!doctype html><html><head><meta charset="utf-8"><title>Kitties</title>
<style>body{margin:0;height:100vh;display:grid;place-items:center;background:#0a0a0f;color:#e8e8ea;font:16px system-ui,sans-serif}
.card{padding:28px 32px;border:1px solid rgba(0,240,255,.4);box-shadow:0 0 14px rgba(0,240,255,.25);text-align:center}h1{font-size:18px;margin:0 0 8px}p{color:#9a9aaa;margin:0}</style></head>
<body><div class="card"><h1 id="t">Connecting…</h1><p id="p"></p></div><script>
var token=new URLSearchParams(location.hash.slice(1)).get("token")||"";
history.replaceState(null,"",location.pathname);
fetch("fragment",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({state:%s,token:token})})
.then(function(r){return r.json().then(function(b){return [r.ok,b]})})
.then(function(x){document.getElementById("t").textContent=x[0]?"Connected":"Sign-in failed";
document.getElementById("p").textContent=x[0]?"The connector "+x[1].name+" is signed in. You can close this window.":(x[1].error||"")})
.catch(function(e){document.getElementById("t").textContent="Sign-in failed";document.getElementById("p").textContent=String(e)});
</script></body></html>`
