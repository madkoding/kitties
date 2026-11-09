import { useEffect, useState } from "preact/hooks";
import type { Kitty, MemoryView } from "./api";
import { api } from "./api";
import { t } from "./i18n";
import { NoteRow } from "./Notes";

// WHAT A KITTY KNOWS ABOUT YOU.
//
// Notes it was told or worked out, a level of how demanding you are (which shapes how careful it
// is and how soon it is trusted with actions alone), and how far each kind of action is on the
// way to being done unasked. Everything can be read, corrected and forgotten here.

const LABEL = "block text-xs text-[var(--t3)] mb-1 mt-3 uppercase tracking-wider font-mono";
const INPUT = "w-full px-3 py-2 text-sm";
const LEVELS = ["relaxed", "easygoing", "balanced", "demanding", "strict"];

// The file a memory is exported to and imported from.
interface MemoryFile {
  kitties_memory: 1;
  kitty: string;
  notes: { scope: string; text: string }[];
}

// exportMemory saves a kitty's notes as a JSON file.
function exportMemory(name: string, view: MemoryView) {
  const file: MemoryFile = {
    kitties_memory: 1,
    kitty: name,
    notes: view.notes.map((n) => ({ scope: n.scope, text: n.text })),
  };
  const url = URL.createObjectURL(
    new Blob([JSON.stringify(file, null, 2)], { type: "application/json" }),
  );
  const a = document.createElement("a");
  a.href = url;
  a.download = `${name}-memory.json`;
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

// notesIn reads the notes of an exported memory, or fails with a word for the person.
export function notesIn(raw: string): { scope: string; text: string }[] {
  let data: unknown;
  try {
    data = JSON.parse(raw);
  } catch {
    throw new Error(t("That file is not a kitty's memory."));
  }
  const notes = (data as Partial<MemoryFile>)?.notes;
  if (!Array.isArray(notes)) {
    throw new Error(t("That file is not a kitty's memory."));
  }
  return notes
    .filter((n) => n && typeof n.text === "string" && n.text.trim())
    .map((n) => ({ scope: typeof n.scope === "string" ? n.scope : "user", text: n.text.trim() }));
}

export function MemoryPanel({ kitty }: { kitty: Kitty }) {
  const [view, setView] = useState<MemoryView | null>(null);
  const [err, setErr] = useState("");
  const [text, setText] = useState("");
  const [scope, setScope] = useState("user");
  const [busy, setBusy] = useState(false);
  const [info, setInfo] = useState("");
  const [query, setQuery] = useState("");
  const [only, setOnly] = useState("");
  const load = () => {
    api
      .memory(kitty.id)
      .then(setView)
      .catch((e) => {
        setView(null);
        setErr((e as Error).message);
      });
  };
  useEffect(load, [kitty.id]);
  const act = async (fn: () => Promise<unknown>) => {
    setErr("");
    setInfo("");
    setBusy(true);
    try {
      await fn();
      load();
    } catch (e) {
      setErr((e as Error).message);
    } finally {
      setBusy(false);
    }
  };
  if (!view) {
    return err ? <p class="text-xs text-[var(--t4)] mt-5">{err}</p> : null;
  }
  const p = view.profile;
  const scopes = [...new Set(view.notes.map((n) => n.scope))].sort();
  const words = query.trim().toLowerCase();
  const shown = view.notes.filter(
    (n) => (!only || n.scope === only) && (!words || n.text.toLowerCase().includes(words)),
  );
  return (
    <div data-testid="memory">
      <div class="section-title !mx-0 mt-5">
        {t("What {name} knows about you", { name: kitty.name })}
      </div>
      <label class={LABEL}>{t("How demanding you are")}</label>
      <div class="flex gap-1 flex-wrap" role="group" aria-label={t("How demanding you are")}>
        {LEVELS.map((name, i) => (
          <button
            key={name}
            type="button"
            class={`${p.demand === i + 1 ? "btn-outline" : "btn-ghost"} px-2 py-1 text-xs`}
            disabled={busy}
            onClick={() => act(() => api.setProfile(kitty.id, i + 1, p.auto))}
          >
            {i + 1} · {t(name)}
          </button>
        ))}
      </div>
      <p class="text-xs text-[var(--t3)] mt-1" data-testid="level">
        {view.level}
      </p>
      <label class="flex items-center gap-2 text-xs text-[var(--t3)] mt-2">
        <input
          type="checkbox"
          checked={p.auto}
          onChange={(e) =>
            act(() => api.setProfile(kitty.id, p.demand, (e.target as HTMLInputElement).checked))
          }
        />{" "}
        {t("let it follow my feedback ({up} up, {down} down)", { up: p.up, down: p.down })}
      </label>
      {view.trust && Object.keys(view.trust).length > 0 && (
        <>
          <label class={LABEL}>{t("On its way to acting alone")}</label>
          <div class="flex flex-col gap-1 text-xs" data-testid="trust">
            {Object.entries(view.trust).map(([name, tr]) => (
              <div key={name} class="flex items-center gap-2">
                <code class="font-mono flex-1 truncate">{name}</code>
                <span class="text-[var(--t3)]">
                  {tr.needs === 0
                    ? t("never alone for you")
                    : t("{streak} of {needs} yeses in a row", {
                        streak: tr.streak,
                        needs: tr.needs,
                      })}
                </span>
              </div>
            ))}
          </div>
        </>
      )}
      {view.spent && (
        <>
          <label class={LABEL}>{t("Today")}</label>
          <div class="flex flex-col gap-1 text-xs" data-testid="spent">
            {(
              [
                ["tokens", view.spent.tokens, view.spent.day_tokens],
                ["actions", view.spent.actions, view.spent.day_actions],
              ] as [string, number, number][]
            ).map(([name, used, max]) => (
              <div key={name} class="flex items-center gap-2">
                <span class="flex-1">{t(name)}</span>
                <span
                  class={
                    max > 0 && used >= max
                      ? "text-[var(--c-red)]"
                      : max > 0 && used * 100 >= max * 80
                        ? "text-[var(--c-amber)]"
                        : "text-[var(--t3)]"
                  }
                >
                  {used.toLocaleString("en-US")}
                  {max > 0
                    ? " " + t("of {max}", { max: max.toLocaleString("en-US") })
                    : " " + t("(no limit)")}
                </span>
              </div>
            ))}
          </div>
        </>
      )}
      <label class={LABEL}>{t("Notes")}</label>
      {view.notes.length === 0 && (
        <p class="text-xs text-[var(--t4)]">
          {t("Nothing yet. It learns from your thumbs, your yes and no, and what it notices.")}
        </p>
      )}
      {view.notes.length > 4 && (
        <div class="flex gap-2 mb-2">
          <input
            class="flex-1 min-w-0 px-2 py-1 text-xs"
            type="search"
            value={query}
            onInput={(e) => setQuery((e.target as HTMLInputElement).value)}
            placeholder={t("Search the notes")}
            aria-label={t("Search the notes")}
          />
          <select
            class="px-2 py-1 text-xs"
            aria-label={t("Show the notes of")}
            value={only}
            onChange={(e) => setOnly((e.target as HTMLSelectElement).value)}
          >
            <option value="">{t("every scope")}</option>
            {scopes.map((sc) => (
              <option key={sc} value={sc}>
                {sc.startsWith("project:") ? sc.replace("project:", "") : t(sc)}
              </option>
            ))}
          </select>
        </div>
      )}
      <div class="flex flex-col gap-1" data-testid="notes">
        {shown.length === 0 && view.notes.length > 0 && (
          <p class="text-xs text-[var(--t4)]">{t("No note matches.")}</p>
        )}
        {shown.map((n) => (
          <NoteRow
            key={n.id}
            n={n}
            busy={busy}
            onSave={(text) => act(() => api.editNote(kitty.id, n.id, text))}
            onForget={() => act(() => api.forgetNote(kitty.id, n.id))}
          />
        ))}
      </div>
      <div class="flex gap-2 mt-2">
        <select
          class="px-2 py-2 text-sm"
          aria-label={t("Scope of the note")}
          value={scope}
          onChange={(e) => setScope((e.target as HTMLSelectElement).value)}
        >
          <option value="user">{t("about me")}</option>
          <option value="work">{t("about the work")}</option>
          <option value="shared">{t("for every kitty")}</option>
          <option value="people">{t("about people and groups (WhatsApp)")}</option>
        </select>
        <input
          class={INPUT}
          value={text}
          onInput={(e) => setText((e.target as HTMLInputElement).value)}
          placeholder={t("Always answer in Spanish")}
          aria-label={t("A note for it to keep")}
        />
        <button
          class="btn-outline px-3 py-1.5 text-xs whitespace-nowrap"
          disabled={busy || !text.trim()}
          onClick={() =>
            act(async () => {
              await api.remember(kitty.id, text.trim(), scope);
              setText("");
            })
          }
        >
          {t("Add note")}
        </button>
      </div>
      <div class="flex flex-wrap gap-2 mt-2">
        <button
          class="btn-ghost px-3 py-1.5 text-xs"
          disabled={busy || p.up + p.down === 0}
          onClick={() => act(() => api.distill(kitty.id))}
          title={t("Its own brain merges the notes and the feedback into a short list")}
        >
          {t("Consolidate now")}
          {p.pending > 0 ? " " + t("({n} new)", { n: p.pending }) : ""}
        </button>
        <button
          class="btn-ghost px-3 py-1.5 text-xs"
          disabled={busy || view.notes.length === 0}
          onClick={() => exportMemory(kitty.name, view)}
          title={t("Save the notes in a file, to keep or to give to another kitty")}
        >
          {t("Export")}
        </button>
        <label class={`btn-ghost px-3 py-1.5 text-xs cursor-pointer ${busy ? "opacity-50" : ""}`}>
          {t("Import")}
          <input
            type="file"
            accept="application/json,.json"
            class="sr-only"
            aria-label={t("Import notes from a file")}
            disabled={busy}
            onChange={(e) => {
              const input = e.target as HTMLInputElement;
              const file = input.files?.[0];
              input.value = "";
              if (!file) {
                return;
              }
              act(async () => {
                const notes = notesIn(await file.text());
                for (const n of notes) {
                  await api.remember(kitty.id, n.text, n.scope);
                }
                setInfo(t("Notes imported: {n}.", { n: notes.length }));
              });
            }}
          />
        </label>
      </div>
      {info && <p class="text-xs text-[var(--t3)] mt-2">{info}</p>}
      {err && <p class="text-sm text-[var(--c-red)] mt-2">{err}</p>}
    </div>
  );
}
