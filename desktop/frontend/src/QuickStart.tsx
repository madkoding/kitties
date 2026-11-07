import { useEffect, useRef, useState } from "preact/hooks";
import type { Account, ProviderKind } from "./api";
import { api, errText, openExternal } from "./api";
import { friendlyError, PROVIDERS, SIGN_IN } from "./friendly";
import { t } from "./i18n";
import { Missing, Required } from "./Required";
import { AccountSignIn } from "./SignIn";

// THE FIRST START, IN ONE SCREEN.
//
// With no model and no kitty yet, Welcome offers this: pick where the brain comes from, paste its
// key (Ollama Cloud has no sign-in for other apps, only its keys) or sign in with a plan the person already pays for (Claude, ChatGPT, GitHub Copilot, a
// Google login), name the kitty, and one button saves the model, checks it answers, creates the
// kitty and opens a chat with it. Everything else (more models, a look, connections) waits in
// Settings. The sign-ins work from a browser on another machine too: none of them needs the
// browser to reach the gateway back.

const CHOICES = ["anthropic", "openai", "gemini", "copilot", "ollama-cloud", "ollama"] as const;
type Choice = (typeof CHOICES)[number];
type Way = "key" | "login";

// signsOnly are the choices with no key to paste: Copilot only signs in.
const signsOnly = (c: Choice) => c === "copilot";

// The address of Ollama on this computer, its usual one.
const OLLAMA = "http://127.0.0.1:11434";

export default function QuickStart({ onDone }: { onDone: (chat: string) => void }) {
  const [choice, setChoice] = useState<Choice>("anthropic");
  // way is how the brain is paid: the person's own plan, signed in to, or a key. A plan comes
  // first: it is what most people already have. With no claude program where the gateway runs,
  // the key is offered first instead (until the person picks).
  const [way, setWay] = useState<Way>("login");
  const touched = useRef(false);
  const [key, setKey] = useState("");
  const [name, setName] = useState("Mimi");
  const [kinds, setKinds] = useState<ProviderKind[]>([]);
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [phase, setPhase] = useState<"edit" | "model" | "kitty">("edit");
  const [err, setErr] = useState("");
  const loadAccounts = () =>
    api
      .settings()
      .then((s) => {
        setKinds(s.kinds || []);
        setAccounts(s.accounts || []);
        if (!touched.current && s.accounts?.find((a) => a.kind === "claude-code")?.missing) {
          setWay("key");
        }
      })
      .catch(() => setKinds([]));
  useEffect(() => {
    loadAccounts();
  }, []);
  const signIn = SIGN_IN[choice];
  const login = way === "login" && Boolean(signIn);
  // kind is what the model is saved as: a Claude plan is claude-code, a ChatGPT plan codex.
  const kind = login ? signIn.kind : choice;
  const account = login ? accounts.find((a) => a.kind === signIn.kind) : undefined;
  const needsKey = !login && choice !== "ollama";
  const meta = PROVIDERS[choice];
  const missing = [
    needsKey && !key.trim() && t("the key"),
    login && !account?.signed_in && t("sign in"),
    !name.trim() && t("the kitty's name"),
  ].filter(Boolean) as string[];
  const start = async () => {
    if (missing.length || phase !== "edit") {
      return;
    }
    setErr("");
    setPhase("model");
    try {
      const body: Record<string, unknown> = {
        kind,
        model: kinds.find((k) => k.Name === kind)?.DefaultModel || signIn?.models?.[0] || "",
        base_url: kind === "ollama" ? OLLAMA : "",
      };
      if (needsKey) {
        body.api_key = key.trim();
      }
      // claude-code needs no mark: the claude program is always its own login.
      if (login && kind !== "claude-code") {
        body.auth = "login";
      }
      await api.setProvider(kind, body);
      const r = await api.testProvider(kind).catch((e) => ({ ok: false, error: errText(e) }));
      if (!r.ok) {
        // The model stays saved (Settings can fix it); the kitty waits for one that answers.
        setErr(friendlyError((r as { error?: string }).error || t("it did not answer")));
        setPhase("edit");
        return;
      }
      setPhase("kitty");
      const k = await api.createKitty({ name: name.trim(), provider: kind });
      const chat = await api.createChat("", [k.kitty.id]);
      setKey("");
      onDone(chat.id);
    } catch (e) {
      setErr(friendlyError(errText(e)));
      setPhase("edit");
    }
  };
  const busy = phase !== "edit";
  return (
    <div class="callout mt-4" data-testid="quick-start">
      <div class="font-semibold text-sm">{t("Quick start")}</div>
      <p class="text-xs text-[var(--t3)] mt-1">
        {t("Three answers and your first kitty is ready to chat. The rest can wait.")}
      </p>
      <label class="block text-xs text-[var(--t3)] mt-3 mb-1" for="qs-kind">
        {t("Its brain")}
      </label>
      <select
        id="qs-kind"
        class="w-full px-3 py-2 text-sm bg-transparent border border-white/10 rounded"
        value={choice}
        disabled={busy}
        onChange={(e) => {
          const c = (e.target as HTMLSelectElement).value as Choice;
          touched.current = true;
          setChoice(c);
          // A plan where one signs in from the page; Google's needs gcloud, so its key first.
          setWay(SIGN_IN[c] && c !== "gemini" ? "login" : "key");
          setErr("");
        }}
      >
        {CHOICES.map((c) => (
          <option key={c} value={c}>
            {PROVIDERS[c].label}
          </option>
        ))}
      </select>
      <p class="text-xs text-[var(--t4)] mt-1">{meta.blurb}</p>
      {signIn && !signsOnly(choice) && (
        <div class="flex gap-2 mt-3" role="radiogroup" aria-label={t("How to pay for it")}>
          {(["key", "login"] as const).map((w) => (
            <button
              key={w}
              type="button"
              role="radio"
              aria-checked={way === w}
              class={`plain way ${way === w ? "on" : ""}`}
              disabled={busy}
              onClick={() => {
                touched.current = true;
                setWay(w);
                setErr("");
              }}
              data-testid={`quick-start-${w}`}
            >
              <div class="text-sm font-semibold">{w === "key" ? t("API key") : signIn.label}</div>
              <div class="text-xs text-[var(--t3)]">
                {w === "key"
                  ? t("Pay per use with a key")
                  : signIn.unavailable
                    ? t("Not available")
                    : t("Sign in with your subscription")}
              </div>
            </button>
          ))}
        </div>
      )}
      {login ? (
        <AccountSignIn signIn={signIn} account={account} onAccounts={loadAccounts} />
      ) : needsKey ? (
        <>
          <label class="block text-xs text-[var(--t3)] mt-3 mb-1" for="qs-key">
            {t("Its key")}
            <Required />
          </label>
          <input
            id="qs-key"
            class="w-full px-3 py-2 text-sm font-mono bg-transparent border border-white/10 rounded"
            type="password"
            autocomplete="off"
            value={key}
            disabled={busy}
            onInput={(e) => setKey((e.target as HTMLInputElement).value)}
            placeholder={t("paste the key")}
          />
          {meta.keyURL && (
            <a
              class="text-xs text-[var(--accent)] mt-1 inline-block"
              href={meta.keyURL}
              target="_blank"
              rel="noreferrer"
              onClick={(e) => {
                e.preventDefault();
                openExternal(meta.keyURL!);
              }}
            >
              {t("Get a key")}
            </a>
          )}
        </>
      ) : (
        <p class="text-xs text-[var(--t3)] mt-2">
          {t("Ollama has to be running on this computer, at {addr}.", { addr: OLLAMA })}
        </p>
      )}
      <label class="block text-xs text-[var(--t3)] mt-3 mb-1" for="qs-name">
        {t("Your kitty's name")}
        <Required />
      </label>
      <input
        id="qs-name"
        class="w-full px-3 py-2 text-sm bg-transparent border border-white/10 rounded"
        value={name}
        disabled={busy}
        onInput={(e) => setName((e.target as HTMLInputElement).value)}
      />
      {err && (
        <p class="text-sm text-[var(--c-red)] mt-2" role="alert">
          {err}
        </p>
      )}
      <button
        class="btn-accent w-full mt-4 px-4 py-2 text-sm"
        disabled={busy || missing.length > 0}
        onClick={start}
        data-testid="quick-start-go"
      >
        {phase === "model"
          ? t("Checking the model…")
          : phase === "kitty"
            ? t("Creating {name}…", { name: name.trim() })
            : t("Start")}
      </button>
      <Missing items={missing} />
    </div>
  );
}
