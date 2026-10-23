import { useEffect, useLayoutEffect, useState } from "preact/hooks";
import type { Kitty, ProviderInfo } from "./api";
import { api, errText, inAndroid, inWails, saveConnection, savedConnection } from "./api";
import { ask } from "./Confirm";
import { NONE, mayUse, providerLabel, usesAll, withConnection } from "./friendly";
import { ProviderLogo } from "./ConnectorLogo";
import { Shot } from "./Conversation";
import { IconCamera, IconPlay, IconStop, IconTrash, IconX } from "./icons";
import { GoalsPanel, JournalPanel, LimitsForm } from "./Agenda";
import { MemoryPanel } from "./Memory";
import PixelCat, { BREEDS, CatPicker, COLORS, type Mood } from "./PixelCat";
import { Missing, Required } from "./Required";
import { t } from "./i18n";

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
  useLayoutEffect(() => {
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
            aria-label={t("Close")}
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
  onNeedKitty,
}: {
  kitties: Kitty[];
  onClose: () => void;
  onCreated: (id: string) => void;
  onNeedKitty?: () => void;
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
  if (kitties.length === 0) {
    return (
      <Modal title={t("New chat")} onClose={onClose}>
        <p class="text-sm text-[var(--t3)]" data-testid="chat-needs-kitty">
          {t(
            "A chat is a conversation with your kitties, and there is none yet. Create a kitty first, then chat with it.",
          )}
        </p>
        <div class="flex justify-end gap-2 mt-5">
          <button class="btn-ghost px-4 py-2 text-sm" onClick={onClose}>
            {t("Cancel")}
          </button>
          <button class="btn-accent px-4 py-2 text-sm" onClick={onNeedKitty}>
            {t("Create a kitty")}
          </button>
        </div>
      </Modal>
    );
  }
  return (
    <Modal title={t("New chat")} onClose={onClose}>
      <p class="text-sm text-[var(--t3)]">
        {t("Pick one kitty, or several for a group that works as a team.")}
      </p>
      <label class={LABEL}>
        {t("Kitties")}
        <Required />
      </label>
      <div class="flex flex-col gap-1" role="group" aria-label={t("Kitties")} aria-required="true">
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
            <span class="text-xs text-[var(--t4)] truncate max-w-[220px]">
              {k.soul?.persona || ""}
            </span>
          </label>
        ))}
      </div>
      <label class={LABEL}>{t("Title (optional)")}</label>
      <input
        class={INPUT}
        value={title}
        onInput={(e) => setTitle((e.target as HTMLInputElement).value)}
        placeholder={t("Launch planning")}
      />
      {err && <p class="text-sm text-[var(--c-red)] mt-2">{err}</p>}
      <div class="flex justify-end gap-2 mt-5">
        <button class="btn-ghost px-4 py-2 text-sm" onClick={onClose}>
          {t("Cancel")}
        </button>
        <button class="btn-accent px-4 py-2 text-sm" disabled={picked.size === 0} onClick={create}>
          {picked.size > 1 ? t("Start group chat") : t("Start chat")}
        </button>
      </div>
      <Missing items={[picked.size === 0 && t("a kitty")]} />
    </Modal>
  );
}

// slugName turns what a person types into a kitty name: lowercase letters, digits and dashes.
function slugName(raw: string): string {
  return raw
    .normalize("NFD")
    .replace(/[\u0300-\u036f]/g, "")
    .toLowerCase()
    .replace(/[^a-z0-9-]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 40);
}

// modelLabel says a model as a person reads it: "OpenAI · gpt-5.5".
const modelLabel = (p: ProviderInfo) => `${providerLabel(p.kind)} · ${p.model}`;

export function NewKittyModal({
  onClose,
  onCreated,
  onNeedModel,
  containers = true,
  taken = [],
}: {
  onClose: () => void;
  // onCreated gets what the person should know about the new kitty (a desktop that could not
  // start), if anything: the kitty exists either way.
  onCreated: (note?: string) => void;
  onNeedModel?: () => void;
  containers?: boolean;
  // taken are the names of the kitties there are already.
  taken?: string[];
}) {
  const [providers, setProviders] = useState<ProviderInfo[] | null>(null);
  const [connections, setConnections] = useState<string[]>([]);
  const [raw, setRaw] = useState("");
  const [persona, setPersona] = useState("");
  const [tone, setTone] = useState("");
  const [provider, setProvider] = useState("");
  const [desktop, setDesktop] = useState(containers);
  const [useAll, setUseAll] = useState(true);
  const [avatar, setAvatar] = useState<{ breed: string; color: string } | null>(null);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    api
      .providers()
      .then(setProviders)
      .catch(() => setProviders([]));
    api
      .connectorNames()
      .then(setConnections)
      .catch(() => {});
  }, []);
  const name = slugName(raw);
  const clash = Boolean(name) && taken.includes(name);
  const look = avatar || suggestAvatar(name || "kitty");
  if (providers !== null && providers.length === 0) {
    return (
      <Modal title={t("New kitty")} onClose={onClose}>
        <div class="avatar-stage">
          <PixelCat breed={look.breed} color={look.color} mood="sleeping" size={80} />
        </div>
        <p class="text-sm text-[var(--t3)] mt-3" data-testid="kitty-needs-model">
          {t(
            "A kitty needs a brain to think with, and there is no model yet. Add one in Settings (it takes a minute: pick a provider and paste its key), then come back.",
          )}
        </p>
        <div class="flex justify-end gap-2 mt-5">
          <button class="btn-ghost px-4 py-2 text-sm" onClick={onClose}>
            {t("Cancel")}
          </button>
          <button class="btn-accent px-4 py-2 text-sm" onClick={onNeedModel}>
            {t("Add a model")}
          </button>
        </div>
      </Modal>
    );
  }
  const create = async () => {
    setBusy(true);
    setErr("");
    try {
      const r = await api.createKitty({
        name,
        provider: provider || undefined,
        soul: { persona: persona.trim(), tone: tone.trim() },
        desktop: desktop && containers,
        avatar: look,
        connectors: useAll ? [] : [NONE],
      });
      onCreated(
        r.warning
          ? t("{name} was created, but {why}", {
              name,
              why: r.warning.replace(/^created but /, ""),
            })
          : undefined,
      );
    } catch (e) {
      setErr(errText(e));
      setBusy(false);
    }
  };
  return (
    <Modal title={t("New kitty")} onClose={onClose} wide>
      <div class="avatar-stage">
        <PixelCat breed={look.breed} color={look.color} mood="happy" size={96} />
      </div>
      <label class={LABEL} for="kitty-name">
        {t("Name")}
        <Required />
      </label>
      <input
        id="kitty-name"
        class={INPUT}
        aria-required="true"
        aria-invalid={clash}
        onKeyDown={(e) => {
          // Enter creates it, like the button, once there is a name no other kitty has.
          if (e.key === "Enter" && name && !clash && !busy) {
            e.preventDefault();
            create();
          }
        }}
        aria-describedby={clash ? "kitty-name-taken" : undefined}
        value={raw}
        onInput={(e) => setRaw((e.target as HTMLInputElement).value)}
        placeholder="Luna"
        autofocus
      />
      {clash ? (
        <p id="kitty-name-taken" class="text-xs text-[var(--c-red)] mt-1" role="alert">
          {t("There is already a kitty called ")}
          <b>{name}</b>
          {t(": pick another name.")}
        </p>
      ) : (
        raw &&
        name !== raw && (
          <p class="text-xs text-[var(--t4)] mt-1">
            {t("It will be called ")}
            <b>{name || "…"}</b>
            {t(" (lowercase letters, digits and dashes).")}
          </p>
        )
      )}
      <label class={LABEL} for="kitty-persona">
        {t("Who is it?")}
      </label>
      <input
        id="kitty-persona"
        class={INPUT}
        value={persona}
        onInput={(e) => setPersona((e.target as HTMLInputElement).value)}
        placeholder={t("a planner who keeps the team on track")}
      />
      <p class="text-xs text-[var(--t4)] mt-1">
        {t("A line about its job and character. Empty makes a helpful all-rounder.")}
      </p>
      <label class={LABEL} for="kitty-tone">
        {t("How does it talk?")}
      </label>
      <input
        id="kitty-tone"
        class={INPUT}
        value={tone}
        onInput={(e) => setTone((e.target as HTMLInputElement).value)}
        placeholder={t("calm and structured")}
      />
      <label class={LABEL}>{t("Looks")}</label>
      <CatPicker breed={look.breed} color={look.color} onChange={setAvatar} />
      <label class={LABEL} for="kitty-brain">
        {t("Brain")}
      </label>
      <select
        id="kitty-brain"
        class={INPUT}
        value={provider}
        onChange={(e) => setProvider((e.target as HTMLSelectElement).value)}
      >
        <option value="">{t("the default model")}</option>
        {(providers || []).map((p) => (
          <option key={p.name} value={p.name}>
            {modelLabel(p)}
          </option>
        ))}
      </select>
      {connections.length > 0 && (
        <label class="flex items-center gap-2 mt-4 text-sm cursor-pointer">
          <input type="checkbox" checked={useAll} onChange={() => setUseAll(!useAll)} />
          {t("Let it use my connections ({list}, and the ones I add later)", {
            list: connections.join(", "),
          })}
        </label>
      )}
      <label
        class={`flex items-center gap-2 mt-3 text-sm ${containers ? "cursor-pointer" : "opacity-60"}`}
      >
        <input
          type="checkbox"
          checked={desktop && containers}
          disabled={!containers}
          onChange={() => setDesktop(!desktop)}
        />
        {t("Give it a computer of its own (a desktop with a browser it can use)")}
      </label>
      {!containers && (
        <p class="text-xs text-[var(--t4)] mt-1">
          {t(
            "Needs Docker running on the gateway's machine. Without it the kitty still thinks, talks and uses connections.",
          )}
        </p>
      )}
      {err && <p class="text-sm text-[var(--c-red)] mt-2">{err}</p>}
      <div class="flex justify-end gap-2 mt-5">
        <button class="btn-ghost px-4 py-2 text-sm" onClick={onClose}>
          {t("Cancel")}
        </button>
        <button
          class="btn-accent px-4 py-2 text-sm"
          disabled={!name || clash || busy}
          onClick={create}
        >
          {busy ? t("Creating…") : t("Create")}
        </button>
      </div>
      <Missing items={[!name && t("a name"), clash && t("a name no other kitty has")]} />
    </Modal>
  );
}

// KittyBasics edits what a person most often changes: its brain, who it is and how it talks,
// and the connections it may use. Each change is saved at once and says so.
function KittyBasics({
  kitty,
  models,
  onChanged,
}: {
  kitty: Kitty;
  models: ProviderInfo[];
  onChanged: () => void;
}) {
  const [persona, setPersona] = useState(kitty.soul?.persona || "");
  const [tone, setTone] = useState(kitty.soul?.tone || "");
  const [connections, setConnections] = useState<string[]>([]);
  const [note, setNote] = useState("");
  const [err, setErr] = useState("");
  useEffect(() => {
    setPersona(kitty.soul?.persona || "");
    setTone(kitty.soul?.tone || "");
  }, [kitty.id]);
  useEffect(() => {
    api
      .connectorNames()
      .then(setConnections)
      .catch(() => {});
  }, [kitty.id]);
  useEffect(() => {
    if (!note) {
      return;
    }
    const id = setTimeout(() => setNote(""), 2000);
    return () => clearTimeout(id);
  }, [note]);
  const save = async (u: Parameters<typeof api.editKitty>[1], what: string) => {
    setErr("");
    try {
      await api.editKitty(kitty.id, u);
      setNote(what);
      onChanged();
    } catch (e) {
      setErr(errText(e));
    }
  };
  const mine = kitty.connectors || [];
  const all = usesAll(mine);
  const toggle = (c: string) => {
    const on = !mayUse(mine, c);
    save(
      { connectors: withConnection(mine, c, on, connections) },
      on
        ? t("{name} may use {connection}.", { name: kitty.name, connection: c })
        : t("{name} no longer uses {connection}.", { name: kitty.name, connection: c }),
    );
  };
  const dirty = persona !== (kitty.soul?.persona || "") || tone !== (kitty.soul?.tone || "");
  return (
    <div data-testid="kitty-basics">
      <div class="section-title !mx-0">{t("Brain")}</div>
      <div class="flex items-center gap-2">
        <span class="provider-badge">
          <ProviderLogo
            kind={models.find((m) => m.name === kitty.provider)?.kind || ""}
            size={22}
          />
        </span>
        <select
          class={INPUT}
          aria-label={t("Brain")}
          value={kitty.provider}
          onChange={(e) => {
            const v = (e.target as HTMLSelectElement).value;
            const p = models.find((m) => m.name === v);
            save(
              { provider: v },
              t("{name} now thinks with {model}.", {
                name: kitty.name,
                model: p ? modelLabel(p) : v,
              }),
            );
          }}
        >
          {!models.some((m) => m.name === kitty.provider) && (
            <option value={kitty.provider}>{kitty.provider || t("no model")}</option>
          )}
          {models.map((p) => (
            <option key={p.name} value={p.name}>
              {modelLabel(p)}
            </option>
          ))}
        </select>
      </div>
      <div class="section-title !mx-0 mt-4">{t("Personality")}</div>
      <label class="block text-xs text-[var(--t4)] mb-1" for="drawer-persona">
        {t("Who is it?")}
      </label>
      <textarea
        id="drawer-persona"
        class={`${INPUT} min-h-[60px]`}
        value={persona}
        onInput={(e) => setPersona((e.target as HTMLTextAreaElement).value)}
      />
      <label class="block text-xs text-[var(--t4)] mb-1 mt-2" for="drawer-tone">
        {t("How does it talk?")}
      </label>
      <input
        id="drawer-tone"
        class={INPUT}
        value={tone}
        onInput={(e) => setTone((e.target as HTMLInputElement).value)}
      />
      {dirty && (
        <button
          class="btn-outline px-3 py-1.5 text-xs mt-2"
          onClick={() =>
            save({ soul: { persona: persona.trim(), tone: tone.trim() } }, t("Saved."))
          }
        >
          {t("Save personality")}
        </button>
      )}
      {(kitty.soul?.traits?.length || 0) > 0 && (
        <div class="flex flex-wrap gap-1 mt-2">
          {kitty.soul!.traits!.map((t) => (
            <span key={t} class="tag">
              {t}
            </span>
          ))}
        </div>
      )}
      <div class="section-title !mx-0 mt-4">{t("Connections")}</div>
      {connections.length === 0 ? (
        <p class="text-xs text-[var(--t4)]">
          {t(
            "None yet. Add services in Settings, Connections, and choose the kitties that may use them.",
          )}
        </p>
      ) : (
        <div class="flex flex-wrap gap-2" data-testid="kitty-connections">
          <label
            class={`chip ${all ? "on" : ""}`}
            title={t("Every connection, including the ones you add later")}
          >
            <input
              type="checkbox"
              class="sr-only"
              checked={all}
              onChange={() =>
                all
                  ? save(
                      { connectors: [NONE] },
                      t("{name} uses no connection.", { name: kitty.name }),
                    )
                  : save(
                      { connectors: [] },
                      t("{name} may use every connection.", { name: kitty.name }),
                    )
              }
            />
            {t("All, new ones too")}
          </label>
          {connections.map((c) => (
            <label key={c} class={`chip ${mayUse(mine, c) ? "on" : ""}`}>
              <input
                type="checkbox"
                class="sr-only"
                checked={mayUse(mine, c)}
                onChange={() => toggle(c)}
              />
              {c}
            </label>
          ))}
        </div>
      )}
      {note && <p class="text-xs text-[var(--c-green)] mt-2">{note}</p>}
      {err && <p class="text-sm text-[var(--c-red)] mt-2">{err}</p>}
    </div>
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
  const mood: Mood =
    kitty.status === "running" ? "happy" : kitty.status === "error" ? "error" : "sleeping";
  const [looks, setLooks] = useState(false);
  const [models, setModels] = useState<ProviderInfo[]>([]);
  useEffect(() => {
    api
      .providers()
      .then(setModels)
      .catch(() => {});
  }, [kitty.id]);
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
            <button class="btn-ghost px-2 py-1" onClick={() => setLooks(!looks)}>
              {looks ? t("done") : t("change looks")}
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
          <div class="section-title !mx-0 mt-4">{t("Desktop")}</div>
          {kitty.desktop ? (
            <Shot key={tick} path={`/v1/kitties/${kitty.id}/screenshot?t=${tick}`} big />
          ) : (
            <div class="live-shot flex items-center justify-center text-[var(--t4)] text-sm">
              {t("brain only, no desktop")}
            </div>
          )}
          <div class="flex gap-2 mt-3 flex-wrap">
            <span
              class={`state-pill ${kitty.status === "running" ? "is-ok" : kitty.status === "error" ? "is-off" : ""}`}
            >
              {kitty.status}
            </span>
            <span class="tag tag-accent">
              {(() => {
                const p = models.find((m) => m.name === kitty.provider);
                return p ? modelLabel(p) : kitty.provider || t("no model");
              })()}
            </span>
            {kitty.desktop && <span class="tag">{kitty.container}</span>}
            {kitty.desktop && kitty.status === "running" && (
              <button
                class="btn-ghost px-2 py-1 text-xs flex items-center gap-1"
                onClick={() => setTick((t) => t + 1)}
              >
                <IconCamera size={12} /> {t("refresh")}
              </button>
            )}
          </div>
          {kitty.error && <p class="text-sm text-[var(--c-red)] mt-2">{kitty.error}</p>}
          <GoalsPanel kitty={kitty} onOpenChat={onOpenChat} />
          <JournalPanel kitty={kitty} onOpenChat={onOpenChat} />
        </div>
        <div class="text-sm">
          <KittyBasics kitty={kitty} models={models} onChanged={onChanged} />
          {soul.skills && soul.skills.length > 0 && (
            <div class="flex flex-wrap gap-1 mt-2">
              {soul.skills.map((t) => (
                <span key={t} class="tag tag-magenta">
                  {t}
                </span>
              ))}
            </div>
          )}
          <div class="section-title !mx-0 mt-5">{t("Actions")}</div>
          <div class="flex flex-col gap-2">
            <button class="btn-accent px-3 py-2 text-sm" onClick={onChat}>
              {t("Chat with {name}", { name: kitty.name })}
            </button>
            {kitty.status === "running" ? (
              <button
                class="btn-ghost px-3 py-2 text-sm flex items-center gap-2"
                onClick={() => act(() => api.stopKitty(kitty.id))}
              >
                <IconStop size={14} /> {t("Stop")}
              </button>
            ) : (
              <button
                class="btn-ghost px-3 py-2 text-sm flex items-center gap-2"
                onClick={() => act(() => api.startKitty(kitty.id))}
              >
                <IconPlay size={14} /> {t("Start")}
              </button>
            )}
            <button
              class="btn-danger px-3 py-2 text-sm flex items-center gap-2"
              onClick={async () =>
                (await ask(t("Remove {name} and its container?", { name: kitty.name }))) &&
                act(() => api.removeKitty(kitty.id).then(onClose))
              }
            >
              <IconTrash size={14} /> {t("Remove")}
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
  // The app's first start has nothing to complain about yet: it only asks.
  const firstStart = inAndroid() && !saved.url;
  const [err, setErr] = useState(firstStart ? "" : reason || "");
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
      setErr(errText(e));
      setBusy(false);
    }
  };
  const android = inAndroid();
  // The address is needed unless the page is served by the gateway itself; the app on a phone
  // also needs the token, since it cannot read the gateway's file.
  const needsURL = android || inWails();
  const missing = [
    needsURL && !url.trim() && t("the address"),
    android && !token.trim() && t("the token"),
  ].filter(Boolean) as string[];
  const body = (
    <>
      {android ? (
        <div class="text-sm text-[var(--t3)]" data-testid="android-connect">
          <p>
            {t(
              "The app is a remote for the Kitties gateway running on your computer or server. Type its address on the network and its token.",
            )}
          </p>
          <ol class="steps text-xs mt-2">
            <li>
              {t("Let the gateway listen on the network, not only on the computer:")}{" "}
              <code class="font-mono">KITTIES_LISTEN=0.0.0.0:7488 kitties gateway</code>
              {t(" (or a server with HTTPS, see DEPLOY.md).")}
            </li>
            <li>
              {t("The address is the computer's, like")}{" "}
              <code class="font-mono">http://192.168.1.20:7488</code>.
            </li>
            <li>
              {t("The token is in ")}
              <code class="font-mono">~/.kitties/gateway.token</code>
              {t(" on that computer.")}
            </li>
          </ol>
        </div>
      ) : (
        <p class="text-sm text-[var(--t3)]">
          {t(
            "The address of the kitties gateway. On this machine the token is found on its own (the gateway leaves it in ",
          )}
          <code class="font-mono">~/.kitties/gateway.token</code>
          {t("); a remote gateway needs the token it printed when it started.")}
          {!inWails() &&
            t(" Leave the address empty when this page is served by the gateway itself.")}
        </p>
      )}
      <label class={LABEL}>
        {t("Address")}
        {needsURL ? <Required /> : t(" (optional)")}
      </label>
      <input
        class={INPUT}
        aria-required={needsURL}
        value={url}
        onInput={(e) => setUrl((e.target as HTMLInputElement).value)}
        placeholder={android ? "http://192.168.1.20:7488" : "http://127.0.0.1:7488"}
        inputMode="url"
        autocapitalize="off"
        aria-label={t("Address")}
      />
      <label class={LABEL}>
        {t("Token")}
        {android ? <Required /> : t(" (remote gateways only)")}
      </label>
      <input
        class={`${INPUT} font-mono`}
        aria-required={android}
        value={token}
        onInput={(e) => setToken((e.target as HTMLInputElement).value)}
        placeholder={
          android ? t("paste the gateway's token") : t("found on this machine when empty")
        }
        autocapitalize="off"
        aria-label={t("Token")}
      />
      {err && <p class="text-sm text-[var(--c-red)] mt-2">{err}</p>}
      <div class="flex justify-end gap-2 mt-5">
        <button class="btn-ghost px-4 py-2 text-sm" onClick={onClose}>
          {t("Cancel")}
        </button>
        <button
          class="btn-accent px-4 py-2 text-sm"
          disabled={busy || missing.length > 0}
          title={
            missing.length ? t("Still needed: {items}", { items: missing.join(", ") }) : undefined
          }
          onClick={save}
        >
          {busy ? t("Checking…") : t("Connect")}
        </button>
      </div>
      <Missing items={missing} />
    </>
  );
  if (embedded) {
    return body;
  }
  return (
    <Modal title={t("Gateway")} onClose={onClose}>
      {body}
    </Modal>
  );
}
