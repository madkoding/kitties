import { useEffect, useState } from "preact/hooks";
import type { ConnectorKind, ConnectorView, ProviderKind, ProviderView, SettingsView } from "./api";
import { api } from "./api";
import { IconPlus, IconTrash, IconX } from "./icons";
import { ConnectModal } from "./Modals";

type Tab = "models" | "connectors" | "gateway";

const LABEL = "block text-xs text-[var(--t3)] mb-1 mt-3 uppercase tracking-wider font-mono";
const INPUT = "w-full px-3 py-2 text-sm";

function blankProvider(kinds: ProviderKind[]): ProviderView {
  const k = kinds[0];
  return { name: "", kind: k?.Name || "openai", model: k?.DefaultModel || "", api_key_set: false, default: false, describe: "", from_file: false };
}

// ProviderForm edits one AI profile. The key is write-only: the page never sees a saved one.
function ProviderForm({ view, kinds, onSaved, onClose }: { view: ProviderView; kinds: ProviderKind[]; onSaved: (s: SettingsView) => void; onClose: () => void }) {
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
          <input class={INPUT} value={name} disabled={Boolean(view.name)} onInput={(e) => setName((e.target as HTMLInputElement).value)} placeholder="strong" />
        </div>
        <div>
          <label class={LABEL}>Kind</label>
          <select class={INPUT} value={kind} onChange={(e) => pickKind((e.target as HTMLSelectElement).value)}>
            {kinds.map((k) => (
              <option key={k.Name} value={k.Name}>
                {k.Name}
              </option>
            ))}
          </select>
        </div>
        <div>
          <label class={LABEL}>Model</label>
          <input class={INPUT} value={model} onInput={(e) => setModel((e.target as HTMLInputElement).value)} placeholder={info?.DefaultModel} />
        </div>
        <div>
          <label class={LABEL}>API key {info?.NoKey ? "(optional)" : ""}</label>
          <input
            class={`${INPUT} font-mono`}
            type="password"
            value={key}
            onInput={(e) => setKey((e.target as HTMLInputElement).value)}
            placeholder={view.api_key_set ? "saved; type to replace" : info?.KeyEnv ? `or set ${info.KeyEnv}` : ""}
          />
        </div>
        <div>
          <label class={LABEL}>Base URL</label>
          <input class={INPUT} value={base} onInput={(e) => setBase((e.target as HTMLInputElement).value)} placeholder={info?.BaseURL || "https://…/v1"} />
        </div>
        <div>
          <label class={LABEL}>Max tokens</label>
          <input class={INPUT} value={maxTokens} onInput={(e) => setMaxTokens((e.target as HTMLInputElement).value)} placeholder="4096" />
        </div>
      </div>
      {err && <p class="text-sm text-[var(--c-red)] mt-2">{err}</p>}
      <div class="flex justify-end gap-2 mt-4">
        <button class="btn-ghost px-4 py-2 text-sm" onClick={onClose}>
          Cancel
        </button>
        <button class="btn-accent px-4 py-2 text-sm" disabled={busy || !name.trim() || !model.trim()} onClick={save}>
          {busy ? "Saving…" : "Save"}
        </button>
      </div>
    </div>
  );
}

function ProviderRow({ p, onEdit, onChanged }: { p: ProviderView; onEdit: () => void; onChanged: (s: SettingsView) => void }) {
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
        {test && <div class={`text-xs mt-0.5 ${test.startsWith("✓") ? "text-[var(--c-green)]" : "text-[var(--c-red)]"}`}>{test}</div>}
      </div>
      <div class="row-actions flex gap-1 flex-none">
        <button class="btn-ghost px-2 py-1 text-xs" disabled={busy || Boolean(p.error)} onClick={run}>
          {busy ? "…" : "test"}
        </button>
        {!p.default && !p.error && (
          <button class="btn-ghost px-2 py-1 text-xs" onClick={() => api.setDefaultProvider(p.name).then(onChanged)}>
            default
          </button>
        )}
        <button class="btn-ghost px-2 py-1 text-xs text-[var(--c-red)]" aria-label={`Remove ${p.name}`} onClick={() => confirm(`Remove the profile ${p.name}?`) && api.removeProvider(p.name).then(onChanged)}>
          <IconTrash size={12} />
        </button>
      </div>
    </div>
  );
}

function ConnectorForm({ view, catalog, categories, motitaURL, onSaved, onClose }: { view: ConnectorView; catalog: ConnectorKind[]; categories: string[]; motitaURL?: string; onSaved: (s: SettingsView) => void; onClose: () => void }) {
  const [name, setName] = useState(view.name);
  const [kind, setKind] = useState(view.kind || catalog[0]?.name || "");
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
  return (
    <div class="mt-3 p-4 border border-[rgb(var(--accent-rgb)/0.3)] bg-[rgb(var(--accent-rgb)/0.05)]">
      <div class="grid sm:grid-cols-2 gap-x-4">
        <div>
          <label class={LABEL}>Name</label>
          <input class={INPUT} value={name} disabled={Boolean(view.name)} onInput={(e) => setName((e.target as HTMLInputElement).value)} placeholder="team-slack" />
        </div>
        <div>
          <label class={LABEL}>Service</label>
          <select
            class={INPUT}
            value={kind}
            onChange={(e) => {
              setKind((e.target as HTMLSelectElement).value);
              setValues({});
            }}
          >
            {categories.map((c) => (
              <optgroup key={c} label={c}>
                {catalog
                  .filter((k) => k.category === c)
                  .map((k) => (
                    <option key={k.name} value={k.name}>
                      {k.label}
                    </option>
                  ))}
              </optgroup>
            ))}
          </select>
        </div>
        {def && (
          <div class="sm:col-span-2 text-xs text-[var(--t3)] mt-2">
            {def.help}.{" "}
            {def.actions.length > 0 && <span>Actions: {def.actions.map((a) => a.name + "(" + (a.args || []).join(", ") + ")").join("; ")}</span>}
          </div>
        )}
        <div class="sm:col-span-2">
          <label class={LABEL}>Description (what the kitties are told it does)</label>
          <input class={INPUT} value={description} onInput={(e) => setDescription((e.target as HTMLInputElement).value)} placeholder={def?.help} />
        </div>
        {def?.fields.map((f) => (
          <div key={f.name} class={f.name === "url" || f.name === "headers" ? "sm:col-span-2" : ""}>
            <label class={LABEL}>
              {f.label}
              {f.required ? "" : " (optional)"}
            </label>
            {f.name === "headers" ? (
              <textarea class={`${INPUT} font-mono`} value={values[f.name] || ""} onInput={(e) => set(f.name, (e.target as HTMLTextAreaElement).value)} placeholder={f.help} />
            ) : f.secret ? (
              <input class={`${INPUT} font-mono`} type="password" value={values[f.name] || ""} onInput={(e) => set(f.name, (e.target as HTMLInputElement).value)} placeholder={view.secrets_set?.includes(f.name) ? "saved; type to replace" : f.placeholder || f.help} />
            ) : (
              <input class={INPUT} type="text" value={values[f.name] || ""} onInput={(e) => set(f.name, (e.target as HTMLInputElement).value)} placeholder={f.name === "url" && kind === "motita" ? motitaURL || f.help : f.placeholder || f.help} />
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

const NEW_CONNECTOR: ConnectorView = { name: "", kind: "slack", category: "messaging", label: "Slack", token_set: false, settings: {}, secrets_set: [], from_file: false };

export default function SettingsModal({ onClose, initial, addConnector }: { onClose: () => void; initial?: Tab; addConnector?: boolean }) {
  const [tab, setTab] = useState<Tab>(initial || "models");
  const [view, setView] = useState<SettingsView | null>(null);
  const [err, setErr] = useState("");
  const [editing, setEditing] = useState<ProviderView | null>(null);
  const [editingC, setEditingC] = useState<ConnectorView | null>(addConnector ? NEW_CONNECTOR : null);
  useEffect(() => {
    api.settings().then(setView).catch((e) => setErr((e as Error).message));
  }, []);
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
    { id: "gateway", label: "Gateway" },
  ];
  return (
    <div class="fixed inset-0 z-40 bg-black/60 flex items-center justify-center p-4 backdrop" onClick={onClose} data-modal>
      <div class="chat-modal w-full max-w-3xl p-5 max-h-[90vh] overflow-y-auto scroll-thin" onClick={(e) => e.stopPropagation()} role="dialog">
        <div class="flex items-center justify-between mb-4">
          <h2 class="font-mono text-base tracking-wide">Settings</h2>
          <button class="plain p-1 text-[var(--t3)] hover:text-[var(--t1)]" onClick={onClose} aria-label="Close">
            <IconX />
          </button>
        </div>
        <div class="flex gap-1 border-b border-white/5 mb-3">
          {tabs.map((t) => (
            <button key={t.id} class={`plain px-4 py-2 text-sm border-b-2 ${tab === t.id ? "border-[var(--color-accent)] text-[var(--t1)]" : "border-transparent text-[var(--t3)]"}`} onClick={() => setTab(t.id)}>
              {t.label}
            </button>
          ))}
        </div>
        {err && <p class="text-sm text-[var(--c-red)] mb-2">{err}</p>}

        {tab === "models" && view && (
          <div>
            <p class="text-sm text-[var(--t3)]">The AI profiles a kitty can think with. Keys are kept on the gateway and never shown again; a profile from the YAML can be edited or removed here too.</p>
            <div class="flex flex-col gap-1 mt-3">
              {view.providers.map((p) => (
                <ProviderRow key={p.name} p={p} onEdit={() => setEditing(p)} onChanged={saved} />
              ))}
              {view.providers.length === 0 && <div class="text-sm text-[var(--t4)] px-2">No profile yet: add one to give the kitties a brain.</div>}
            </div>
            {editing ? (
              <ProviderForm key={editing.name || "new"} view={editing} kinds={view.kinds} onSaved={saved} onClose={() => setEditing(null)} />
            ) : (
              <button class="btn-outline mt-3 flex items-center gap-2 px-3 py-2 text-sm" onClick={() => setEditing(blankProvider(view.kinds))}>
                <IconPlus size={14} /> Add a model
              </button>
            )}
          </div>
        )}

        {tab === "connectors" && view && (
          <div>
            <p class="text-sm text-[var(--t3)]">
              External services the kitties may use. The catalogue covers {view.connector_catalog.length} services in {view.connector_categories.length} categories:{" "}
              {view.connector_categories.map((c) => (
                <span key={c} class="tag mr-1">
                  {c} · {view.connector_catalog.filter((k) => k.category === c).length}
                </span>
              ))}
            </p>
            <div class="flex flex-col gap-1 mt-3">
              {view.connectors.map((c) => (
                <div key={c.name} class="row !py-2.5">
                  <button class="row-hit" onClick={() => setEditingC(c)} aria-label={`Edit ${c.name}`} />
                  <span class="dot running" />
                  <div class="flex-1 min-w-0">
                    <div class="flex items-center gap-2 flex-wrap">
                      <span class="text-sm font-semibold">{c.name}</span>
                      <span class="tag tag-magenta">{c.label || c.kind}</span>
                      <span class="tag">{c.category}</span>
                      {c.from_file && <span class="tag">yaml</span>}
                      {c.secrets_set?.length > 0 && <span class="tag tag-green">secrets set</span>}
                    </div>
                    <div class="text-xs text-[var(--t3)] truncate">{c.description || c.url || c.command || c.kind}</div>
                  </div>
                  <div class="row-actions flex gap-1 flex-none">
                    <button class="btn-ghost px-2 py-1 text-xs text-[var(--c-red)]" aria-label={`Remove ${c.name}`} onClick={() => confirm(`Remove the connector ${c.name}?`) && api.removeConnector(c.name).then(saved)}>
                      <IconTrash size={12} />
                    </button>
                  </div>
                </div>
              ))}
              {view.connectors.length === 0 && <div class="text-sm text-[var(--t4)] px-2">No connector yet.</div>}
            </div>
            {editingC ? (
              <ConnectorForm key={editingC.name || "new"} view={editingC} catalog={view.connector_catalog} categories={view.connector_categories} motitaURL={view.motita_url} onSaved={saved} onClose={() => setEditingC(null)} />
            ) : (
              <button class="btn-outline mt-3 flex items-center gap-2 px-3 py-2 text-sm" onClick={() => setEditingC(NEW_CONNECTOR)}>
                <IconPlus size={14} /> Add a connector
              </button>
            )}
          </div>
        )}

        {tab === "gateway" && <ConnectModal inline onClose={onClose} onDone={onClose} />}
      </div>
    </div>
  );
}
