import { useEffect, useRef, useState } from "preact/hooks";
import type { HandOverDay, HandOverStat, Kitty, ProviderInfo } from "./api";
import {
  ApiError,
  api,
  errText,
  gatewayAddress,
  inAndroid,
  inWails,
  saveConnection,
  sendsInTheClear,
  savedConnection,
  THEMES,
} from "./api";
import { ask } from "./Confirm";
import { Modal } from "./Modal";
import { NONE, mayUse, providerLabel, usesAll, withConnection } from "./friendly";
import { ProviderLogo } from "./ConnectorLogo";
import { IconCrown, IconPlay, IconStop, IconTrash } from "./icons";
import DesktopCare from "./DesktopCare";
import MachinePower from "./MachinePower";
import LiveDesktop from "./LiveDesktop";
import type { LiveChatProps } from "./LiveChat";
import { GoalsPanel, JournalPanel, LimitsForm } from "./Agenda";
import { MemoryPanel } from "./Memory";
import PixelCat, { BREEDS, CatPicker, COLORS, type Mood } from "./PixelCat";
import { Missing, Required } from "./Required";
import { t } from "./i18n";
import { kittyTab, setKittyTab } from "./prefs";
import Tabs from "./Tabs";

// VOICES are the speech voices a kitty's WhatsApp voice notes can take (OpenAI's).
const VOICES = [
  "alloy",
  "ash",
  "ballad",
  "coral",
  "echo",
  "fable",
  "nova",
  "onyx",
  "sage",
  "shimmer",
  "verse",
];

// suggestAvatar picks a cat from the name, the way the gateway does when none is chosen.
function suggestAvatar(name: string): { breed: string; color: string } {
  let h = 2166136261;
  for (const ch of name) {
    h = Math.imul(h ^ ch.charCodeAt(0), 16777619) >>> 0;
  }
  return { breed: BREEDS[h % BREEDS.length], color: COLORS[Math.floor(h / 7) % COLORS.length] };
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
  // A new chat starts with the lead kitty picked, when there is one: it orchestrates the rest.
  const [picked, setPicked] = useState<Set<string>>(
    () => new Set(kitties.filter((k) => k.lead).map((k) => k.id)),
  );
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
            <span class="text-xs text-[var(--t4)] truncate max-w-[13.75rem]">
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
  dockerState,
  taken = [],
}: {
  onClose: () => void;
  // onCreated gets what the person should know about the new kitty (a desktop that could not
  // start), if anything: the kitty exists either way.
  onCreated: (note?: string) => void;
  onNeedModel?: () => void;
  containers?: boolean;
  // dockerState says why there are no containers: Docker is not installed, or it is closed.
  dockerState?: "not_installed" | "not_running";
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
          {dockerState === "not_installed"
            ? t(
                "Docker is not installed on the gateway's machine. Install Docker Desktop to give kitties a computer; without it they still think, talk and use connections.",
              )
            : dockerState === "not_running"
              ? t(
                  "Docker is installed but not running. Open Docker Desktop and wait for it to start; the option unlocks by itself.",
                )
              : t(
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
      {kitty.desktop && (
        <>
          <div class="section-title !mx-0 mt-4">{t("Desktop look")}</div>
          <select
            class={INPUT}
            aria-label={t("Desktop look")}
            value={kitty.theme || ""}
            onChange={(e) => {
              const v = (e.target as HTMLSelectElement).value;
              const label = THEMES.find(([name]) => name === v)?.[1] || v;
              save(
                { theme: v },
                t("{name}'s desktop now wears {theme}.", { name: kitty.name, theme: label }),
              );
            }}
            data-testid="desktop-theme"
          >
            {THEMES.map(([name, label]) => (
              <option key={name} value={name}>
                {label}
              </option>
            ))}
          </select>
        </>
      )}
      <div class="section-title !mx-0 mt-4">{t("Personality")}</div>
      <label class="block text-xs text-[var(--t4)] mb-1" for="drawer-persona">
        {t("Who is it?")}
      </label>
      <textarea
        id="drawer-persona"
        class={`${INPUT} min-h-[3.75rem]`}
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
      <label class="block text-xs text-[var(--t4)] mb-1 mt-3" for="drawer-voice">
        {t("Voice for WhatsApp voice notes")}
      </label>
      <select
        id="drawer-voice"
        class={INPUT}
        value={kitty.soul?.voice && kitty.soul.voice !== "default" ? kitty.soul.voice : ""}
        onChange={(e) => {
          const v = (e.target as HTMLSelectElement).value;
          save(
            { soul: { voice: v || "default" } },
            v
              ? t("{name} now speaks with the voice {voice}.", { name: kitty.name, voice: v })
              : t("{name} speaks with the gateway's voice.", { name: kitty.name }),
          );
        }}
        data-testid="drawer-voice"
      >
        <option value="">{t("The gateway's voice")}</option>
        {VOICES.map((v) => (
          <option key={v} value={v}>
            {v}
          </option>
        ))}
      </select>
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

// SUMMARY_ASK is what the person asks the lead with "Summarize reports", in the page's language
// (the lead answers in the language it is written to).
const SUMMARY_ASK = () =>
  t(
    "Summarize the reports in this thread since your last summary: what each kitty did, what is stuck or failed, what waits for me, and what you suggest next. Short, in bullets.",
  );

// LeadPanel makes a kitty the lead (the principal one, which orchestrates the others and
// reports for all of them) or takes the lead from it, and opens its Reports thread.
// dayLabel is a day of the chart, short, in the page's language.
function dayLabel(day: string): string {
  const d = new Date(`${day}T12:00:00`);
  return isNaN(d.getTime())
    ? day
    : d.toLocaleDateString(document.documentElement.lang || undefined, {
        day: "numeric",
        month: "short",
      });
}

// HandOverChart is the team's hand-overs per day, the last two weeks: a bar a day, the ones that
// failed stacked on the ones done, the tallest day as the scale. Each bar says its numbers on
// hover, and a table under it says them all for a screen reader.
export function HandOverChart({ days }: { days: HandOverDay[] }) {
  const top = Math.max(1, ...days.map((d) => d.count));
  const total = days.reduce((n, d) => n + d.count, 0);
  const failed = days.reduce((n, d) => n + d.failed, 0);
  const tip = (d: HandOverDay) =>
    t("{day}: {n} handed around, {failed} failed", {
      day: dayLabel(d.day),
      n: d.count,
      failed: d.failed,
    });
  return (
    <figure class="handover-chart" data-testid="handover-chart">
      <figcaption class="flex items-center gap-3 flex-wrap text-xs text-[var(--t4)] mb-1">
        <span class="flex-1 min-w-0">
          {t("Subtasks per day, last {n} days: {total} ({failed} failed)", {
            n: days.length,
            total,
            failed,
          })}
        </span>
        <span class="flex items-center gap-1">
          <span class="handover-key done" aria-hidden="true" />
          {t("done")}
        </span>
        <span class="flex items-center gap-1">
          <span class="handover-key failed" aria-hidden="true" />
          {t("failed")}
        </span>
      </figcaption>
      <div class="handover-bars" aria-hidden="true">
        {days.map((d) => (
          <div key={d.day} class="handover-day" title={tip(d)} data-testid="handover-day">
            <div class="handover-stack" style={`height: ${(d.count / top) * 100}%`}>
              {d.failed > 0 && <div class="handover-seg failed" style={`flex-grow: ${d.failed}`} />}
              {d.count - d.failed > 0 && (
                <div class="handover-seg done" style={`flex-grow: ${d.count - d.failed}`} />
              )}
            </div>
          </div>
        ))}
      </div>
      <div class="handover-axis text-[0.625rem] text-[var(--t4)]" aria-hidden="true">
        <span>{dayLabel(days[0].day)}</span>
        <span>{dayLabel(days[days.length - 1].day)}</span>
      </div>
      <table class="sr-only">
        <caption>{t("Subtasks per day")}</caption>
        <tbody>
          {days.map((d) => (
            <tr key={d.day}>
              <td>{tip(d)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </figure>
  );
}

export function LeadPanel({
  kitty,
  kitties,
  onChanged,
  onOpenChat,
}: {
  kitty: Kitty;
  kitties: Kitty[];
  onChanged: () => void;
  onOpenChat: (id: string) => void;
}) {
  const [err, setErr] = useState("");
  // stats is how the team hands work around (who asks, who takes it on, what fails), for the lead.
  const [stats, setStats] = useState<HandOverStat[]>([]);
  // days are the team's hand-overs per day, for the chart (an older gateway gives none).
  const [days, setDays] = useState<HandOverDay[]>([]);
  // open is the row whose hand-over threads are listed.
  const [open, setOpen] = useState("");
  useEffect(() => {
    if (!kitty.lead) {
      setStats([]);
      setDays([]);
      return;
    }
    api
      .lead()
      .then((r) => {
        setStats(r.handovers || []);
        setDays(r.handover_days || []);
      })
      .catch(() => {
        setStats([]);
        setDays([]);
      });
  }, [kitty.id, kitty.lead]);
  const current = kitties.find((k) => k.lead && k.id !== kitty.id);
  const set = async (on: boolean) => {
    setErr("");
    try {
      await api.setLead(on ? kitty.id : "");
      onChanged();
    } catch (e) {
      setErr((e as Error).message);
    }
  };
  const reports = async () => {
    setErr("");
    try {
      const r = await api.lead();
      if (r.reports) onOpenChat(r.reports);
    } catch (e) {
      setErr((e as Error).message);
    }
  };
  // summarize opens the Reports thread and asks the lead for a summary there: the answer
  // arrives in the thread while the person watches.
  const summarize = async () => {
    setErr("");
    try {
      const r = await api.lead();
      if (!r.reports) return;
      onOpenChat(r.reports);
      api.send(r.reports, SUMMARY_ASK()).catch((e) => setErr((e as Error).message));
    } catch (e) {
      setErr((e as Error).message);
    }
  };
  return (
    <div class="lead-panel mt-4" data-testid="lead-panel">
      <div class="flex items-center gap-2 flex-wrap">
        <span class={kitty.lead ? "text-accent" : "text-[var(--t4)]"}>
          <IconCrown size={16} />
        </span>
        <div class="flex-1 min-w-0 basis-[14rem]">
          <div class="text-sm font-medium">
            {kitty.lead
              ? t("{name} is the lead kitty", { name: kitty.name })
              : t("Lead kitty (orchestrator)")}
          </div>
          <p class="text-xs text-[var(--t4)]">
            {kitty.lead
              ? t(
                  "It splits what you ask among the other kitties, gives each a task that stands on its own, gathers their answers and gives you one report. The others report to it and ask it their doubts; it asks you only what it cannot decide. Everything reaches you through it, in its Reports thread.",
                )
              : current
                ? t("{lead} leads the fleet now; making {name} the lead takes it from {lead}.", {
                    lead: current.name,
                    name: kitty.name,
                  })
                : t(
                    "One kitty can lead the others: you talk to it, it hands the work out and reports for all of them.",
                  )}
          </p>
        </div>
        <div class="flex gap-2 flex-wrap">
          {kitty.lead ? (
            <>
              <button
                class="btn-accent px-3 py-1.5 text-xs"
                onClick={summarize}
                data-testid="lead-summary"
              >
                {t("Summarize reports")}
              </button>
              <button class="btn-outline px-3 py-1.5 text-xs" onClick={reports}>
                {t("Open reports")}
              </button>
              <button class="btn-ghost px-3 py-1.5 text-xs" onClick={() => set(false)}>
                {t("Stop leading")}
              </button>
            </>
          ) : (
            <button
              class="btn-outline px-3 py-1.5 text-xs flex items-center gap-1"
              onClick={() => set(true)}
            >
              <IconCrown size={12} /> {t("Make {name} the lead", { name: kitty.name })}
            </button>
          )}
        </div>
      </div>
      {kitty.lead && days.length > 0 && <HandOverChart days={days} />}
      {kitty.lead && stats.length > 0 && (
        <table class="handover-stats" data-testid="handover-stats">
          <caption class="text-xs text-[var(--t4)] text-left mb-1">
            {t("Subtasks handed around the team")}
          </caption>
          <thead>
            <tr>
              <th scope="col">{t("Kitty")}</th>
              <th scope="col">{t("Handed out")}</th>
              <th scope="col">{t("Taken on")}</th>
              <th scope="col">{t("Failed")}</th>
            </tr>
          </thead>
          <tbody>
            {stats.map((s) => [
              <tr key={s.kitty_id} data-testid="handover-row">
                <td>
                  {s.threads && s.threads.length > 0 ? (
                    <button
                      class="plain handover-kitty"
                      onClick={() => setOpen(open === s.kitty_id ? "" : s.kitty_id)}
                      aria-expanded={open === s.kitty_id}
                      title={t("See {name}'s hand-over threads", { name: s.kitty })}
                    >
                      {open === s.kitty_id ? "▾" : "▸"} {s.kitty}
                    </button>
                  ) : (
                    s.kitty
                  )}
                </td>
                <td>{s.asked}</td>
                <td>{s.took}</td>
                <td class={s.took_failed ? "text-[var(--c-red)]" : ""}>{s.took_failed}</td>
              </tr>,
              open === s.kitty_id && (
                <tr key={s.kitty_id + "-threads"} data-testid="handover-threads">
                  <td colSpan={4}>
                    <ul class="handover-threads">
                      {s.threads!.map((th) => (
                        <li key={th.id}>
                          <button class="plain" onClick={() => onOpenChat(th.id)}>
                            {th.title || th.id}
                          </button>
                        </li>
                      ))}
                    </ul>
                  </td>
                </tr>
              ),
            ])}
          </tbody>
        </table>
      )}
      {err && <p class="text-sm text-[var(--c-red)] mt-2">{err}</p>}
    </div>
  );
}

// KittyTab is a section of a kitty's drawer.
export type KittyTab = "desktop" | "goals" | "team" | "journal" | "notes" | "config";

// KITTY_TABS are the drawer's sections, in order, in the page's language. Desktop is only for a
// kitty with one, and Team only for the lead (the others offer to lead under Configuration).
const KITTY_TABS = (): { id: KittyTab; label: string }[] => [
  { id: "desktop", label: t("Desktop") },
  { id: "goals", label: t("Goals") },
  { id: "team", label: t("Team") },
  { id: "journal", label: t("Journal") },
  { id: "notes", label: t("Notes") },
  { id: "config", label: t("Configuration") },
];

// ATTENTION are how a goal's last run ends when it needs the person to look at it.
const ATTENTION = ["blocked", "escalated", "failed"];

// useTabCounts counts what waits for the person in a kitty's drawer: goals that need a look and
// notes proposed for every kitty. It counts again when the person moves between tabs, so what
// was handled in one shows on the way out.
function useTabCounts(kitty: string, tab: string): Partial<Record<KittyTab, number>> {
  const [goals, setGoals] = useState(0);
  const [notes, setNotes] = useState(0);
  useEffect(() => {
    let alive = true;
    api
      .goals(kitty)
      .then((r) => {
        if (alive) setGoals(r.filter((g) => ATTENTION.includes(g.last_result || "")).length);
      })
      .catch(() => alive && setGoals(0));
    api
      .memory(kitty)
      .then((v) => {
        if (alive) setNotes(v.notes.filter((n) => n.proposed).length);
      })
      .catch(() => alive && setNotes(0));
    return () => {
      alive = false;
    };
  }, [kitty, tab]);
  return { goals, notes };
}

export function KittyDrawer({
  kitty,
  kitties,
  onClose,
  onChanged,
  onChat,
  onOpenChat,
  chat,
  tab: wanted = "",
  onTab = () => undefined,
}: {
  kitty: Kitty;
  kitties: Kitty[];
  onClose: () => void;
  onChanged: () => void;
  onChat: () => void;
  onOpenChat: (id: string) => void;
  // tab is the tab the address names (#kitty/<id>/<tab>); onTab tells the address the one picked.
  tab?: string;
  onTab?: (tab: KittyTab) => void;
  // chat is the direct chat with this kitty, for the sidebar of its enlarged desktop.
  chat?: LiveChatProps;
}) {
  const [err, setErr] = useState("");
  const mood: Mood =
    kitty.status === "running" ? "happy" : kitty.status === "error" ? "error" : "sleeping";
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
  const act = async (fn: () => Promise<unknown>) => {
    setErr("");
    try {
      await fn();
      onChanged();
    } catch (e) {
      setErr((e as Error).message);
    }
  };
  // A kitty with a machine is turned on and off through its power: a machine in use asks first.
  const power = (on: boolean) =>
    act(async () => {
      try {
        await api.setPower(kitty.id, on);
      } catch (e) {
        if (!on && e instanceof ApiError && e.status === 409) {
          if (await ask(e.message, t("Turn off anyway"))) {
            await api.setPower(kitty.id, false, true);
          }
          return;
        }
        throw e;
      }
    });
  const soul = kitty.soul || {};
  const running = kitty.status === "running";
  const model = models.find((m) => m.name === kitty.provider);
  // The drawer is split in tabs; the desktop's only shows for a kitty that has one. The tab the
  // person left a kitty on is where its drawer opens next time.
  // The address's tab wins over the one kept from last time.
  const [chosen, setChosen] = useState<KittyTab>(() => (wanted || kittyTab(kitty.id)) as KittyTab);
  useEffect(() => setChosen((wanted || kittyTab(kitty.id)) as KittyTab), [kitty.id, wanted]);
  const counts = useTabCounts(kitty.id, chosen);
  const tabs = KITTY_TABS()
    .filter((x) => (x.id !== "desktop" || kitty.desktop) && (x.id !== "team" || kitty.lead))
    .map((x) => ({ ...x, count: counts[x.id] }));
  const tab = tabs.some((x) => x.id === chosen) ? chosen : tabs[0].id;
  const pickTab = (x: KittyTab) => {
    setChosen(x);
    setKittyTab(kitty.id, x);
    onTab(x);
  };
  // The digits 1 to 6 pick a tab, unless the person is typing, holds the desktop's controls or
  // has a window open over the drawer.
  const keys = useRef({ tabs, pickTab });
  keys.current = { tabs, pickTab };
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const n = Number(e.key);
      if (
        e.defaultPrevented ||
        e.altKey ||
        e.ctrlKey ||
        e.metaKey ||
        !Number.isInteger(n) ||
        n < 1 ||
        n > keys.current.tabs.length ||
        document.querySelectorAll('[role="dialog"]').length > 1
      ) {
        return;
      }
      const el = e.target as HTMLElement | null;
      if (
        el?.closest?.(
          'input, textarea, select, [contenteditable], [data-testid="live-desktop"], iframe',
        ) ||
        el?.isContentEditable
      ) {
        return;
      }
      e.preventDefault();
      keys.current.pickTab(keys.current.tabs[n - 1].id);
      document.getElementById(`kitty-tab-${keys.current.tabs[n - 1].id}`)?.focus();
    };
    addEventListener("keydown", onKey);
    return () => removeEventListener("keydown", onKey);
  }, []);
  return (
    <Modal title={kitty.name} onClose={onClose} wide>
      <div class="flex items-center gap-4 flex-wrap" data-testid="kitty-header">
        <div class="avatar-stage !p-2 !pt-3 shrink-0">
          <PixelCat breed={kitty.avatar?.breed} color={kitty.avatar?.color} mood={mood} size={72} />
        </div>
        <div class="min-w-0 flex-1 basis-[14rem]">
          <div class="flex gap-2 flex-wrap items-center min-w-0">
            <span
              class={`state-pill ${running ? "is-ok" : kitty.status === "error" ? "is-off" : ""}`}
            >
              {kitty.status}
            </span>
            {kitty.lead && (
              <span class="tag tag-accent flex items-center gap-1" data-testid="lead-tag">
                <IconCrown size={11} /> {t("Lead")}
              </span>
            )}
            <span class="tag tag-accent max-w-full min-w-0">
              <span class="truncate">
                {model ? modelLabel(model) : kitty.provider || t("no model")}
              </span>
            </span>
            {kitty.desktop && (
              <span class="tag max-w-full min-w-0">
                <span class="truncate">{kitty.container}</span>
              </span>
            )}
          </div>
          {soul.skills && soul.skills.length > 0 && (
            <div class="flex flex-wrap gap-1 mt-2">
              {soul.skills.map((s) => (
                <span key={s} class="tag tag-magenta">
                  {s}
                </span>
              ))}
            </div>
          )}
          {kitty.error && <p class="text-sm text-[var(--c-red)] mt-2">{kitty.error}</p>}
        </div>
        <div class="flex gap-2 flex-wrap shrink-0 max-w-full" data-testid="kitty-actions">
          <button class="btn-accent px-3 py-2 text-sm" onClick={onChat}>
            {t("Chat with {name}", { name: kitty.name })}
          </button>
          {running ? (
            <button
              class="btn-ghost px-3 py-2 text-sm flex items-center gap-2"
              onClick={() => (kitty.desktop ? power(false) : act(() => api.stopKitty(kitty.id)))}
            >
              <IconStop size={14} /> {t("Stop")}
            </button>
          ) : (
            <button
              class="btn-ghost px-3 py-2 text-sm flex items-center gap-2"
              onClick={() => (kitty.desktop ? power(true) : act(() => api.startKitty(kitty.id)))}
            >
              <IconPlay size={14} /> {t("Start")}
            </button>
          )}
        </div>
      </div>
      {err && <p class="text-sm text-[var(--c-red)] mt-2">{err}</p>}
      <div class="h-4" />
      <Tabs
        tabs={tabs}
        tab={tab}
        onPick={pickTab}
        prefix="kitty"
        keys
        label={t("{name}'s sections", { name: kitty.name })}
        panels
      />
      <div
        role="tabpanel"
        id={`kitty-panel-${tab}`}
        aria-labelledby={`kitty-tab-${tab}`}
        class="kitty-panel min-w-0"
        data-testid={`kitty-panel-${tab}`}
      >
        {tab === "desktop" && kitty.desktop && (
          <>
            {/* The viewer always comes first; the machine's options follow it. */}
            {running ? (
              <LiveDesktop
                kitty={kitty}
                inline
                onClose={() => undefined}
                chat={chat}
                onStartChat={onChat}
              />
            ) : (
              <p class="text-sm text-[var(--t3)]" data-testid="desktop-off">
                {t("Start {name} to see its desktop live and take control of it.", {
                  name: kitty.name,
                })}
              </p>
            )}
            <div class="mt-4">
              <MachinePower kitty={kitty} onChanged={onChanged} />
            </div>
            <div class="mt-5">
              <DesktopCare kitty={kitty} onChanged={onChanged} />
            </div>
          </>
        )}
        {tab === "goals" && <GoalsPanel kitty={kitty} onOpenChat={onOpenChat} />}
        {tab === "team" && (
          <LeadPanel
            kitty={kitty}
            kitties={kitties}
            onChanged={onChanged}
            onOpenChat={onOpenChat}
          />
        )}
        {tab === "journal" && <JournalPanel kitty={kitty} onOpenChat={onOpenChat} />}
        {tab === "notes" && <MemoryPanel kitty={kitty} />}
        {tab === "config" && (
          <div data-testid="kitty-config">
            <div class="grid md:grid-cols-2 gap-x-6 gap-y-2 text-sm">
              <div class="min-w-0">
                <div class="section-title !mx-0">{t("Looks")}</div>
                <CatPicker
                  breed={kitty.avatar?.breed || "tabby"}
                  color={kitty.avatar?.color || "orange"}
                  onChange={pick}
                />
                <div class="mt-4">
                  <KittyBasics kitty={kitty} models={models} onChanged={onChanged} />
                </div>
              </div>
              <div class="min-w-0">
                <LimitsForm kitty={kitty} kitties={kitties} onChanged={onChanged} />
                <div class="mt-6">
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
              </div>
            </div>
            {!kitty.lead && (
              <LeadPanel
                kitty={kitty}
                kitties={kitties}
                onChanged={onChanged}
                onOpenChat={onOpenChat}
              />
            )}
          </div>
        )}
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
      const address = gatewayAddress(url);
      if (address === null) {
        throw new Error(
          t("The address has to be like http://192.168.1.20:7488 or https://kitties.example.com."),
        );
      }
      const app = window.go?.main?.App;
      if (app) {
        await app.Connect(address, token);
      } else {
        saveConnection({ url: address, token: token.trim() });
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
      {sendsInTheClear(gatewayAddress(url) || "") && (
        <p class="text-xs text-[var(--c-red)] mt-1" data-testid="clear-warning">
          {t("This address is plain http: the token would travel unencrypted. Use https.")}
        </p>
      )}
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
