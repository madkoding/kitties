import type { ComponentChildren } from "preact";
import { useEffect, useErrorBoundary, useMemo, useState } from "preact/hooks";
import type {
  Account,
  ConnectorKind,
  ConnectorView,
  Kitty,
  ProviderKind,
  ProviderView,
  SettingsView,
} from "./api";
import { api, errText, inWails, oauthStart, openExternal, savedConnection } from "./api";
import ConnectorLogo, { ProviderLogo, logoColor } from "./ConnectorLogo";
import {
  PROVIDER_ORDER,
  PROVIDERS,
  SIGN_IN,
  actionLabel,
  categoryLabel,
  friendlyError,
  mayUse,
  people,
  pickOf,
  providerLabel,
  signedInWith,
  uniqueName,
  withConnection,
} from "./friendly";
import { IconPlus, IconTrash, IconX } from "./icons";
import { ConnectModal } from "./Modals";
import { Missing, Required, RequiredLegend } from "./Required";
import { AccountSignIn } from "./SignIn";

// SETTINGS, FOR PEOPLE.
//
// Three things a person sets up here, in plain words: the brains the kitties think with
// (models), the services they may act in (connections, and which kitty may use each), and how
// they are reached when away (notifications). Every form asks only for what it cannot guess;
// names, addresses and limits wait under "More options".

export type Tab = "models" | "connectors" | "notifications" | "gateway";

const LABEL = "block text-xs text-[var(--t3)] mb-1 mt-3 uppercase tracking-wider font-mono";
const INPUT = "w-full px-3 py-2 text-sm";
const HELP = "text-xs text-[var(--t4)] mt-1";

// useFlash shows a short confirmation that fades by itself.
function useFlash(): [string, (msg: string) => void] {
  const [msg, setMsg] = useState("");
  useEffect(() => {
    if (!msg) {
      return;
    }
    const id = setTimeout(() => setMsg(""), 2500);
    return () => clearTimeout(id);
  }, [msg]);
  return [msg, setMsg];
}

function More({
  children,
  label = "More options",
}: {
  children: ComponentChildren;
  label?: string;
}) {
  return (
    <details class="more mt-3">
      <summary>{label}</summary>
      <div class="pt-1">{children}</div>
    </details>
  );
}

function Err({ text }: { text: string }) {
  return text ? (
    <p class="text-sm text-[var(--c-red)] mt-2" role="alert">
      {text}
    </p>
  ) : null;
}

// ─── Models ─────────────────────────────────────────────────────────────────────────────────

// ProviderPicker offers the providers as buttons, the common ones first.
function ProviderPicker({ kinds, onPick }: { kinds: ProviderKind[]; onPick: (k: string) => void }) {
  const names = kinds.map((k) => k.Name);
  const order = [
    ...PROVIDER_ORDER.filter((n) => names.includes(n)),
    ...names.filter((n) => !PROVIDER_ORDER.includes(n)),
  ];
  return (
    <div class="grid sm:grid-cols-2 gap-2 mt-2" data-testid="provider-picker">
      {order.map((n) => (
        <button
          key={n}
          type="button"
          class="plain pick-card"
          onClick={() => onPick(n)}
          aria-label={`${providerLabel(n)}: ${PROVIDERS[n]?.blurb || n}`}
        >
          <span class="provider-badge">
            <ProviderLogo kind={n} size={22} />
          </span>
          <span class="flex flex-col min-w-0">
            <span class="font-semibold text-sm">{providerLabel(n)}</span>
            <span class="text-xs text-[var(--t3)]">{PROVIDERS[n]?.blurb || n}</span>
            {SIGN_IN[n] && !SIGN_IN[n].unavailable && (
              <span class="text-xs text-[var(--color-accent)]">
                API key or your {SIGN_IN[n].label}
              </span>
            )}
          </span>
        </button>
      ))}
    </div>
  );
}

// ModelForm adds or edits a model. A new one starts with the provider, then how to pay for it:
// an API key (write-only: a saved one is never shown again) or a subscription the person signs in
// with. Saving tests it at once and says how it went.
function ModelForm({
  view,
  kinds,
  taken,
  accounts,
  onAccounts,
  onSaved,
  onClose,
}: {
  view: ProviderView | null;
  kinds: ProviderKind[];
  taken: string[];
  accounts: Account[];
  onAccounts: () => void;
  onSaved: (s: SettingsView, note: string) => void;
  onClose: () => void;
}) {
  // picked is the provider card; kind is what the profile is saved as (a Claude subscription is
  // Anthropic's card, saved as claude-code).
  const [picked, setPicked] = useState(view ? pickOf(view.kind, view.auth) : "");
  const signIn = SIGN_IN[picked];
  const [way, setWay] = useState<"key" | "login">(
    view && (view.auth === "login" || view.kind === "claude-code") ? "login" : "key",
  );
  const kind = way === "login" && signIn ? signIn.kind : picked;
  const info = kinds.find((k) => k.Name === kind);
  const meta = PROVIDERS[picked] || { label: picked, blurb: "" };
  const account = signIn && accounts.find((a) => a.kind === signIn.kind);
  const [model, setModel] = useState(view?.model || "");
  const [key, setKey] = useState("");
  const [base, setBase] = useState(view?.base_url || "");
  const [maxTokens, setMaxTokens] = useState(view?.max_tokens ? String(view.max_tokens) : "");
  const [name, setName] = useState(view?.name || "");
  const [err, setErr] = useState("");
  const [phase, setPhase] = useState<"edit" | "saving" | "testing" | "failed">("edit");
  const [saved, setSaved] = useState<SettingsView | null>(null);
  const defaultModel = (k: string) => kinds.find((x) => x.Name === k)?.DefaultModel || "";
  const pick = (k: string) => {
    // Copilot has no key of its own to speak of: its sign-in comes first.
    const w = k === "copilot" ? "login" : "key";
    setPicked(k);
    setWay(w);
    setModel(defaultModel(w === "login" ? SIGN_IN[k].kind : k));
    setBase("");
  };
  const choose = (w: "key" | "login") => {
    setWay(w);
    setErr("");
    setModel(defaultModel(w === "login" && signIn ? signIn.kind : picked));
  };
  if (!picked) {
    return (
      <div class="form-card">
        <div class="font-semibold">Which provider?</div>
        <p class="text-xs text-[var(--t3)]">
          Pick where the brain comes from. Pay with an API key, or sign in with a subscription you
          already have (Claude, ChatGPT, GitHub Copilot). Ollama on this computer is free.
        </p>
        <ProviderPicker kinds={kinds} onPick={pick} />
        <div class="flex justify-end mt-4">
          <button class="btn-ghost px-4 py-2 text-sm" onClick={onClose}>
            Cancel
          </button>
        </div>
      </div>
    );
  }
  const needsKey = way === "key" && !info?.NoKey;
  const showAddress = way === "key" && Boolean(meta.address);
  const finalName = view?.name || name.trim() || uniqueName(kind, taken);
  const save = async () => {
    setErr("");
    setPhase("saving");
    try {
      const body: Record<string, unknown> = { kind, model: model.trim(), base_url: base.trim() };
      if (way === "login" && kind !== "claude-code") {
        body.auth = "login";
      }
      if (way === "key" && key.trim()) {
        body.api_key = key.trim();
      }
      if (maxTokens.trim()) {
        body.max_tokens = Number(maxTokens);
      }
      const s = await api.setProvider(finalName, body);
      setSaved(s);
      setPhase("testing");
      const r = await api.testProvider(finalName).catch((e) => ({ ok: false, error: errText(e) }));
      if (r.ok) {
        onSaved(s, `${meta.label} answered: it works.`);
      } else {
        setErr(friendlyError((r as { error?: string }).error || "it did not answer"));
        setPhase("failed");
      }
    } catch (e) {
      setErr(friendlyError(errText(e)));
      setPhase("edit");
    }
  };
  const missingKey = needsKey && !key.trim() && !(view?.api_key_set && view.kind === kind);
  const missingLogin = way === "login" && (!account?.signed_in || Boolean(signIn?.unavailable));
  const stillNeeded = [
    missingKey && "the API key",
    missingLogin && "sign in",
    !model.trim() && "the model",
  ].filter(Boolean) as string[];
  return (
    <div class="form-card" data-testid="model-form">
      <div class="flex items-start gap-3">
        <span class="provider-badge">
          <ProviderLogo kind={kind} size={26} />
        </span>
        <div class="flex-1 min-w-0">
          <div class="font-semibold">{meta.label}</div>
          <div class="text-xs text-[var(--t3)]">{meta.blurb}</div>
        </div>
        {!view && (
          <button type="button" class="btn-ghost px-3 py-1.5 text-xs" onClick={() => setPicked("")}>
            change
          </button>
        )}
      </div>
      {signIn && (
        <div class="flex gap-2 mt-3" role="radiogroup" aria-label="How to pay for it">
          <button
            type="button"
            role="radio"
            aria-checked={way === "key"}
            class={`plain way ${way === "key" ? "on" : ""}`}
            onClick={() => choose("key")}
          >
            <div class="text-sm font-semibold">API key</div>
            <div class="text-xs text-[var(--t3)]">Pay per use with a key</div>
          </button>
          <button
            type="button"
            role="radio"
            aria-checked={way === "login"}
            class={`plain way ${way === "login" ? "on" : ""}`}
            onClick={() => choose("login")}
          >
            <div class="text-sm font-semibold">{signIn.label}</div>
            <div class="text-xs text-[var(--t3)]">
              {signIn.unavailable ? "Not available" : "Sign in with your subscription"}
            </div>
          </button>
        </div>
      )}
      {way === "login" && signIn && (
        <AccountSignIn signIn={signIn} account={account} onAccounts={onAccounts} />
      )}
      {way === "key" && (needsKey || kind === "custom") && (
        <>
          <label class={LABEL} for="model-key">
            API key{needsKey ? <Required /> : " (if the server asks for one)"}
          </label>
          <input
            id="model-key"
            class={`${INPUT} font-mono`}
            aria-required={missingKey}
            type="password"
            autocomplete="off"
            value={key}
            onInput={(e) => setKey((e.target as HTMLInputElement).value)}
            placeholder={view?.api_key_set ? "saved: leave empty to keep it" : "paste the key here"}
          />
          <p class={HELP}>
            Kept on your gateway and never shown again.{" "}
            {meta.keyURL && (
              <a
                href={meta.keyURL}
                onClick={(e) => {
                  e.preventDefault();
                  openExternal(meta.keyURL!);
                }}
              >
                Get a key from {meta.label} ↗
              </a>
            )}
          </p>
        </>
      )}
      {showAddress && (
        <>
          <label class={LABEL} for="model-address">
            Server address
          </label>
          <input
            id="model-address"
            class={INPUT}
            value={base}
            onInput={(e) => setBase((e.target as HTMLInputElement).value)}
            placeholder={info?.BaseURL || "http://127.0.0.1:1234/v1"}
          />
          <p class={HELP}>
            {kind === "ollama"
              ? "Leave it empty when Ollama runs on this computer. "
              : "The address of the server's API, usually ending in /v1. "}
            {kind === "ollama" && meta.keyURL && (
              <a
                href={meta.keyURL}
                onClick={(e) => {
                  e.preventDefault();
                  openExternal(meta.keyURL!);
                }}
              >
                Download Ollama ↗
              </a>
            )}
          </p>
        </>
      )}
      <label class={LABEL} for="model-name">
        Model
        <Required />
      </label>
      <input
        id="model-name"
        class={`${INPUT} font-mono`}
        aria-required="true"
        list="model-suggestions"
        value={model}
        onInput={(e) => setModel((e.target as HTMLInputElement).value)}
        placeholder={info?.DefaultModel || "model name"}
      />
      <datalist id="model-suggestions">
        {[
          ...new Set(
            [
              info?.DefaultModel,
              ...((way === "login" ? signIn?.models : meta.models) || []),
            ].filter(Boolean),
          ),
        ].map((m) => (
          <option key={m} value={m} />
        ))}
      </datalist>
      <p class={HELP}>Not sure? Keep the suggested one.</p>
      <More>
        <label class={LABEL} for="model-id">
          Name in Kitties
        </label>
        <input
          id="model-id"
          class={INPUT}
          value={view?.name || name}
          disabled={Boolean(view)}
          onInput={(e) => setName((e.target as HTMLInputElement).value)}
          placeholder={uniqueName(kind, taken)}
        />
        <p class={HELP}>How the kitties and the configuration file call it.</p>
        {!showAddress && way === "key" && (
          <>
            <label class={LABEL} for="model-base">
              Server address
            </label>
            <input
              id="model-base"
              class={INPUT}
              value={base}
              onInput={(e) => setBase((e.target as HTMLInputElement).value)}
              placeholder={info?.BaseURL}
            />
            <p class={HELP}>Only to reach it through a proxy or a compatible service.</p>
          </>
        )}
        <label class={LABEL} for="model-max">
          Longest answer (tokens)
        </label>
        <input
          id="model-max"
          class={INPUT}
          inputMode="numeric"
          value={maxTokens}
          onInput={(e) => setMaxTokens((e.target as HTMLInputElement).value.replace(/\D/g, ""))}
          placeholder="4096"
        />
      </More>
      {phase === "failed" ? (
        <div class="callout callout-warn mt-3">
          <div>
            Saved, but the test did not pass: <b>{err}</b>
          </div>
          <div class="flex gap-2 mt-2">
            <button class="btn-ghost px-3 py-1.5 text-xs" onClick={() => setPhase("edit")}>
              Change something
            </button>
            <button
              class="btn-ghost px-3 py-1.5 text-xs"
              onClick={() => saved && onSaved(saved, "Saved without a passing test.")}
            >
              Keep it anyway
            </button>
          </div>
        </div>
      ) : (
        <Err text={err} />
      )}
      {phase !== "failed" && (
        <>
          <div class="flex items-center justify-end gap-2 mt-4">
            <span class="flex-1">
              <RequiredLegend />
            </span>
            <button class="btn-ghost px-4 py-2 text-sm" onClick={onClose}>
              Cancel
            </button>
            <button
              class="btn-accent px-4 py-2 text-sm"
              disabled={phase !== "edit" || stillNeeded.length > 0}
              title={stillNeeded.length ? `Still needed: ${stillNeeded.join(", ")}` : undefined}
              onClick={save}
            >
              {phase === "saving" ? "Saving…" : phase === "testing" ? "Checking…" : "Save and test"}
            </button>
          </div>
          <Missing items={stillNeeded} />
        </>
      )}
    </div>
  );
}

function ModelRow({
  p,
  onEdit,
  onChanged,
}: {
  p: ProviderView;
  onEdit: () => void;
  onChanged: (s: SettingsView) => void;
}) {
  const [test, setTest] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const label = providerLabel(pickOf(p.kind, p.auth));
  const plan = signedInWith(p.kind, p.auth);
  const run = async () => {
    setBusy(true);
    setTest("");
    try {
      const r = await api.testProvider(p.name);
      setTest(r.ok ? "✓ It answered: it works." : "✗ " + friendlyError(r.error || "no answer"));
    } catch (e) {
      setTest("✗ " + friendlyError(errText(e)));
    }
    setBusy(false);
  };
  const remove = async () => {
    const users = p.used_by || [];
    const what = users.length
      ? `${people(users)} will switch to another model.`
      : "No kitty uses it.";
    if (!confirm(`Remove ${label} (${p.model})? ${what}`)) {
      return;
    }
    setErr("");
    try {
      onChanged(await api.removeProvider(p.name));
    } catch (e) {
      setErr(errText(e));
    }
  };
  const users = p.used_by || [];
  return (
    <div class="item" data-testid={`model-${p.name}`}>
      <span class="provider-badge">
        <ProviderLogo kind={p.kind} size={22} />
        <span class={`dot ${p.error ? "error" : "running"} provider-dot`} />
      </span>
      <div class="flex-1 min-w-0">
        <div class="flex items-center gap-2 flex-wrap">
          <span class="text-sm font-semibold">{label}</span>
          <span class="text-xs font-mono text-[var(--t3)] truncate">{p.model}</span>
          {p.default && <span class="tag tag-green">default</span>}
          {plan && <span class="tag">{plan}</span>}
        </div>
        <div class="text-xs text-[var(--t3)] mt-0.5">
          {p.error ? (
            <span class="text-[var(--c-red)]">{friendlyError(p.error)}</span>
          ) : users.length ? (
            `Used by ${people(users)}`
          ) : (
            "No kitty uses it yet"
          )}
          {p.name !== p.kind && <span class="text-[var(--t4)]"> · {p.name}</span>}
          {p.from_file && <span class="text-[var(--t4)]"> · from the configuration file</span>}
        </div>
        {test && (
          <div
            class={`text-xs mt-1 ${test.startsWith("✓") ? "text-[var(--c-green)]" : "text-[var(--c-red)]"}`}
          >
            {test}
          </div>
        )}
        <Err text={err} />
      </div>
      <div class="flex gap-1 flex-none flex-wrap justify-end">
        <button class="btn-ghost px-2 py-1 text-xs" disabled={busy} onClick={run}>
          {busy ? "Testing…" : "Test"}
        </button>
        {!p.default && !p.error && (
          <button
            class="btn-ghost px-2 py-1 text-xs"
            onClick={() =>
              api
                .setDefaultProvider(p.name)
                .then(onChanged)
                .catch((e) => setErr(errText(e)))
            }
          >
            Make default
          </button>
        )}
        <button class="btn-ghost px-2 py-1 text-xs" onClick={onEdit} aria-label={`Edit ${p.name}`}>
          Edit
        </button>
        <button
          class="btn-ghost px-2 py-1 text-xs text-[var(--c-red)]"
          aria-label={`Remove ${p.name}`}
          onClick={remove}
        >
          <IconTrash size={12} />
        </button>
      </div>
    </div>
  );
}

// ─── Connections ────────────────────────────────────────────────────────────────────────────

// ServiceGrid is the catalogue as buttons with their logos, filtered by a search and a category.
function ServiceGrid({
  catalog,
  categories,
  onPick,
}: {
  catalog: ConnectorKind[];
  categories: string[];
  onPick: (k: ConnectorKind) => void;
}) {
  const [q, setQ] = useState("");
  const [cat, setCat] = useState("");
  const words = q.trim().toLowerCase();
  const shown = catalog.filter(
    (k) =>
      (!cat || k.category === cat) &&
      (!words ||
        k.label.toLowerCase().includes(words) ||
        k.name.includes(words) ||
        k.help.toLowerCase().includes(words)),
  );
  return (
    <div class="service-grid">
      <input
        class={`${INPUT} mt-2`}
        type="search"
        aria-label="Search services"
        placeholder="Search: Slack, Gmail, GitHub, Stripe…"
        value={q}
        onInput={(e) => setQ((e.target as HTMLInputElement).value)}
      />
      <div class="flex flex-wrap gap-1 mt-2">
        {["", ...categories].map((c) => (
          <button
            key={c || "all"}
            type="button"
            class={`chip ${cat === c ? "on" : ""}`}
            onClick={() => setCat(c)}
          >
            {c ? categoryLabel(c) : "All"}
          </button>
        ))}
      </div>
      <div class="flex flex-wrap gap-2 mt-3">
        {shown.map((k) => (
          <button
            key={k.name}
            type="button"
            class="plain service-btn"
            style={`--brand:${logoColor(k.name)}`}
            onClick={() => onPick(k)}
            title={k.help}
          >
            <ConnectorLogo kind={k.name} size={26} />
            <span>{k.label}</span>
            {k.oauth && <span class="service-oauth">sign in</span>}
          </button>
        ))}
        {shown.length === 0 && (
          <p class="text-sm text-[var(--t4)]">Nothing matches: try another word.</p>
        )}
      </div>
    </div>
  );
}

// OAUTH_LABEL names the identity services a connector can sign in with.
const OAUTH_LABEL: Record<string, string> = {
  google: "Google",
  microsoft: "Microsoft",
  slack: "Slack",
  github: "GitHub",
  gitlab: "GitLab",
  spotify: "Spotify",
  tidal: "TIDAL",
  dropbox: "Dropbox",
  notion: "Notion",
  todoist: "Todoist",
  linear: "Linear",
  airtable: "Airtable",
  figma: "Figma",
  atlassian: "Atlassian",
  zendesk: "Zendesk",
  x: "X",
  linkedin: "LinkedIn",
  facebook: "Facebook",
  mastodon: "Mastodon",
  "home-assistant": "Home Assistant",
};

// A connection that signs in keeps a key or a token as the other way in, and an app of the
// person's own as a third; these are their fields.
const KEY_FIELDS = ["token", "email"];
const APP_FIELDS = ["client_id", "client_secret"];

// gatewayBase is the gateway's address as the browser reaches it: where a sign-in comes back.
function gatewayBase(): string {
  const saved = savedConnection().url;
  if (saved) {
    return saved.replace(/\/$/, "");
  }
  return inWails() ? "http://127.0.0.1:7488" : location.origin;
}

function FieldInput({
  f,
  value,
  saved,
  placeholder,
  onInput,
}: {
  f: ConnectorKind["fields"][number];
  value: string;
  saved: boolean;
  placeholder?: string;
  onInput: (v: string) => void;
}) {
  const id = "field-" + f.name;
  return (
    <div>
      <label class={LABEL} for={id}>
        {f.label}
        {f.required ? <Required /> : " (optional)"}
      </label>
      {f.name === "headers" ? (
        <textarea
          id={id}
          class={`${INPUT} font-mono`}
          aria-required={f.required}
          value={value}
          onInput={(e) => onInput((e.target as HTMLTextAreaElement).value)}
          placeholder={f.help}
        />
      ) : f.secret ? (
        <input
          id={id}
          class={`${INPUT} font-mono`}
          type="password"
          autocomplete="off"
          aria-required={f.required && !saved}
          value={value}
          onInput={(e) => onInput((e.target as HTMLInputElement).value)}
          placeholder={saved ? "saved: leave empty to keep it" : placeholder || f.placeholder || ""}
        />
      ) : (
        <input
          id={id}
          class={INPUT}
          type="text"
          autocomplete="off"
          aria-required={f.required}
          value={value}
          onInput={(e) => onInput((e.target as HTMLInputElement).value)}
          placeholder={placeholder || f.placeholder || ""}
        />
      )}
      {f.help && <p class={HELP}>{f.help}</p>}
    </div>
  );
}

// ConnectionForm adds or edits a connection: the fields the service needs, which kitties may
// use it, and (under "More options") its name, its description for the kitties and the
// optional fields.
function ConnectionForm({
  view,
  kind,
  catalog,
  kitties,
  taken,
  motitaURL,
  ready: readyList = [],
  onSaved,
  onClose,
  onChange,
}: {
  view: ConnectorView | null;
  kind: ConnectorKind;
  catalog: ConnectorKind[];
  kitties: Kitty[];
  taken: string[];
  motitaURL?: string;
  // ready are the services Kitties signs in to without an app of the person's own.
  ready?: string[];
  onSaved: (s: SettingsView, name: string) => void;
  onClose: () => void;
  onChange: () => void;
}) {
  const def = catalog.find((k) => k.name === kind.name) || kind;
  const [name, setName] = useState(view?.name || "");
  const [description, setDescription] = useState(view?.description || "");
  const [values, setValues] = useState<Record<string, string>>({ ...(view?.settings || {}) });
  // Who may use it starts from the kitties' own lists (all of them for a new one), and follows
  // the list of kitties if it arrives later, until the person changes it.
  const initialWho = () =>
    new Set(
      (view ? kitties.filter((k) => mayUse(k.connectors, view.name)) : kitties).map((k) => k.name),
    );
  const [who, setWho] = useState<Set<string>>(initialWho);
  const [touched, setTouched] = useState(false);
  useEffect(() => {
    if (!touched) {
      setWho(initialWho());
    }
  }, [kitties]);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const set = (k: string, v: string) => setValues({ ...values, [k]: v });
  const finalName = view?.name || name.trim() || uniqueName(def.name, taken);
  const signs = Boolean(def.oauth);
  const keyFields = signs ? def.fields.filter((f) => KEY_FIELDS.includes(f.name)) : [];
  const appFields = signs ? def.fields.filter((f) => APP_FIELDS.includes(f.name)) : [];
  const required = def.fields.filter((f) => f.required);
  const optional = def.fields.filter(
    (f) => !f.required && !keyFields.includes(f) && !appFields.includes(f),
  );
  const [signedIn, setSignedIn] = useState(Boolean(view?.signed_in));
  const [account, setAccount] = useState(view?.account || "");
  const [waiting, setWaiting] = useState(false);
  // Signing in needs no app of the person's when Kitties has one or the service needs none.
  const ready =
    signs &&
    (readyList.includes(def.oauth || "") ||
      Boolean((values.client_id || "").trim()) ||
      Boolean(view?.settings?.client_id));
  const label = OAUTH_LABEL[def.oauth || ""] || def.label;
  const filled = (n: string) =>
    Boolean((values[n] || "").trim()) || (view?.secrets_set || []).includes(n);
  const missing = required.filter((f) => !filled(f.name));
  // A service that signs in still needs an account to work: the sign-in, or the key typed
  // instead (with its email when the service takes one), or Slack's webhook.
  const noAccount =
    signs &&
    !signedIn &&
    !(keyFields.length > 0 && keyFields.every((f) => filled(f.name))) &&
    !(def.name === "slack" && filled("url"));
  const keyWord = def.name === "jira" || def.name === "zendesk" ? "an API token" : "a key";
  const stillNeeded = [
    ...missing.map((f) => f.label),
    noAccount && (keyFields.length ? `sign in, or ${keyWord} instead` : "sign in"),
  ].filter(Boolean) as string[];
  const toggle = (n: string) => {
    setTouched(true);
    const next = new Set(who);
    next.has(n) ? next.delete(n) : next.add(n);
    setWho(next);
  };
  // persist saves the connection and who may use it; it reports whether it went through.
  const persist = async (signingIn = false): Promise<boolean> => {
    setBusy(true);
    setErr("");
    try {
      const settings: Record<string, string> = {};
      for (const [k, v] of Object.entries(values)) {
        if (v.trim()) {
          settings[k] = v.trim();
        }
      }
      await api.setConnector(finalName, { kind: def.name, description, settings }, signingIn);
      // Who may use it: each kitty's own list of connections changes (a kitty that may use all
      // of them has it already).
      const all = await api.connectorNames();
      for (const k of kitties) {
        const has = mayUse(k.connectors, finalName);
        const want = who.has(k.name);
        if (has !== want) {
          await api.editKitty(k.id, {
            connectors: withConnection(k.connectors, finalName, want, all),
          });
        }
      }
      onChange();
      return true;
    } catch (e) {
      setErr(errText(e));
      setBusy(false);
      return false;
    }
  };
  const save = async () => {
    if (await persist()) {
      onSaved(await api.settings(), finalName);
    }
  };
  // signIn saves what is filled in, opens the service's sign-in, and follows it until the
  // gateway has the account.
  const signIn = async () => {
    if (!(await persist(true))) {
      return;
    }
    try {
      openExternal(await oauthStart(finalName));
    } catch (e) {
      setErr(errText(e));
      setBusy(false);
      return;
    }
    setBusy(false);
    setWaiting(true);
    const until = Date.now() + 3 * 60_000;
    const look = async () => {
      const s = await api.settings().catch(() => null);
      const c = s?.connectors.find((x) => x.name === finalName);
      if (s && c?.signed_in) {
        setWaiting(false);
        setSignedIn(true);
        setAccount(c.account || "");
        onChange();
        onSaved(s, finalName);
        return;
      }
      if (Date.now() < until) {
        setTimeout(look, 2500);
      } else {
        setWaiting(false);
      }
    };
    setTimeout(look, 2500);
  };
  const field = (f: ConnectorKind["fields"][number]) => (
    <FieldInput
      key={f.name}
      f={f}
      value={values[f.name] || ""}
      saved={(view?.secrets_set || []).includes(f.name)}
      placeholder={f.name === "url" && def.name === "motita" ? motitaURL : undefined}
      onInput={(v) => set(f.name, v)}
    />
  );
  return (
    <div class="form-card" data-testid="connection-form">
      <div class="flex items-center gap-3">
        <span class="service-badge" style={`--brand:${logoColor(def.name)}`}>
          <ConnectorLogo kind={def.name} size={28} />
        </span>
        <div class="flex-1 min-w-0">
          <div class="font-semibold">{def.label}</div>
          <div class="text-xs text-[var(--t3)]">{def.help}</div>
        </div>
      </div>
      {required.map(field)}
      {signs && (
        <div class="callout mt-3" data-testid="connection-sign-in">
          {signedIn ? (
            <div class="flex items-center gap-2 flex-wrap">
              <span class="flex-1 text-sm">
                ✓ Signed in{account ? ` as ${account}` : ""}. The kitties use this account.
              </span>
              <button
                type="button"
                class="btn-ghost px-3 py-1.5 text-xs"
                disabled={busy || missing.length > 0}
                onClick={signIn}
              >
                Sign in again
              </button>
            </div>
          ) : (
            <>
              <button
                type="button"
                class="btn-accent px-4 py-2 text-sm flex items-center gap-2"
                disabled={busy || waiting || missing.length > 0 || !ready}
                title={
                  missing.length
                    ? `Fill in: ${missing.map((f) => f.label).join(", ")}`
                    : !ready
                      ? "Add the client id of your app first"
                      : undefined
                }
                onClick={signIn}
                data-testid="connection-sign-in-button"
              >
                <ConnectorLogo kind={def.name} size={16} mono />
                {waiting ? "Waiting for the browser…" : `Sign in with ${label}`}
              </button>
              <p class="text-xs text-[var(--t3)] mt-2">
                {waiting
                  ? "Finish signing in in your browser; this changes by itself."
                  : ready
                    ? "The easy way: sign in and allow Kitties. Nothing to copy."
                    : `To sign in, ${label} needs an app of your own for now: create one with the redirect address below and paste its client id.`}
              </p>
              {!ready && (
                <>
                  <code class="command mt-2">{gatewayBase()}/v1/oauth/callback</code>
                  {appFields.map(field)}
                </>
              )}
            </>
          )}
        </div>
      )}
      {keyFields.length > 0 && (
        <More
          label={
            def.name === "jira" || def.name === "zendesk"
              ? "Use an API token instead"
              : "Use an API key or a token instead"
          }
        >
          {keyFields.map(field)}
        </More>
      )}
      {ready && appFields.length > 0 && (
        <More label="Use an app of your own">{appFields.map(field)}</More>
      )}
      {kitties.length > 0 && (
        <>
          <label class={LABEL}>Which kitties may use it</label>
          <div class="flex flex-wrap gap-2" data-testid="connection-kitties">
            {kitties.map((k) => (
              <label key={k.id} class={`chip ${who.has(k.name) ? "on" : ""}`}>
                <input
                  type="checkbox"
                  class="sr-only"
                  checked={who.has(k.name)}
                  onChange={() => toggle(k.name)}
                />
                {k.name}
              </label>
            ))}
          </div>
        </>
      )}
      {def.actions.length > 0 && (
        <More label="What the kitties can do with it">
          <ul class="text-xs text-[var(--t3)] flex flex-col gap-1 mt-1">
            {def.actions.map((a) => (
              <li key={a.name}>
                <b class="text-[var(--t2)]">{actionLabel(a.name)}</b>: {a.help}
                {a.risk === "always" && <span class="tag tag-amber ml-1">always asks you</span>}
                {a.risk === "ask" && <span class="tag ml-1">asks you first</span>}
              </li>
            ))}
          </ul>
        </More>
      )}
      <More>
        {optional.map(field)}
        <label class={LABEL} for="connection-name">
          Name in Kitties
        </label>
        <input
          id="connection-name"
          class={INPUT}
          value={view?.name || name}
          disabled={Boolean(view)}
          onInput={(e) => setName((e.target as HTMLInputElement).value)}
          placeholder={uniqueName(def.name, taken)}
        />
        <label class={LABEL} for="connection-description">
          What the kitties are told it is for
        </label>
        <input
          id="connection-description"
          class={INPUT}
          value={description}
          onInput={(e) => setDescription((e.target as HTMLInputElement).value)}
          placeholder={def.help}
        />
      </More>
      <Err text={err} />
      <div class="flex items-center justify-end gap-2 mt-4">
        <span class="flex-1">
          <RequiredLegend />
        </span>
        <button class="btn-ghost px-4 py-2 text-sm" onClick={onClose}>
          Cancel
        </button>
        <button
          class="btn-accent px-4 py-2 text-sm"
          disabled={busy || stillNeeded.length > 0}
          title={stillNeeded.length ? `Still needed: ${stillNeeded.join(", ")}` : undefined}
          onClick={save}
        >
          {busy ? "Saving…" : "Save"}
        </button>
      </div>
      <Missing items={stillNeeded} />
    </div>
  );
}

function ConnectionRow({
  c,
  onEdit,
  onChanged,
}: {
  c: ConnectorView;
  onEdit: () => void;
  onChanged: (s: SettingsView) => void;
}) {
  const [err, setErr] = useState("");
  const users = c.used_by || [];
  return (
    <div class="item" data-testid={`connection-${c.name}`}>
      <span class="service-badge" style={`--brand:${logoColor(c.kind)}`}>
        <ConnectorLogo kind={c.kind} size={22} />
      </span>
      <div class="flex-1 min-w-0">
        <div class="flex items-center gap-2 flex-wrap">
          <span class="text-sm font-semibold">{c.label || c.kind}</span>
          {c.name !== c.kind && <span class="text-xs text-[var(--t4)]">{c.name}</span>}
          {c.signed_in && (
            <span class="tag tag-green">signed in{c.account ? " · " + c.account : ""}</span>
          )}
          {c.needs_sign_in && <span class="tag tag-amber">sign in needed</span>}
        </div>
        <div class="text-xs text-[var(--t3)] mt-0.5">
          {users.length ? `Used by ${people(users)}` : "No kitty may use it yet"}
          {c.from_file && <span class="text-[var(--t4)]"> · from the configuration file</span>}
        </div>
        <Err text={err} />
      </div>
      <div class="flex gap-1 flex-none">
        <button class="btn-ghost px-2 py-1 text-xs" onClick={onEdit} aria-label={`Edit ${c.name}`}>
          Edit
        </button>
        <button
          class="btn-ghost px-2 py-1 text-xs text-[var(--c-red)]"
          aria-label={`Remove ${c.name}`}
          onClick={() =>
            confirm(`Remove the connection ${c.label || c.name}? The kitties lose it too.`) &&
            api
              .removeConnector(c.name)
              .then(onChanged)
              .catch((e) => setErr(errText(e)))
          }
        >
          <IconTrash size={12} />
        </button>
      </div>
    </div>
  );
}

// ─── Notifications ──────────────────────────────────────────────────────────────────────────

// NOTIFY_LABELS say what each event is, as the page offers it.
const NOTIFY_LABELS: Record<string, string> = {
  help: "A kitty gave up and needs my help",
  approval: "An action is waiting for my yes",
  goal_done: "A goal finished",
  goal_blocked: "A goal could not finish",
  recovered: "A kitty's container was brought back",
  budget: "A kitty spent its day's tokens",
};

// NotifyForm chooses where the kitties tell you what happened while you were away (a push to
// the phone, Telegram, Slack, mail) and which events are worth a message.
function NotifyForm({
  view,
  onSaved,
  onSetUp,
}: {
  view: SettingsView;
  onSaved: (s: SettingsView) => void;
  onSetUp: () => void;
}) {
  const channels = view.connectors.filter((c) => c.category === "messaging");
  const [connector, setConnector] = useState(view.notify?.connector || "");
  const [on, setOn] = useState<string[]>(
    view.notify?.on?.length ? view.notify.on : ["help", "approval"],
  );
  const [digest, setDigest] = useState(view.notify?.digest || "");
  const [day, setDay] = useState(Boolean(view.notify?.day));
  const [msg, flash] = useFlash();
  const [err, setErr] = useState("");
  const toggle = (e: string) => setOn(on.includes(e) ? on.filter((x) => x !== e) : [...on, e]);
  const body = { connector, on, digest, day };
  const save = async () => {
    setErr("");
    try {
      onSaved(await api.setNotify(body));
      flash("Saved.");
    } catch (e) {
      setErr(errText(e));
    }
  };
  const test = async () => {
    setErr("");
    try {
      await api.setNotify(body);
      const r = await api.testNotify(connector);
      r.ok
        ? flash("Sent: look at your phone or channel.")
        : setErr(r.error || "It did not go through.");
    } catch (e) {
      setErr(errText(e));
    }
  };
  const sendDigest = async () => {
    setErr("");
    try {
      await api.setNotify(body);
      const r = await api.sendDigest();
      r.ok ? flash("Summary sent.") : setErr(r.error || "The summary did not go through.");
    } catch (e) {
      setErr(errText(e));
    }
  };
  return (
    <div data-testid="notify">
      <p class="text-sm text-[var(--t3)]">
        The kitties work while you are away. Choose how they reach you outside this window: a push
        to your phone is the quickest, and Telegram, Slack or email work too.
      </p>
      {channels.length === 0 && (
        <div class="callout mt-3">
          <div class="font-semibold text-sm">Get alerts on your phone</div>
          <p class="text-xs text-[var(--t3)] mt-1">
            Install the free ntfy app on your phone, choose a topic name and subscribe to it. Then
            connect the same topic here.
          </p>
          <button class="btn-accent px-3 py-1.5 text-xs mt-2" onClick={onSetUp}>
            Set up phone notifications
          </button>
        </div>
      )}
      <label class={LABEL} for="notify-connector">
        Send through
      </label>
      <select
        id="notify-connector"
        class={INPUT}
        value={connector}
        onChange={(e) => setConnector((e.target as HTMLSelectElement).value)}
      >
        <option value="">nowhere: notifications are off</option>
        {channels.map((c) => (
          <option key={c.name} value={c.name}>
            {c.label}
            {c.name !== c.kind ? ` (${c.name})` : ""}
          </option>
        ))}
      </select>
      <label class={LABEL}>Tell me when</label>
      <div class="flex flex-col gap-1">
        {view.notify_events.map((e) => (
          <label key={e} class="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={on.includes(e)} onChange={() => toggle(e)} />{" "}
            {NOTIFY_LABELS[e] || e}
          </label>
        ))}
      </div>
      <label class={LABEL} for="notify-digest">
        A summary of the last day, every day at
      </label>
      <div class="flex items-center gap-2 flex-wrap">
        <input
          id="notify-digest"
          class={`${INPUT} !w-32 font-mono`}
          type="time"
          value={digest}
          onInput={(e) => setDigest((e.target as HTMLInputElement).value)}
        />
        {digest && (
          <button class="btn-ghost px-3 py-1.5 text-xs" onClick={() => setDigest("")}>
            none
          </button>
        )}
        <button
          class="btn-ghost px-3 py-1.5 text-xs"
          disabled={!connector}
          onClick={sendDigest}
          data-testid="send-digest"
        >
          Send one now
        </button>
      </div>
      <p class={HELP}>
        What each kitty did, what it spent, and what waits for your yes. Leave it empty for no
        summary.
      </p>
      <label class="flex items-start gap-2 mt-3 text-sm cursor-pointer">
        <input
          type="checkbox"
          class="mt-1"
          checked={day}
          onChange={() => setDay(!day)}
          data-testid="notify-day"
        />
        <span>
          Start it with my day: what is on the calendar, the reminders still to do and the weather,
          from my connections (calendar, reminders, weather).
        </span>
      </label>
      <div class="flex items-center gap-2 mt-4 flex-wrap">
        <button class="btn-accent px-4 py-2 text-sm" onClick={save}>
          Save
        </button>
        <button class="btn-ghost px-4 py-2 text-sm" disabled={!connector} onClick={test}>
          Send a test
        </button>
        {msg && <span class="tag tag-green">{msg}</span>}
      </div>
      <Err text={err} />
    </div>
  );
}

// ─── The dialog ─────────────────────────────────────────────────────────────────────────────

type ConnectionEdit = { view: ConnectorView | null; kind: ConnectorKind };

export default function SettingsModal({
  onClose,
  initial,
  addConnector,
  editConnector,
  addModel,
  kitties = [],
  onKittiesChanged = () => {},
}: {
  onClose: () => void;
  initial?: Tab;
  addConnector?: boolean;
  editConnector?: string;
  addModel?: boolean;
  kitties?: Kitty[];
  onKittiesChanged?: () => void;
}) {
  const [tab, setTab] = useState<Tab>(initial || "models");
  const [view, setView] = useState<SettingsView | null>(null);
  const [err, setErr] = useState("");
  // A page that cannot draw a tab says why, instead of leaving it blank.
  const [crash] = useErrorBoundary();
  const [model, setModel] = useState<ProviderView | null | "new">(addModel ? "new" : null);
  const [conn, setConn] = useState<ConnectionEdit | null>(null);
  const [adding, setAdding] = useState(Boolean(addConnector));
  const [note, flash] = useFlash();
  // The route decides the tab and the form while the dialog stays open.
  useEffect(() => {
    setTab(initial || "models");
    setAdding(Boolean(addConnector));
    if (addModel) {
      setModel("new");
    }
  }, [initial, addConnector, addModel]);
  const load = () =>
    api
      .settings()
      .then((v) => {
        setView(v);
        return v;
      })
      .catch((e) => {
        setErr(errText(e));
        return null;
      });
  useEffect(() => {
    load().then((v) => {
      if (v && editConnector) {
        const c = v.connectors.find((x) => x.name === editConnector);
        const k = c && v.connector_catalog.find((x) => x.name === c.kind);
        if (c && k) {
          setConn({ view: c, kind: k });
        }
      }
    });
  }, [editConnector]);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    addEventListener("keydown", onKey);
    return () => removeEventListener("keydown", onKey);
  }, [onClose]);
  const kindOf = (name: string) => view?.connector_catalog.find((k) => k.name === name);
  const tabs: { id: Tab; label: string }[] = [
    { id: "models", label: "Models" },
    { id: "connectors", label: "Connections" },
    { id: "notifications", label: "Notifications" },
    { id: "gateway", label: inWails() ? "Gateway" : "Connection" },
  ];
  const modelNames = useMemo(() => (view?.providers || []).map((p) => p.name), [view]);
  const connNames = useMemo(() => (view?.connectors || []).map((c) => c.name), [view]);
  const setUpNtfy = () => {
    const k = kindOf("ntfy");
    setTab("connectors");
    if (k) {
      setConn({ view: null, kind: k });
    }
  };
  return (
    <div
      class="fixed inset-0 z-40 bg-black/60 flex items-center justify-center p-4 backdrop"
      onClick={onClose}
      data-modal
    >
      <div
        class="chat-modal w-full max-w-3xl p-5 max-h-[90vh] overflow-y-auto scroll-thin"
        onClick={(e) => e.stopPropagation()}
        role="dialog"
      >
        <div class="flex items-center justify-between mb-4">
          <h2 class="font-mono text-base tracking-wide">Settings</h2>
          <button
            class="plain p-1 text-[var(--t3)] hover:text-[var(--t1)]"
            onClick={onClose}
            aria-label="Close"
          >
            <IconX />
          </button>
        </div>
        <div class="flex gap-1 border-b border-white/5 mb-3 overflow-x-auto" role="tablist">
          {tabs.map((t) => (
            <button
              key={t.id}
              role="tab"
              aria-selected={tab === t.id}
              class={`plain px-4 py-2 text-sm border-b-2 whitespace-nowrap ${tab === t.id ? "border-[var(--color-accent)] text-[var(--t1)]" : "border-transparent text-[var(--t3)]"}`}
              onClick={() => setTab(t.id)}
            >
              {t.label}
            </button>
          ))}
        </div>
        <Err text={err} />
        {crash && (
          <p class="text-sm text-[var(--c-red)] mb-2" role="alert">
            Settings could not be drawn: {crash.message}. The gateway may be older than this app:
            update it and reopen.
          </p>
        )}
        {note && <div class="callout callout-ok mb-3">{note}</div>}
        {!view && !err && !crash && <p class="text-sm text-[var(--t4)]">Loading…</p>}

        {tab === "models" && view && (
          <div>
            <p class="text-sm text-[var(--t3)]">
              Models are the brains your kitties think with. Add one to start; you can add others
              later and give each kitty the one it should use.
            </p>
            <div class="flex flex-col gap-2 mt-3">
              {view.providers.map((p) => (
                <ModelRow
                  key={p.name}
                  p={p}
                  onEdit={() => setModel(p)}
                  onChanged={(s) => {
                    setView(s);
                    onKittiesChanged();
                  }}
                />
              ))}
              {view.providers.length === 0 && model === null && (
                <div class="callout">
                  <div class="font-semibold text-sm">No model yet</div>
                  <p class="text-xs text-[var(--t3)] mt-1">
                    Your kitties need a brain to think with. Add one from a provider you have an
                    account with, or Ollama to run free models on this computer.
                  </p>
                </div>
              )}
            </div>
            {model !== null ? (
              <ModelForm
                key={model === "new" ? "new" : model.name}
                view={model === "new" ? null : model}
                kinds={view.kinds}
                taken={modelNames}
                accounts={view.accounts || []}
                onAccounts={load}
                onSaved={(s, msg) => {
                  setView(s);
                  setModel(null);
                  flash(msg);
                  onKittiesChanged();
                }}
                onClose={() => setModel(null)}
              />
            ) : (
              <button
                class="btn-outline mt-3 flex items-center gap-2 px-3 py-2 text-sm"
                onClick={() => setModel("new")}
              >
                <IconPlus size={14} /> Add a model
              </button>
            )}
          </div>
        )}

        {tab === "connectors" && view && (
          <div>
            <p class="text-sm text-[var(--t3)]">
              Connections let your kitties act in other services: send messages, read email, open
              issues, post. You choose which kitties may use each one, and anything that reaches
              other people asks you first.
            </p>
            {conn ? (
              <ConnectionForm
                key={(conn.view?.name || "new") + conn.kind.name}
                view={conn.view}
                kind={conn.kind}
                catalog={view.connector_catalog}
                kitties={kitties}
                taken={connNames}
                motitaURL={view.motita_url}
                ready={view.oauth_ready || []}
                onSaved={(s, name) => {
                  setView(s);
                  setAdding(false);
                  const saved = s.connectors.find((c) => c.name === name);
                  // A service that signs in stays open on its button; the rest close.
                  if (saved?.needs_sign_in) {
                    setConn({ view: saved, kind: conn.kind });
                    flash("Saved: now sign in.");
                  } else {
                    setConn(null);
                    flash(`${conn.kind.label} is connected.`);
                  }
                }}
                onClose={() => setConn(null)}
                onChange={() => {
                  onKittiesChanged();
                  load();
                }}
              />
            ) : (
              <>
                <div class="section-title !mx-0 mt-4">Your connections</div>
                <div class="flex flex-col gap-2">
                  {view.connectors.map((c) => (
                    <ConnectionRow
                      key={c.name}
                      c={c}
                      onEdit={() => {
                        const k = kindOf(c.kind);
                        k && setConn({ view: c, kind: k });
                      }}
                      onChanged={(s) => {
                        setView(s);
                        onKittiesChanged();
                      }}
                    />
                  ))}
                  {view.connectors.length === 0 && (
                    <p class="text-sm text-[var(--t4)]">None yet: pick a service below.</p>
                  )}
                </div>
                {adding || view.connectors.length === 0 ? (
                  <>
                    <div class="section-title !mx-0 mt-5">Add a connection</div>
                    <ServiceGrid
                      catalog={view.connector_catalog}
                      categories={view.connector_categories}
                      onPick={(k) => setConn({ view: null, kind: k })}
                    />
                  </>
                ) : (
                  <button
                    class="btn-outline mt-3 flex items-center gap-2 px-3 py-2 text-sm"
                    onClick={() => setAdding(true)}
                  >
                    <IconPlus size={14} /> Add a connection
                  </button>
                )}
              </>
            )}
          </div>
        )}

        {tab === "notifications" && view && (
          <NotifyForm view={view} onSaved={setView} onSetUp={setUpNtfy} />
        )}
        {tab === "gateway" && <ConnectModal inline onClose={onClose} onDone={onClose} />}
      </div>
    </div>
  );
}
