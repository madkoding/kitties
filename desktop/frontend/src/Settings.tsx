import { useEffect, useState } from "preact/hooks";
import type { ConnectorKind, ConnectorView, ProviderKind, ProviderView, SettingsView } from "./api";
import { api } from "./api";
import { IconPlus, IconTrash, IconX } from "./icons";
import { ConnectModal } from "./Modals";
import ConnectorLogo, { logoColor } from "./ConnectorLogo";
import { openExternal, oauthStart } from "./api";

type Tab = "models" | "connectors" | "notifications" | "gateway";

const LABEL = "block text-xs text-[var(--t3)] mb-1 mt-3 uppercase tracking-wider font-mono";
const INPUT = "w-full px-3 py-2 text-sm";

function blankProvider(kinds: ProviderKind[]): ProviderView {
  const k = kinds[0];
  return {
    name: "",
    kind: k?.Name || "openai",
    model: k?.DefaultModel || "",
    api_key_set: false,
    default: false,
    describe: "",
    from_file: false,
  };
}

// ProviderForm edits one AI profile. The key is write-only: the page never sees a saved one.
function ProviderForm({
  view,
  kinds,
  onSaved,
  onClose,
}: {
  view: ProviderView;
  kinds: ProviderKind[];
  onSaved: (s: SettingsView) => void;
  onClose: () => void;
}) {
  const [name, setName] = useState(view.name);
  const [kind, setKind] = useState(view.kind);
  const [model, setModel] = useState(view.model);
  const [key, setKey] = useState("");
  const [base, setBase] = useState(view.base_url || "");
  const [maxTokens, setMaxTokens] = useState(view.max_tokens ? String(view.max_tokens) : "");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const info = kinds.find((k) => k.Name === kind);
  const pickKind = (k: string) => {
    setKind(k);
    const next = kinds.find((x) => x.Name === k);
    if (next && (!model || model === info?.DefaultModel)) {
      setModel(next.DefaultModel);
    }
  };
  const save = async () => {
    setBusy(true);
    setErr("");
    try {
      const body: Record<string, unknown> = { kind, model, base_url: base };
      if (key) {
        body.api_key = key;
      }
      if (maxTokens) {
        body.max_tokens = Number(maxTokens);
      }
      onSaved(await api.setProvider(name.trim(), body));
    } catch (e) {
      setErr((e as Error).message);
      setBusy(false);
    }
  };
  return (
    <div class="mt-3 p-4 border border-[rgb(var(--accent-rgb)/0.3)] bg-[rgb(var(--accent-rgb)/0.05)]">
      <div class="grid sm:grid-cols-2 gap-x-4">
        <div>
          <label class={LABEL}>Name</label>
          <input
            class={INPUT}
            value={name}
            disabled={Boolean(view.name)}
            onInput={(e) => setName((e.target as HTMLInputElement).value)}
            placeholder="strong"
          />
        </div>
        <div>
          <label class={LABEL}>Kind</label>
          <select
            class={INPUT}
            value={kind}
            onChange={(e) => pickKind((e.target as HTMLSelectElement).value)}
          >
            {kinds.map((k) => (
              <option key={k.Name} value={k.Name}>
                {k.Name}
              </option>
            ))}
          </select>
        </div>
        <div>
          <label class={LABEL}>Model</label>
          <input
            class={INPUT}
            value={model}
            onInput={(e) => setModel((e.target as HTMLInputElement).value)}
            placeholder={info?.DefaultModel}
          />
        </div>
        <div>
          <label class={LABEL}>API key {info?.NoKey ? "(optional)" : ""}</label>
          <input
            class={`${INPUT} font-mono`}
            type="password"
            value={key}
            onInput={(e) => setKey((e.target as HTMLInputElement).value)}
            placeholder={
              view.api_key_set
                ? "saved; type to replace"
                : info?.KeyEnv
                  ? `or set ${info.KeyEnv}`
                  : ""
            }
          />
        </div>
        <div>
          <label class={LABEL}>Base URL</label>
          <input
            class={INPUT}
            value={base}
            onInput={(e) => setBase((e.target as HTMLInputElement).value)}
            placeholder={info?.BaseURL || "https://…/v1"}
          />
        </div>
        <div>
          <label class={LABEL}>Max tokens</label>
          <input
            class={INPUT}
            value={maxTokens}
            onInput={(e) => setMaxTokens((e.target as HTMLInputElement).value)}
            placeholder="4096"
          />
        </div>
      </div>
      {err && <p class="text-sm text-[var(--c-red)] mt-2">{err}</p>}
      <div class="flex justify-end gap-2 mt-4">
        <button class="btn-ghost px-4 py-2 text-sm" onClick={onClose}>
          Cancel
        </button>
        <button
          class="btn-accent px-4 py-2 text-sm"
          disabled={busy || !name.trim() || !model.trim()}
          onClick={save}
        >
          {busy ? "Saving…" : "Save"}
        </button>
      </div>
    </div>
  );
}

function ProviderRow({
  p,
  onEdit,
  onChanged,
}: {
  p: ProviderView;
  onEdit: () => void;
  onChanged: (s: SettingsView) => void;
}) {
  const [test, setTest] = useState<string>("");
  const [busy, setBusy] = useState(false);
  const run = async () => {
    setBusy(true);
    setTest("");
    try {
      const r = await api.testProvider(p.name);
      setTest(r.ok ? "✓ " + (r.reply || "answered") : "✗ " + (r.error || "failed"));
    } catch (e) {
      setTest("✗ " + (e as Error).message);
    }
    setBusy(false);
  };
  return (
    <div class="row !py-2.5">
      <button class="row-hit" onClick={onEdit} aria-label={`Edit ${p.name}`} />
      <span class={`dot ${p.error ? "error" : "running"}`} />
      <div class="flex-1 min-w-0">
        <div class="flex items-center gap-2 flex-wrap">
          <span class="text-sm font-semibold">{p.name}</span>
          <span class="tag tag-accent">{p.kind}</span>
          <span class="text-xs font-mono text-[var(--t3)] truncate">{p.model}</span>
          {p.default && <span class="tag tag-green">default</span>}
          {p.from_file && <span class="tag">yaml</span>}
          {!p.api_key_set && !p.error && <span class="tag tag-amber">env key</span>}
        </div>
        {p.error && <div class="text-xs text-[var(--c-red)] mt-0.5">{p.error}</div>}
        {test && (
          <div
            class={`text-xs mt-0.5 ${test.startsWith("✓") ? "text-[var(--c-green)]" : "text-[var(--c-red)]"}`}
          >
            {test}
          </div>
        )}
      </div>
      <div class="row-actions flex gap-1 flex-none">
        <button
          class="btn-ghost px-2 py-1 text-xs"
          disabled={busy || Boolean(p.error)}
          onClick={run}
        >
          {busy ? "…" : "test"}
        </button>
        {!p.default && !p.error && (
          <button
            class="btn-ghost px-2 py-1 text-xs"
            onClick={() => api.setDefaultProvider(p.name).then(onChanged)}
          >
            default
          </button>
        )}
        <button
          class="btn-ghost px-2 py-1 text-xs text-[var(--c-red)]"
          aria-label={`Remove ${p.name}`}
          onClick={() =>
            confirm(`Remove the profile ${p.name}?`) && api.removeProvider(p.name).then(onChanged)
          }
        >
          <IconTrash size={12} />
        </button>
      </div>
    </div>
  );
}

// ServiceGrid is the catalogue as buttons with their logos, grouped by category.
function ServiceGrid({
  catalog,
  categories,
  picked,
  onPick,
}: {
  catalog: ConnectorKind[];
  categories: string[];
  picked: string;
  onPick: (k: ConnectorKind) => void;
}) {
  return (
    <div class="service-grid">
      {categories.map((c) => (
        <div key={c}>
          <div class="section-title !mx-0">{c}</div>
          <div class="flex flex-wrap gap-2">
            {catalog
              .filter((k) => k.category === c)
              .map((k) => (
                <button
                  key={k.name}
                  type="button"
                  class={`plain service-btn ${picked === k.name ? "on" : ""}`}
                  style={`--brand:${logoColor(k.name)}`}
                  onClick={() => onPick(k)}
                  title={k.help}
                >
                  <ConnectorLogo kind={k.name} size={26} />
                  <span>{k.label}</span>
                  {k.oauth && <span class="service-oauth">OAuth</span>}
                </button>
              ))}
          </div>
        </div>
      ))}
    </div>
  );
}

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
function NotifyForm({ view, onSaved }: { view: SettingsView; onSaved: (s: SettingsView) => void }) {
  const channels = view.connectors.filter((c) => c.category === "messaging");
  const [connector, setConnector] = useState(view.notify?.connector || "");
  const [on, setOn] = useState<string[]>(
    view.notify?.on?.length ? view.notify.on : ["help", "approval"],
  );
  const [digest, setDigest] = useState(view.notify?.digest || "");
  const [msg, setMsg] = useState("");
  const [err, setErr] = useState("");
  const toggle = (e: string) => setOn(on.includes(e) ? on.filter((x) => x !== e) : [...on, e]);
  const save = async () => {
    setErr("");
    setMsg("");
    try {
      onSaved(await api.setNotify({ connector, on, digest }));
      setMsg("Saved.");
    } catch (e) {
      setErr((e as Error).message);
    }
  };
  const test = async () => {
    setErr("");
    setMsg("");
    try {
      await api.setNotify({ connector, on, digest });
      const r = await api.testNotify(connector);
      if (r.ok) {
        setMsg("Sent: look at your phone or channel.");
      } else {
        setErr(r.error || "The test did not go through.");
      }
    } catch (e) {
      setErr((e as Error).message);
    }
  };
  const sendDigest = async () => {
    setErr("");
    setMsg("");
    try {
      await api.setNotify({ connector, on, digest });
      const r = await api.sendDigest();
      if (r.ok) {
        setMsg("Summary sent.");
      } else {
        setErr(r.error || "The summary did not go through.");
      }
    } catch (e) {
      setErr((e as Error).message);
    }
  };
  return (
    <div data-testid="notify">
      <p class="text-sm text-[var(--t3)]">
        The kitties work while you are away. Choose how they reach you outside this page: a push to
        your phone with ntfy is the quickest, and Telegram, Slack or mail work too. They write
        through a connector you already set up.
      </p>
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
            {c.name} ({c.label})
          </option>
        ))}
      </select>
      {channels.length === 0 && (
        <p class="text-xs text-[var(--t4)] mt-1">
          There is no messaging connector yet: add ntfy, Telegram, Slack or email under Connectors
          first.
        </p>
      )}
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
      <div class="flex items-center gap-2">
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
      <p class="text-xs text-[var(--t4)] mt-1">
        What each kitty did, what it spent, and what waits for your yes. The time is the gateway's
        clock. Leave it empty for no summary.
      </p>
      <div class="flex items-center gap-2 mt-4">
        <button class="btn-accent px-4 py-2 text-sm" onClick={save}>
          Save
        </button>
        <button class="btn-ghost px-4 py-2 text-sm" disabled={!connector} onClick={test}>
          Send a test
        </button>
        {msg && <span class="tag tag-green">{msg}</span>}
      </div>
      {err && <p class="text-sm text-[var(--c-red)] mt-2">{err}</p>}
    </div>
  );
}

// OAUTH_LABEL names the identity services a connector can sign in with.
const OAUTH_LABEL: Record<string, string> = {
  google: "Google",
  microsoft: "Microsoft",
  slack: "Slack",
  github: "GitHub",
};

// OAuthButton signs a connector in: it asks the gateway for the consent URL and opens it.
function OAuthButton({ view, onDone }: { view: ConnectorView; onDone: () => void }) {
  const [msg, setMsg] = useState("");
  const go = async () => {
    try {
      const url = await oauthStart(view.name);
      openExternal(url);
      setMsg("A browser window opened: sign in there, then come back.");
      const id = setInterval(onDone, 3000);
      setTimeout(() => clearInterval(id), 120000);
    } catch (e) {
      setMsg((e as Error).message);
    }
  };
  return (
    <div class="mt-1 mb-3 flex items-center gap-3 flex-wrap">
      <button
        type="button"
        class={`${view.signed_in ? "btn-ghost" : "btn-accent"} px-4 py-2 text-sm flex items-center gap-2`}
        onClick={go}
      >
        <ConnectorLogo kind={view.kind} size={16} mono />
        {view.signed_in
          ? "Sign in again"
          : `Sign in with ${OAUTH_LABEL[view.oauth || ""] || view.oauth}`}
      </button>
      {view.signed_in && (
        <span class="tag tag-green">signed in{view.account ? " · " + view.account : ""}</span>
      )}
      {msg && <span class="text-xs text-[var(--t3)]">{msg}</span>}
    </div>
  );
}

function ConnectorForm({
  view,
  catalog,
  categories,
  motitaURL,
  onSaved,
  onClose,
}: {
  view: ConnectorView;
  catalog: ConnectorKind[];
  categories: string[];
  motitaURL?: string;
  onSaved: (s: SettingsView) => void;
  onClose: () => void;
}) {
  const [name, setName] = useState(view.name);
  const [kind, setKind] = useState(view.kind || "");
  const [description, setDescription] = useState(view.description || "");
  const [values, setValues] = useState<Record<string, string>>({ ...view.settings });
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const def = catalog.find((k) => k.name === kind);
  const set = (k: string, v: string) => setValues({ ...values, [k]: v });
  const save = async () => {
    setBusy(true);
    setErr("");
    try {
      const settings: Record<string, string> = {};
      for (const [k, v] of Object.entries(values)) {
        if (v.trim()) {
          settings[k] = v;
        }
      }
      onSaved(await api.setConnector(name.trim(), { kind, description, settings }));
    } catch (e) {
      setErr((e as Error).message);
      setBusy(false);
    }
  };
  if (!kind) {
    return (
      <div class="mt-3 p-4 border border-[rgb(var(--accent-rgb)/0.3)] bg-[rgb(var(--accent-rgb)/0.05)]">
        <p class="text-sm text-[var(--t3)]">Pick the service to connect.</p>
        <ServiceGrid
          catalog={catalog}
          categories={categories}
          picked=""
          onPick={(k) => {
            setKind(k.name);
            setValues({});
            if (!name) {
              setName(k.name);
            }
          }}
        />
        <div class="flex justify-end gap-2 mt-4">
          <button class="btn-ghost px-4 py-2 text-sm" onClick={onClose}>
            Cancel
          </button>
        </div>
      </div>
    );
  }
  return (
    <div class="mt-3 p-4 border border-[rgb(var(--accent-rgb)/0.3)] bg-[rgb(var(--accent-rgb)/0.05)]">
      <div class="flex items-center gap-3 mb-1">
        <span class="service-badge" style={`--brand:${logoColor(kind)}`}>
          <ConnectorLogo kind={kind} size={28} />
        </span>
        <div class="flex-1 min-w-0">
          <div class="font-semibold">{def?.label || kind}</div>
          <div class="text-xs text-[var(--t3)]">{def?.help}</div>
        </div>
        {!view.name && (
          <button type="button" class="btn-ghost px-3 py-1.5 text-xs" onClick={() => setKind("")}>
            change service
          </button>
        )}
      </div>
      {def?.oauth && view.name && (
        <OAuthButton
          view={view}
          onDone={() =>
            api
              .settings()
              .then(onSaved)
              .catch(() => {})
          }
        />
      )}
      {def?.oauth && !view.name && (
        <p class="text-xs text-[var(--t3)] mb-2">
          {def.fields.some((f) => f.name === "token")
            ? "Paste a token below, or save first and sign in with the button that appears here."
            : "Save first, then sign in with the button that appears here."}
        </p>
      )}
      <div class="grid sm:grid-cols-2 gap-x-4">
        <div class="sm:col-span-2">
          <label class={LABEL}>Name</label>
          <input
            class={INPUT}
            value={name}
            disabled={Boolean(view.name)}
            onInput={(e) => setName((e.target as HTMLInputElement).value)}
            placeholder="team-slack"
          />
        </div>
        {def && def.actions.length > 0 && (
          <div class="sm:col-span-2 text-xs text-[var(--t3)] mt-2">
            Actions:{" "}
            {def.actions
              .map(
                (a) =>
                  a.name +
                  "(" +
                  (a.args || []).join(", ") +
                  ")" +
                  (a.risk === "always"
                    ? " [always asks you]"
                    : a.risk === "ask"
                      ? " [asks you first]"
                      : ""),
              )
              .join("; ")}
          </div>
        )}
        <div class="sm:col-span-2">
          <label class={LABEL}>Description (what the kitties are told it does)</label>
          <input
            class={INPUT}
            value={description}
            onInput={(e) => setDescription((e.target as HTMLInputElement).value)}
            placeholder={def?.help}
          />
        </div>
        {def?.fields.map((f) => (
          <div key={f.name} class={f.name === "url" || f.name === "headers" ? "sm:col-span-2" : ""}>
            <label class={LABEL}>
              {f.label}
              {f.required ? "" : " (optional)"}
            </label>
            {f.name === "headers" ? (
              <textarea
                class={`${INPUT} font-mono`}
                value={values[f.name] || ""}
                onInput={(e) => set(f.name, (e.target as HTMLTextAreaElement).value)}
                placeholder={f.help}
              />
            ) : f.secret ? (
              <input
                class={`${INPUT} font-mono`}
                type="password"
                value={values[f.name] || ""}
                onInput={(e) => set(f.name, (e.target as HTMLInputElement).value)}
                placeholder={
                  view.secrets_set?.includes(f.name)
                    ? "saved; type to replace"
                    : f.placeholder || f.help
                }
              />
            ) : (
              <input
                class={INPUT}
                type="text"
                value={values[f.name] || ""}
                onInput={(e) => set(f.name, (e.target as HTMLInputElement).value)}
                placeholder={
                  f.name === "url" && kind === "motita"
                    ? motitaURL || f.help
                    : f.placeholder || f.help
                }
              />
            )}
          </div>
        ))}
      </div>
      {err && <p class="text-sm text-[var(--c-red)] mt-2">{err}</p>}
      <div class="flex justify-end gap-2 mt-4">
        <button class="btn-ghost px-4 py-2 text-sm" onClick={onClose}>
          Cancel
        </button>
        <button class="btn-accent px-4 py-2 text-sm" disabled={busy || !name.trim()} onClick={save}>
          {busy ? "Saving…" : "Save"}
        </button>
      </div>
    </div>
  );
}

const NEW_CONNECTOR: ConnectorView = {
  name: "",
  kind: "",
  category: "",
  label: "",
  token_set: false,
  settings: {},
  secrets_set: [],
  from_file: false,
};

export default function SettingsModal({
  onClose,
  initial,
  addConnector,
  editConnector,
}: {
  onClose: () => void;
  initial?: Tab;
  addConnector?: boolean;
  editConnector?: string;
}) {
  const [tab, setTab] = useState<Tab>(initial || "models");
  const [view, setView] = useState<SettingsView | null>(null);
  const [err, setErr] = useState("");
  const [editing, setEditing] = useState<ProviderView | null>(null);
  const [editingC, setEditingC] = useState<ConnectorView | null>(
    addConnector ? NEW_CONNECTOR : null,
  );
  // The route decides the tab and the form while the modal stays open (#new-settings to
  // #new-connectors-add is one page, not a reload).
  useEffect(() => {
    setTab(initial || "models");
    setEditingC(addConnector ? NEW_CONNECTOR : null);
  }, [initial, addConnector]);
  useEffect(() => {
    api
      .settings()
      .then((v) => {
        setView(v);
        if (editConnector) {
          const c = v.connectors.find((x) => x.name === editConnector);
          if (c) {
            setEditingC(c);
          }
        }
      })
      .catch((e) => setErr((e as Error).message));
  }, [editConnector]);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    addEventListener("keydown", onKey);
    return () => removeEventListener("keydown", onKey);
  }, [onClose]);
  const saved = (s: SettingsView) => {
    setView(s);
    setEditing(null);
    setEditingC(null);
  };
  const tabs: { id: Tab; label: string }[] = [
    { id: "models", label: "Models" },
    { id: "connectors", label: "Connectors" },
    { id: "notifications", label: "Notifications" },
    { id: "gateway", label: "Gateway" },
  ];
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
        <div class="flex gap-1 border-b border-white/5 mb-3">
          {tabs.map((t) => (
            <button
              key={t.id}
              class={`plain px-4 py-2 text-sm border-b-2 ${tab === t.id ? "border-[var(--color-accent)] text-[var(--t1)]" : "border-transparent text-[var(--t3)]"}`}
              onClick={() => setTab(t.id)}
            >
              {t.label}
            </button>
          ))}
        </div>
        {err && <p class="text-sm text-[var(--c-red)] mb-2">{err}</p>}

        {tab === "models" && view && (
          <div>
            <p class="text-sm text-[var(--t3)]">
              The AI profiles a kitty can think with. Keys are kept on the gateway and never shown
              again; a profile from the YAML can be edited or removed here too.
            </p>
            <div class="flex flex-col gap-1 mt-3">
              {view.providers.map((p) => (
                <ProviderRow key={p.name} p={p} onEdit={() => setEditing(p)} onChanged={saved} />
              ))}
              {view.providers.length === 0 && (
                <div class="text-sm text-[var(--t4)] px-2">
                  No profile yet: add one to give the kitties a brain.
                </div>
              )}
            </div>
            {editing ? (
              <ProviderForm
                key={editing.name || "new"}
                view={editing}
                kinds={view.kinds}
                onSaved={saved}
                onClose={() => setEditing(null)}
              />
            ) : (
              <button
                class="btn-outline mt-3 flex items-center gap-2 px-3 py-2 text-sm"
                onClick={() => setEditing(blankProvider(view.kinds))}
              >
                <IconPlus size={14} /> Add a model
              </button>
            )}
          </div>
        )}

        {tab === "connectors" && view && (
          <div>
            <p class="text-sm text-[var(--t3)]">
              External services the kitties may use: {view.connector_catalog.length} services in{" "}
              {view.connector_categories.length} categories. Pick one below to connect it.
            </p>
            {!editingC && (
              <ServiceGrid
                catalog={view.connector_catalog}
                categories={view.connector_categories}
                picked=""
                onPick={(k) =>
                  setEditingC({
                    ...NEW_CONNECTOR,
                    kind: k.name,
                    category: k.category,
                    label: k.label,
                    name: view.connectors.some((c) => c.name === k.name) ? "" : k.name,
                  })
                }
              />
            )}
            <div class="section-title !mx-0 mt-4">Connected</div>
            <div class="flex flex-col gap-1 mt-3">
              {view.connectors.map((c) => (
                <div key={c.name} class="row !py-2.5">
                  <button
                    class="row-hit"
                    onClick={() => setEditingC(c)}
                    aria-label={`Edit ${c.name}`}
                  />
                  <span class="service-badge" style={`--brand:${logoColor(c.kind)}`}>
                    <ConnectorLogo kind={c.kind} size={22} />
                  </span>
                  <div class="flex-1 min-w-0">
                    <div class="flex items-center gap-2 flex-wrap">
                      <span class="text-sm font-semibold">{c.name}</span>
                      <span class="tag tag-magenta">{c.label || c.kind}</span>
                      <span class="tag">{c.category}</span>
                      {c.from_file && <span class="tag">yaml</span>}
                      {c.secrets_set?.length > 0 && <span class="tag tag-green">secrets set</span>}
                      {c.signed_in && (
                        <span class="tag tag-green">
                          signed in{c.account ? " · " + c.account : ""}
                        </span>
                      )}
                      {c.needs_sign_in && <span class="tag tag-amber">sign in needed</span>}
                    </div>
                    <div class="text-xs text-[var(--t3)] truncate">
                      {c.description || c.url || c.command || c.kind}
                    </div>
                  </div>
                  <div class="row-actions flex gap-1 flex-none">
                    <button
                      class="btn-ghost px-2 py-1 text-xs text-[var(--c-red)]"
                      aria-label={`Remove ${c.name}`}
                      onClick={() =>
                        confirm(`Remove the connector ${c.name}?`) &&
                        api.removeConnector(c.name).then(saved)
                      }
                    >
                      <IconTrash size={12} />
                    </button>
                  </div>
                </div>
              ))}
              {view.connectors.length === 0 && (
                <div class="text-sm text-[var(--t4)] px-2">No connector yet.</div>
              )}
            </div>
            {editingC ? (
              <ConnectorForm
                key={editingC.name || "new"}
                view={editingC}
                catalog={view.connector_catalog}
                categories={view.connector_categories}
                motitaURL={view.motita_url}
                onSaved={saved}
                onClose={() => setEditingC(null)}
              />
            ) : (
              <button
                class="btn-outline mt-3 flex items-center gap-2 px-3 py-2 text-sm"
                onClick={() => setEditingC(NEW_CONNECTOR)}
              >
                <IconPlus size={14} /> Add a connector
              </button>
            )}
          </div>
        )}

        {tab === "notifications" && view && <NotifyForm view={view} onSaved={saved} />}
        {tab === "gateway" && <ConnectModal inline onClose={onClose} onDone={onClose} />}
      </div>
    </div>
  );
}
