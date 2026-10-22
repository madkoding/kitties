import { useEffect, useState } from "preact/hooks";
import type { Kitty, MemoryView } from "./api";
import { api } from "./api";
import { IconTrash } from "./icons";

// WHAT A KITTY KNOWS ABOUT YOU.
//
// Notes it was told or worked out, a level of how demanding you are (which shapes how careful it
// is and how soon it is trusted with actions alone), and how far each kind of action is on the
// way to being done unasked. Everything can be read, corrected and forgotten here.

const LABEL = "block text-xs text-[var(--t3)] mb-1 mt-3 uppercase tracking-wider font-mono";
const INPUT = "w-full px-3 py-2 text-sm";
const LEVELS = ["relaxed", "easygoing", "balanced", "demanding", "strict"];

const SCOPE_TAG: Record<string, string> = { user: "tag-accent", work: "tag-amber" };

export function MemoryPanel({ kitty }: { kitty: Kitty }) {
  const [view, setView] = useState<MemoryView | null>(null);
  const [err, setErr] = useState("");
  const [text, setText] = useState("");
  const [scope, setScope] = useState("user");
  const [busy, setBusy] = useState(false);
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
  return (
    <div data-testid="memory">
      <div class="section-title !mx-0 mt-5">What {kitty.name} knows about you</div>
      <label class={LABEL}>How demanding you are</label>
      <div class="flex gap-1 flex-wrap" role="group" aria-label="How demanding you are">
        {LEVELS.map((name, i) => (
          <button
            key={name}
            type="button"
            class={`${p.demand === i + 1 ? "btn-outline" : "btn-ghost"} px-2 py-1 text-xs`}
            disabled={busy}
            onClick={() => act(() => api.setProfile(kitty.id, i + 1, p.auto))}
          >
            {i + 1} · {name}
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
        let it follow my feedback ({p.up} up, {p.down} down)
      </label>
      {view.trust && Object.keys(view.trust).length > 0 && (
        <>
          <label class={LABEL}>On its way to acting alone</label>
          <div class="flex flex-col gap-1 text-xs" data-testid="trust">
            {Object.entries(view.trust).map(([name, t]) => (
              <div key={name} class="flex items-center gap-2">
                <code class="font-mono flex-1 truncate">{name}</code>
                <span class="text-[var(--t3)]">
                  {t.needs === 0
                    ? "never alone for you"
                    : `${t.streak} of ${t.needs} yeses in a row`}
                </span>
              </div>
            ))}
          </div>
        </>
      )}
      <label class={LABEL}>Notes</label>
      {view.notes.length === 0 && (
        <p class="text-xs text-[var(--t4)]">
          Nothing yet. It learns from your thumbs, your yes and no, and what it notices.
        </p>
      )}
      <div class="flex flex-col gap-1" data-testid="notes">
        {view.notes.map((n) => (
          <div key={n.id} class="flex items-start gap-2 text-sm">
            <span class={`tag ${SCOPE_TAG[n.scope] || "tag-magenta"}`}>
              {n.scope.replace("project:", "")}
            </span>
            <span class="flex-1 min-w-0">{n.text}</span>
            {n.pinned && (
              <span class="tag" title="You wrote it: a consolidation never drops it">
                yours
              </span>
            )}
            <button
              class="plain text-[var(--t3)] hover:text-[var(--c-red)] p-1"
              aria-label={`Forget: ${n.text}`}
              onClick={() => act(() => api.forgetNote(kitty.id, n.id))}
            >
              <IconTrash size={12} />
            </button>
          </div>
        ))}
      </div>
      <div class="flex gap-2 mt-2">
        <select
          class="px-2 py-2 text-sm"
          aria-label="Scope of the note"
          value={scope}
          onChange={(e) => setScope((e.target as HTMLSelectElement).value)}
        >
          <option value="user">about me</option>
          <option value="work">about the work</option>
        </select>
        <input
          class={INPUT}
          value={text}
          onInput={(e) => setText((e.target as HTMLInputElement).value)}
          placeholder="Always answer in Spanish"
          aria-label="A note for it to keep"
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
          Add note
        </button>
      </div>
      <button
        class="btn-ghost px-3 py-1.5 text-xs mt-2"
        disabled={busy || p.up + p.down === 0}
        onClick={() => act(() => api.distill(kitty.id))}
        title="Its own brain merges the notes and the feedback into a short list"
      >
        Consolidate now{p.pending > 0 ? ` (${p.pending} new)` : ""}
      </button>
      {err && <p class="text-sm text-[var(--c-red)] mt-2">{err}</p>}
    </div>
  );
}
