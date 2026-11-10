#!/usr/bin/env python3
"""Keeps the logins of the open browser alive: every few minutes it visits the tabs again, so a
site that ends an idle session (Gmail, Google, banks) sees the kitty as still there.

  kitty-refresh            runs for good (the desktop's entrypoint starts it)
  kitty-refresh --once     one pass and out (prints what it did to each tab)

How often, first that applies (minutes; 0 turns it off):
  - ~/.config/kitties/browser-refresh, read on every pass: a number on its first line
  - /dev/shm/kitty-browser-refresh, which the gateway writes when Settings › Machines changes
  - KITTY_BROWSER_REFRESH (the gateway sets it from Settings › Machines), default 20

It never loses work nor gets in the way:
  - A tab is reloaded only when nobody is on it: it is not the one on screen while someone
    (the person in the live view, or the kitty's own clicks and keys) used the desktop in the last
    KITTY_BROWSER_IDLE minutes (default 5); it has no form with something typed in it, no focused
    field, nothing playing; and the kitty has not driven the browser (kitty-page) in the last
    2 minutes.
  - Any other tab only gets a quiet visit: a request to its own address from inside the page, with
    its cookies, which the site answers as a visit (and renews the session with) while nothing on
    the screen changes.
  - A page that asks before it is left (unsaved work) keeps its place: the question is answered
    "stay".
What a page says is never read nor kept; only its address goes to the log (/tmp/kitty-refresh.log).
"""
import json
import os
import subprocess
import sys
import time
import urllib.request

import websocket  # python3-websocket

PORT = 9222
BUSY_MARK = "/tmp/kitty-page.busy"  # kitty-page touches it on every command
BUSY_SECONDS = 120
CONF = os.path.expanduser("~/.config/kitties/browser-refresh")
LIVE = "/dev/shm/kitty-browser-refresh"  # the gateway writes it when Settings › Machines changes
LOG = "/tmp/kitty-refresh.log"

# What the page is doing right now, without its text: whether it is on screen and whether a person
# has something in it that a reload would lose.
STATE_JS = r"""(() => {
  const a = document.activeElement;
  const editing = !!a && (a.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(a.tagName));
  let dirty = false;
  for (const el of document.querySelectorAll('input,textarea')) {
    const t = (el.type || '').toLowerCase();
    if (['hidden','submit','button','image','reset','file'].includes(t)) continue;
    if (t === 'checkbox' || t === 'radio') { if (el.checked !== el.defaultChecked) { dirty = true; break; } continue; }
    if (el.value !== el.defaultValue) { dirty = true; break; }
  }
  let playing = false;
  for (const m of document.querySelectorAll('video,audio')) { if (!m.paused && !m.ended) { playing = true; break; } }
  return {visible: document.visibilityState === 'visible', editing, dirty, playing};
})()"""

# The quiet visit: the page's own address, with its cookies, from inside it. The answer is thrown
# away; what counts is that the site saw the visit (and set its cookies again).
PING_JS = r"""fetch(location.href, {credentials: 'include', cache: 'no-store', redirect: 'follow'})
  .then(r => r.status).catch(() => 0)"""


def note(msg):
    try:
        with open(LOG, "a") as f:
            f.write("%s %s\n" % (time.strftime("%F %T"), msg))
    except OSError:
        pass


def minutes(name, default):
    try:
        return max(0.0, float(os.environ.get(name, default)))
    except ValueError:
        return float(default)


def interval():
    """Minutes between passes; 0 is off. The kitty's own file, then what the gateway set while the
    desktop is on (Settings › Machines), then the environment it was made with."""
    for path in (CONF, LIVE):
        try:
            with open(path) as f:
                return max(0.0, float(f.readline().strip()))
        except (OSError, ValueError):
            continue
    return minutes("KITTY_BROWSER_REFRESH", 20)


def idle_ms():
    """Milliseconds since the last key or pointer move on the display; None when it cannot tell."""
    try:
        out = subprocess.run(["xprintidle"], capture_output=True, text=True, timeout=3)
        return int(out.stdout.strip()) if out.returncode == 0 else None
    except (OSError, ValueError, subprocess.SubprocessError):
        return None


def kitty_busy():
    try:
        return time.time() - os.path.getmtime(BUSY_MARK) < BUSY_SECONDS
    except OSError:
        return False


def tabs():
    try:
        with urllib.request.urlopen("http://127.0.0.1:%d/json" % PORT, timeout=3) as r:
            found = json.load(r)
    except (OSError, ValueError):
        return None
    return [t for t in found if t.get("type") == "page" and t.get("webSocketDebuggerUrl")
            and t.get("url", "").startswith(("http://", "https://"))]


class Tab:
    def __init__(self, tab):
        self.ws = websocket.create_connection(tab["webSocketDebuggerUrl"], timeout=15, suppress_origin=True)
        self.id = 0
        self.events = []

    def call(self, method, **params):
        self.id += 1
        self.ws.send(json.dumps({"id": self.id, "method": method, "params": params}))
        while True:
            msg = json.loads(self.ws.recv())
            if msg.get("id") == self.id:
                return msg.get("result") or {}
            self.on_event(msg)

    def on_event(self, msg):
        # A page that asks before it is left: it has unsaved work, so it stays.
        if msg.get("method") == "Page.javascriptDialogOpening":
            if msg.get("params", {}).get("type") == "beforeunload":
                self.events.append("kept")
                self.ws.send(json.dumps({"id": 0, "method": "Page.handleJavaScriptDialog", "params": {"accept": False}}))

    def eval(self, expression):
        res = self.call("Runtime.evaluate", expression=expression, returnByValue=True, awaitPromise=True)
        return (res.get("result") or {}).get("value")

    def reload(self, wait=8.0):
        self.call("Page.enable")
        self.call("Page.reload", ignoreCache=False)
        end = time.time() + wait
        self.ws.settimeout(1)
        while time.time() < end:
            try:
                msg = json.loads(self.ws.recv())
            except websocket.WebSocketTimeoutException:
                continue
            self.on_event(msg)
            if msg.get("method") == "Page.loadEventFired" or "kept" in self.events:
                break
        return "kept (the page asked before leaving)" if "kept" in self.events else "reloaded"

    def close(self):
        try:
            self.ws.close()
        except (websocket.WebSocketException, OSError):
            pass


def one_pass():
    found = tabs()
    if not found:
        return []
    idle = idle_ms()
    quiet_for = minutes("KITTY_BROWSER_IDLE", 5) * 60000
    someone_here = idle is None or idle < quiet_for
    busy = kitty_busy()
    done = []
    for t in found:
        tab = None
        try:
            tab = Tab(t)
            st = tab.eval(STATE_JS) or {}
            reason = ""
            if busy:
                reason = "the kitty is using the browser"
            elif st.get("dirty") or st.get("editing"):
                reason = "something is being typed in it"
            elif st.get("playing"):
                reason = "it is playing"
            elif st.get("visible") and someone_here:
                reason = "it is on screen and the desktop is in use"
            if reason:
                status = tab.eval(PING_JS)
                what = "visited quietly (%s; answer %s)" % (reason, status)
            else:
                what = tab.reload()
        except (websocket.WebSocketException, OSError, ValueError) as e:
            what = "skipped (%s)" % e.__class__.__name__
        finally:
            if tab:
                tab.close()
        url = t.get("url", "").split("?")[0].split("#")[0][:120]
        done.append((url, what))
        note("%s: %s" % (url, what))
    return done


def main():
    if "--once" in sys.argv[1:]:
        done = one_pass()
        if not done:
            print("no browser tab to refresh")
        for url, what in done:
            print("%s: %s" % (url, what))
        return
    last = time.time()
    while True:
        time.sleep(30)
        every = interval()
        if every <= 0 or time.time() - last < every * 60:
            continue
        last = time.time()
        try:
            one_pass()
        except Exception as e:  # noqa: BLE001 - it keeps running whatever a pass met
            note("pass failed: %r" % e)


if __name__ == "__main__":
    main()
