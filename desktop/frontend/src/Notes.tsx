import { useEffect, useRef, useState } from "preact/hooks";
import type { MemoryNote } from "./api";
import { api } from "./api";
import { t } from "./i18n";
import { IconPencil, IconTrash } from "./icons";

// A NOTE A KITTY KEEPS, AND THE NOTES EVERY KITTY SHARES.
//
// A note can be corrected in place (it becomes the person's, so a consolidation keeps it) or
// forgotten. The shared notes are every kitty's: they are tended here, in Settings, as well as
// from any kitty's memory.

export const SCOPE_TAG: Record<string, string> = {
  user: "tag-accent",
  work: "tag-amber",
  shared: "tag-green",
};

// NoteRow is one note: its scope, its text (or the box that corrects it) and its buttons.
export function NoteRow({
  n,
  busy,
  onSave,
  onForget,
}: {
  n: MemoryNote;
  busy: boolean;
  onSave: (text: string) => void;
  onForget: () => void;
}) {
  const [draft, setDraft] = useState<string | null>(null);
  // Escape drops the correction: the blur that follows must not save it.
  const dropped = useRef(false);
  const save = () => {
    if (dropped.current) {
      dropped.current = false;
      return;
    }
    const text = (draft || "").trim();
    if (text && text !== n.text) {
      onSave(text);
    }
    setDraft(null);
  };
  return (
    <div class="flex items-start gap-2 text-sm">
      <span
        class={`tag ${SCOPE_TAG[n.scope] || "tag-magenta"}`}
        title={n.scope === "shared" ? t("Every kitty knows it") : undefined}
      >
        {n.scope.startsWith("project:") ? n.scope.replace("project:", "") : t(n.scope)}
      </span>
      {draft === null ? (
        <span class="flex-1 min-w-0">{n.text}</span>
      ) : (
        <input
          class="flex-1 min-w-0 px-2 py-1 text-sm"
          value={draft}
          maxLength={240}
          aria-label={t("Correct: {note}", { note: n.text })}
          autoFocus
          onInput={(e) => setDraft((e.target as HTMLInputElement).value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              save();
            } else if (e.key === "Escape") {
              e.stopPropagation();
              dropped.current = true;
              setDraft(null);
            }
          }}
          onBlur={save}
        />
      )}
      {!!n.uses && draft === null && (
        <span
          class="text-[10px] font-mono text-[var(--t4)] pt-1 whitespace-nowrap"
          title={t("It was in the kitty's prompt for {n} answers", { n: n.uses })}
        >
          ×{n.uses}
        </span>
      )}
      {n.proposed && draft === null && (
        <>
          <span
            class="tag tag-amber"
            title={t("A kitty proposed it: it reaches every kitty once you say yes")}
          >
            {t("proposed")}
          </span>
          <button
            class="btn-outline px-2 py-0.5 text-xs"
            aria-label={t("Accept: {note}", { note: n.text })}
            disabled={busy}
            onClick={() => onSave(n.text)}
          >
            {t("Accept")}
          </button>
        </>
      )}
      {n.pinned && draft === null && (
        <span class="tag" title={t("You wrote it: a consolidation never drops it")}>
          {t("yours")}
        </span>
      )}
      {draft === null && (
        <button
          class="plain text-[var(--t3)] hover:text-[var(--t1)] p-1"
          aria-label={t("Edit: {note}", { note: n.text })}
          disabled={busy}
          onClick={() => {
            dropped.current = false;
            setDraft(n.text);
          }}
        >
          <IconPencil size={12} />
        </button>
      )}
      <button
        class="plain text-[var(--t3)] hover:text-[var(--c-red)] p-1"
        aria-label={t("Forget: {note}", { note: n.text })}
        disabled={busy}
        onClick={onForget}
      >
        <IconTrash size={12} />
      </button>
    </div>
  );
}

// SharedMemory is what every kitty knows, in Settings: read, add, correct and forget.
export function SharedMemory() {
  const [notes, setNotes] = useState<MemoryNote[] | null>(null);
  const [text, setText] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const load = () => {
    api
      .sharedNotes()
      .then(setNotes)
      .catch((e) => setErr((e as Error).message));
  };
  useEffect(load, []);
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
  return (
    <div data-testid="shared-memory">
      <p class="text-sm text-[var(--t3)]">
        {t(
          "What every kitty knows about you: your name, your language, how you like to work. It goes into each kitty's prompt; what one kitty learns alone stays in its own memory, in its drawer.",
        )}
      </p>
      {notes && notes.length === 0 && (
        <p class="text-xs text-[var(--t4)] mt-3">{t("Nothing shared yet.")}</p>
      )}
      <div class="flex flex-col gap-1 mt-3" data-testid="shared-notes">
        {(notes || []).map((n) => (
          <NoteRow
            key={n.id}
            n={n}
            busy={busy}
            onSave={(text) => act(() => api.editShared(n.id, text))}
            onForget={() => act(() => api.forgetShared(n.id))}
          />
        ))}
      </div>
      <div class="flex gap-2 mt-3">
        <input
          class="w-full px-3 py-2 text-sm"
          value={text}
          maxLength={240}
          onInput={(e) => setText((e.target as HTMLInputElement).value)}
          placeholder={t("My name is Ana and I work from Madrid")}
          aria-label={t("A note for every kitty")}
        />
        <button
          class="btn-outline px-3 py-1.5 text-xs whitespace-nowrap"
          disabled={busy || !text.trim()}
          onClick={() =>
            act(async () => {
              await api.rememberShared(text.trim());
              setText("");
            })
          }
        >
          {t("Add note")}
        </button>
      </div>
      {err && <p class="text-xs text-[var(--c-red)] mt-2">{err}</p>}
    </div>
  );
}
