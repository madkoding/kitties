import { useState } from "preact/hooks";
import { api } from "./api";
import PixelCat from "./PixelCat";

// Login is what a gateway served on a network shows before anything else: the token it printed
// when it started, traded for a session cookie so the page never keeps it.
export default function Login({ onDone }: { onDone: () => void }) {
  const [token, setToken] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const submit = async (e: Event) => {
    e.preventDefault();
    setBusy(true);
    setErr("");
    try {
      await api.login(token);
      setToken("");
      onDone();
    } catch (x) {
      setErr((x as Error).message);
      setBusy(false);
    }
  };
  return (
    <div class="app-bg fixed inset-0 z-50 flex items-center justify-center p-4" data-testid="login">
      <form class="chat-modal w-full max-w-sm p-6" onSubmit={submit}>
        <div class="flex items-center gap-3 mb-4">
          <PixelCat breed="tabby" color="orange" mood="idle" size={40} />
          <div>
            <h1 class="text-lg font-semibold tracking-wide">Kitties</h1>
            <p class="text-xs text-[var(--t4)]">sign in to this gateway</p>
          </div>
        </div>
        <label class="block text-xs text-[var(--t3)] mb-1" for="login-token">
          Token
        </label>
        <input
          id="login-token"
          class="w-full px-3 py-2 text-sm font-mono bg-transparent border border-white/10 rounded"
          type="password"
          autocomplete="current-password"
          value={token}
          onInput={(e) => setToken((e.target as HTMLInputElement).value)}
          placeholder="the gateway's token"
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
          {busy ? "Checking…" : "Sign in"}
        </button>
        <p class="text-xs text-[var(--t4)] mt-3">
          The token stays on the server: this browser keeps only a session that you can end with
          Sign out.
        </p>
      </form>
    </div>
  );
}
