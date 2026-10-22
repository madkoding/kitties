import { useEffect, useState } from "preact/hooks";
import type { Kitty, ProviderInfo } from "./api";
import { api, inWails, saveConnection, savedConnection } from "./api";
import { Shot } from "./Conversation";
import { IconCamera, IconPlay, IconStop, IconTrash, IconX } from "./icons";
import { GoalsPanel, JournalPanel, LimitsForm } from "./Agenda";
import { MemoryPanel } from "./Memory";
import PixelCat, { BREEDS, CatPicker, COLORS, MOODS, type Mood } from "./PixelCat";

// suggestAvatar picks a cat from the name, the way the gateway does when none is chosen.
function suggestAvatar(name: string): { breed: string; color: string } {
  let h = 2166136261;
  for (const ch of name) {
    h = Math.imul(h ^ ch.charCodeAt(0), 16777619) >>> 0;
  }
  return { breed: BREEDS[h % BREEDS.length], color: COLORS[Math.floor(h / 7) % COLORS.length] };
}

function Modal({
  title,
  onClose,
  children,
  wide,
}: {
  title: string;
  onClose: () => void;
  children: preact.ComponentChildren;
  wide?: boolean;
}) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    addEventListener("keydown", onKey);
    return () => removeEventListener("keydown", onKey);
  }, [onClose]);
  return (
    <div
      class="fixed inset-0 z-40 bg-black/60 flex items-center justify-center p-4 backdrop"
      onClick={onClose}
      data-modal
    >
      <div
        class={`chat-modal w-full ${wide ? "max-w-3xl" : "max-w-md"} p-5 max-h-[90vh] overflow-y-auto scroll-thin`}
        onClick={(e) => e.stopPropagation()}
        role="dialog"
      >
        <div class="flex items-center justify-between mb-4">
          <h2 class="font-mono text-base tracking-wide">{title}</h2>
          <button
            class="plain p-1 text-[var(--t3)] hover:text-[var(--t1)]"
            onClick={onClose}
            aria-label="Close"
          >
            <IconX />
          </button>
        </div>
        {children}
      </div>
    </div>
  );
}

const LABEL = "block text-xs text-[var(--t3)] mb-1 mt-3 uppercase tracking-wider font-mono";
const INPUT = "w-full px-3 py-2 text-sm";

export function NewChatModal({
  kitties,
  onClose,
  onCreated,
}: {
  kitties: Kitty[];
  onClose: () => void;
  onCreated: (id: string) => void;
}) {
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [title, setTitle] = useState("");
  const [err, setErr] = useState("");
  const toggle = (id: string) => {
    const next = new Set(picked);
    next.has(id) ? next.delete(id) : next.add(id);
    setPicked(next);
  };
  const create = async () => {
    try {
      const s = await api.createChat(title.trim(), [...picked]);
      onCreated(s.id);
    } catch (e) {
      setErr((e as Error).message);
    }
  };
  return (
    <Modal title="New chat" onClose={onClose}>
      <p class="text-sm text-[var(--t3)]">
        Pick one kitty, or several for a group that works as a team.
      </p>
      <label class={LABEL}>Kitties</label>
      <div class="flex flex-col gap-1">
        {kitties.map((k) => (
          <label key={k.id} class={`row cursor-pointer ${picked.has(k.id) ? "on" : ""}`}>
            <input
              type="checkbox"
              class="row-hit opacity-0"
              checked={picked.has(k.id)}
              onChange={() => toggle(k.id)}
            />
            <span class={`dot ${k.status}`} />
            <span class="flex-1 text-sm">{k.name}</span>
            <span class="text-xs text-[var(--t4)] truncate max-w-[180px]">
              {k.soul?.persona || k.provider}
            </span>
          </label>
        ))}
      </div>
      <label class={LABEL}>Title (optional)</label>
      <input
        class={INPUT}
        value={title}
        onInput={(e) => setTitle((e.target as HTMLInputElement).value)}
        placeholder="Launch planning"
      />
      {err && <p class="text-sm text-[var(--c-red)] mt-2">{err}</p>}
      <div class="flex justify-end gap-2 mt-5">
        <button class="btn-ghost px-4 py-2 text-sm" onClick={onClose}>
          Cancel
        </button>
        <button class="btn-accent px-4 py-2 text-sm" disabled={picked.size === 0} onClick={create}>
          {picked.size > 1 ? "Start group chat" : "Start chat"}
        </button>
      </div>
    </Modal>
  );
}

export function NewKittyModal({
  onClose,
  onCreated,
}: {
  onClose: () => void;
  onCreated: () => void;
}) {
  const [providers, setProviders] = useState<ProviderInfo[]>([]);
  const [name, setName] = useState("");
  const [persona, setPersona] = useState("");
  const [tone, setTone] = useState("");
  const [provider, setProvider] = useState("");
  const [desktop, setDesktop] = useState(true);
  const [avatar, setAvatar] = useState<{ breed: string; color: string } | null>(null);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    api
      .providers()
      .then(setProviders)
      .catch(() => {});
  }, []);
  const look = avatar || suggestAvatar(name.trim() || "kitty");
  const create = async () => {
    setBusy(true);
    setErr("");
    try {
      const r = await api.createKitty({
        name: name.trim(),
        provider: provider || undefined,
        soul: { persona, tone },
        desktop,
        avatar: look,
      });
      if (r.warning) {
        setErr(r.warning);
        setBusy(false);
        return;
      }
      onCreated();
    } catch (e) {
      setErr((e as Error).message);
      setBusy(false);
    }
  };
  return (
    <Modal title="New kitty" onClose={onClose} wide>
      <div class="avatar-stage">
        <PixelCat breed={look.breed} color={look.color} mood="happy" size={96} />
      </div>
      <label class={LABEL}>Looks</label>
      <CatPicker breed={look.breed} color={look.color} onChange={setAvatar} />
      <label class={LABEL}>Name</label>
      <input
        class={INPUT}
        value={name}
        onInput={(e) => setName((e.target as HTMLInputElement).value)}
        placeholder="luna (lowercase, digits, dashes)"
      />
      <label class={LABEL}>Persona</label>
      <input
        class={INPUT}
        value={persona}
        onInput={(e) => setPersona((e.target as HTMLInputElement).value)}
        placeholder="a planner who keeps the team on track"
      />
      <label class={LABEL}>Tone</label>
      <input
        class={INPUT}
        value={tone}
        onInput={(e) => setTone((e.target as HTMLInputElement).value)}
        placeholder="calm and structured"
      />
      <label class={LABEL}>Brain</label>
      <select
        class={INPUT}
        value={provider}
        onChange={(e) => setProvider((e.target as HTMLSelectElement).value)}
      >
        <option value="">default provider</option>
        {providers.map((p) => (
          <option key={p.name} value={p.name}>
            {p.name} · {p.describe}
          </option>
        ))}
      </select>
      <label class="flex items-center gap-2 mt-4 text-sm cursor-pointer">
        <input type="checkbox" checked={desktop} onChange={() => setDesktop(!desktop)} />
        Give it a desktop (its own container with a display)
      </label>
      {err && <p class="text-sm text-[var(--c-amber)] mt-2">{err}</p>}
      <div class="flex justify-end gap-2 mt-5">
        <button class="btn-ghost px-4 py-2 text-sm" onClick={onClose}>
          Cancel
        </button>
        <button
          class="btn-accent px-4 py-2 text-sm"
          disabled={!name.trim() || busy}
          onClick={create}
        >
          {busy ? "Creating…" : "Create and start"}
        </button>
      </div>
    </Modal>
  );
}

export function KittyDrawer({
  kitty,
  kitties,
  onClose,
  onChanged,
  onChat,
  onOpenChat,
}: {
  kitty: Kitty;
  kitties: Kitty[];
  onClose: () => void;
  onChanged: () => void;
  onChat: () => void;
  onOpenChat: (id: string) => void;
}) {
  const [tick, setTick] = useState(0);
  const [err, setErr] = useState("");
  const [mood, setMood] = useState<Mood>("happy");
  const [looks, setLooks] = useState(false);
  const pick = async (a: { breed: string; color: string }) => {
    try {
      await api.editKitty(kitty.id, { avatar: a });
      onChanged();
    } catch (e) {
      setErr((e as Error).message);
    }
  };
  useEffect(() => {
    if (!kitty.desktop || kitty.status !== "running") {
      return;
    }
    const id = setInterval(() => setTick((t) => t + 1), 5000);
    return () => clearInterval(id);
  }, [kitty.desktop, kitty.status]);
  const act = async (fn: () => Promise<unknown>) => {
    setErr("");
    try {
      await fn();
      onChanged();
    } catch (e) {
      setErr((e as Error).message);
    }
  };
  const soul = kitty.soul || {};
  return (
    <Modal title={kitty.name} onClose={onClose} wide>
      <div class="grid md:grid-cols-[1.4fr_1fr] gap-5">
        <div class="min-w-0">
          <div class="avatar-stage mb-3">
            <PixelCat
              breed={kitty.avatar?.breed}
              color={kitty.avatar?.color}
              mood={mood}
              size={112}
            />
          </div>
          <div class="mood-row">
            {MOODS.map((m) => (
              <button
                key={m}
                class={`${m === mood ? "btn-outline" : "btn-ghost"} px-2 py-1`}
                onClick={() => setMood(m)}
              >
                {m}
              </button>
            ))}
            <button class="btn-ghost px-2 py-1" onClick={() => setLooks(!looks)}>
              {looks ? "done" : "change looks"}
            </button>
          </div>
          {looks && (
            <div class="mt-3">
              <CatPicker
                breed={kitty.avatar?.breed || "tabby"}
                color={kitty.avatar?.color || "orange"}
                onChange={pick}
              />
            </div>
          )}
          <div class="section-title !mx-0 mt-4">Desktop</div>
          {kitty.desktop ? (
            <Shot key={tick} path={`/v1/kitties/${kitty.id}/screenshot?t=${tick}`} big />
          ) : (
            <div class="live-shot flex items-center justify-center text-[var(--t4)] text-sm">
              brain only, no desktop
            </div>
          )}
          <div class="flex gap-2 mt-3 flex-wrap">
            <span
              class={`state-pill ${kitty.status === "running" ? "is-ok" : kitty.status === "error" ? "is-off" : ""}`}
            >
              {kitty.status}
            </span>
            <span class="tag tag-accent">{kitty.provider}</span>
            {kitty.desktop && <span class="tag">{kitty.container}</span>}
            {kitty.desktop && kitty.status === "running" && (
              <button
                class="btn-ghost px-2 py-1 text-xs flex items-center gap-1"
                onClick={() => setTick((t) => t + 1)}
              >
                <IconCamera size={12} /> refresh
              </button>
            )}
          </div>
          {kitty.error && <p class="text-sm text-[var(--c-red)] mt-2">{kitty.error}</p>}
          <GoalsPanel kitty={kitty} onOpenChat={onOpenChat} />
          <JournalPanel kitty={kitty} onOpenChat={onOpenChat} />
        </div>
        <div class="text-sm">
          <div class="section-title !mx-0">Soul</div>
          {soul.persona && (
            <p>
              <span class="text-[var(--t4)]">persona</span> {soul.persona}
            </p>
          )}
          {soul.tone && (
            <p class="mt-1">
              <span class="text-[var(--t4)]">tone</span> {soul.tone}
            </p>
          )}
          {soul.traits && soul.traits.length > 0 && (
            <div class="flex flex-wrap gap-1 mt-2">
              {soul.traits.map((t) => (
                <span key={t} class="tag">
                  {t}
                </span>
              ))}
            </div>
          )}
          {soul.skills && soul.skills.length > 0 && (
            <div class="flex flex-wrap gap-1 mt-2">
              {soul.skills.map((t) => (
                <span key={t} class="tag tag-magenta">
                  {t}
                </span>
              ))}
            </div>
          )}
          <div class="section-title !mx-0 mt-5">Actions</div>
          <div class="flex flex-col gap-2">
            <button class="btn-accent px-3 py-2 text-sm" onClick={onChat}>
              Chat with {kitty.name}
            </button>
            {kitty.status === "running" ? (
              <button
                class="btn-ghost px-3 py-2 text-sm flex items-center gap-2"
                onClick={() => act(() => api.stopKitty(kitty.id))}
              >
                <IconStop size={14} /> Stop
              </button>
            ) : (
              <button
                class="btn-ghost px-3 py-2 text-sm flex items-center gap-2"
                onClick={() => act(() => api.startKitty(kitty.id))}
              >
                <IconPlay size={14} /> Start
              </button>
            )}
            <button
              class="btn-danger px-3 py-2 text-sm flex items-center gap-2"
              onClick={() =>
                confirm(`Remove ${kitty.name} and its container?`) &&
                act(() => api.removeKitty(kitty.id).then(onClose))
              }
            >
              <IconTrash size={14} /> Remove
            </button>
          </div>
          {err && <p class="text-sm text-[var(--c-red)] mt-2">{err}</p>}
          <LimitsForm kitty={kitty} kitties={kitties} onChanged={onChanged} />
          <MemoryPanel kitty={kitty} />
        </div>
      </div>
    </Modal>
  );
}

export function ConnectModal({
  onClose,
  onDone,
  reason,
  inline: embedded,
}: {
  onClose: () => void;
  onDone: () => void;
  reason?: string;
  inline?: boolean;
}) {
  const saved = savedConnection();
  const [url, setUrl] = useState(saved.url || (inWails() ? "http://127.0.0.1:7488" : ""));
  const [token, setToken] = useState(saved.token);
  const [err, setErr] = useState(reason || "");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    const app = window.go?.main?.App;
    if (app) {
      app.Gateway().then((c) => {
        setUrl(c.url);
        setToken(c.token);
      });
    }
  }, []);
  const save = async () => {
    setBusy(true);
    setErr("");
    try {
      const app = window.go?.main?.App;
      if (app) {
        await app.Connect(url, token);
      } else {
        saveConnection({ url: url.trim().replace(/\/$/, ""), token: token.trim() });
        await api.kitties();
      }
      onDone();
    } catch (e) {
      setErr((e as Error).message);
      setBusy(false);
    }
  };
  const body = (
    <>
      <p class="text-sm text-[var(--t3)]">
        The address of the kitties gateway. On this machine the token is found on its own (the
        gateway leaves it in <code class="font-mono">~/.kitties/gateway.token</code>); a remote
        gateway needs the token it printed when it started.
        {!inWails() && " Leave the address empty when this page is served by the gateway itself."}
      </p>
      <label class={LABEL}>Address</label>
      <input
        class={INPUT}
        value={url}
        onInput={(e) => setUrl((e.target as HTMLInputElement).value)}
        placeholder="http://127.0.0.1:7488"
      />
      <label class={LABEL}>Token (remote gateways only)</label>
      <input
        class={`${INPUT} font-mono`}
        value={token}
        onInput={(e) => setToken((e.target as HTMLInputElement).value)}
        placeholder="found on this machine when empty"
      />
      {err && <p class="text-sm text-[var(--c-red)] mt-2">{err}</p>}
      <div class="flex justify-end gap-2 mt-5">
        <button class="btn-ghost px-4 py-2 text-sm" onClick={onClose}>
          Cancel
        </button>
        <button class="btn-accent px-4 py-2 text-sm" disabled={busy} onClick={save}>
          {busy ? "Checking…" : "Connect"}
        </button>
      </div>
    </>
  );
  if (embedded) {
    return body;
  }
  return (
    <Modal title="Gateway" onClose={onClose}>
      {body}
    </Modal>
  );
}
