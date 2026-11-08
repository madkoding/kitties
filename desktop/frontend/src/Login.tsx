import { useEffect, useState } from "preact/hooks";
import { api } from "./api";
import { t } from "./i18n";
import PixelCat from "./PixelCat";
import { Missing, Required } from "./Required";

// A link from the desktop app's QR code carries the token after the #, which never reaches the
// server: it is taken off the address at once (so it stays out of the history and the bar) and
// used to sign in.
let fromLink = "";
{
  const m = location.hash.match(/^#signin=(.+)$/);
  if (m) {
    try {
      fromLink = decodeURIComponent(m[1]);
    } catch {
      fromLink = "";
    }
    history.replaceState(null, "", location.pathname + location.search);
  }
}

// Login is what a gateway served on a network shows before anything else: the token it printed
// when it started, traded for a session cookie so the page never keeps it.
export default function Login({ onDone }: { onDone: () => void }) {
  const [token, setToken] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const signIn = async (tok: string) => {
    setBusy(true);
    setErr("");
    try {
      await api.login(tok);
      setToken("");
      onDone();
    } catch (x) {
      setErr((x as Error).message);
      setBusy(false);
    }
  };
  const submit = async (e: Event) => {
    e.preventDefault();
    // Enter sends the form like the button does: only with a token, and once.
    if (busy || !token.trim()) {
      return;
    }
    await signIn(token.trim());
  };
  useEffect(() => {
    if (fromLink) {
      const tok = fromLink;
      fromLink = "";
      void signIn(tok);
    }
  }, []);
  return (
    <div class="app-bg fixed inset-0 z-50 flex items-center justify-center p-4" data-testid="login">
      <form class="chat-modal w-full max-w-sm p-6" onSubmit={submit}>
        <div class="flex items-center gap-3 mb-4">
          <PixelCat breed="tabby" color="orange" mood="idle" size={40} />
          <div>
            <h1 class="text-lg font-semibold tracking-wide">Kitties</h1>
            <p class="text-xs text-[var(--t4)]">{t("sign in to this gateway")}</p>
          </div>
        </div>
        <label class="block text-xs text-[var(--t3)] mb-1" for="login-token">
          {t("Token")}
          <Required />
        </label>
        <input
          id="login-token"
          aria-required="true"
          class="w-full px-3 py-2 text-sm font-mono bg-transparent border border-white/10 rounded"
          type="password"
          autocomplete="current-password"
          value={token}
          onInput={(e) => setToken((e.target as HTMLInputElement).value)}
          placeholder={t("the gateway's token")}
          autofocus
        />
        {err && (
          <p class="text-sm text-[var(--c-red)] mt-2" role="alert">
            {err}
          </p>
        )}
        <button
          class="btn-accent w-full mt-4 px-4 py-2 text-sm"
          type="submit"
          disabled={busy || !token.trim()}
        >
          {busy ? t("Checking…") : t("Sign in")}
        </button>
        <Missing items={[!token.trim() && t("the token")]} />
        <p class="text-xs text-[var(--t3)] mt-3" data-testid="login-where">
          {t("Where is it? In the console where the gateway started, or run")}{" "}
          <code class="font-mono">kitties token</code>{" "}
          {t("on that machine. In the desktop app: Settings, Preferences, Network access.")}
        </p>
        <p class="text-xs text-[var(--t4)] mt-3">
          {t("The token stays on the server: this browser keeps only a session cookie.")}
        </p>
      </form>
    </div>
  );
}
