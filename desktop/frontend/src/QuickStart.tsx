import { useEffect, useState } from "preact/hooks";
import type { ProviderKind } from "./api";
import { api, errText, openExternal } from "./api";
import { friendlyError, PROVIDERS } from "./friendly";
import { t } from "./i18n";
import { Missing, Required } from "./Required";

// THE FIRST START, IN ONE SCREEN.
//
// With no model and no kitty yet, Welcome offers this: pick where the brain comes from, paste its
// key, name the kitty, and one button saves the model, checks it answers, creates the kitty and
// opens a chat with it. Everything else (more models, a look, connections) waits in Settings.

const CHOICES = ["anthropic", "openai", "gemini", "ollama"] as const;
type Choice = (typeof CHOICES)[number];

// The address of Ollama on this computer, its usual one.
const OLLAMA = "http://127.0.0.1:11434";

export default function QuickStart({ onDone }: { onDone: (chat: string) => void }) {
  const [kind, setKind] = useState<Choice>("anthropic");
  const [key, setKey] = useState("");
  const [name, setName] = useState("Mimi");
  const [kinds, setKinds] = useState<ProviderKind[]>([]);
  const [phase, setPhase] = useState<"edit" | "model" | "kitty">("edit");
  const [err, setErr] = useState("");
  useEffect(() => {
    api
      .settings()
      .then((s) => setKinds(s.kinds || []))
      .catch(() => setKinds([]));
  }, []);
  const needsKey = kind !== "ollama";
  const meta = PROVIDERS[kind];
  const missing = [
    needsKey && !key.trim() && t("the key"),
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
        model: kinds.find((k) => k.Name === kind)?.DefaultModel || "",
        base_url: kind === "ollama" ? OLLAMA : "",
      };
      if (needsKey) {
        body.api_key = key.trim();
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
        value={kind}
        disabled={busy}
        onChange={(e) => {
          setKind((e.target as HTMLSelectElement).value as Choice);
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
      {needsKey ? (
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
