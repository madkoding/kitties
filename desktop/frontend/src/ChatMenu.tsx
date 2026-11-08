import { createPortal } from "preact/compat";
import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import { ApiError, api, errText, type Session } from "./api";
import { IconDots, IconPencil, IconSparkles, IconText, IconTrash } from "./icons";
import { Modal } from "./Modal";
import { t } from "./i18n";
import { when } from "./format";

// byLine says who wrote a description: the person, or the kitty whose proposal was taken.
function byLine(by?: string): string {
  if (!by || by === "user") {
    return t("by you");
  }
  return t("proposed by {name}", { name: by });
}

// The ⋯ menu of a chat, in its sidebar row and in the header: rename it, change its description
// (what the chat is for, which the kitties read on every turn) or delete it.

// suggest opens the editor and has a kitty write both from the chat at once.
export type ChatField = "title" | "description" | "suggest";

// The most a title and a description hold, as the gateway keeps them (chat.MaxTitle and
// chat.MaxDescription).
export const MAX_TITLE = 120;
export const MAX_DESCRIPTION = 500;

export function ChatMenu({
  chat,
  onEdit,
  onDelete,
  where,
}: {
  chat: Session;
  onEdit: (c: Session, field: ChatField) => void;
  onDelete: (c: Session) => void;
  // where says which menu it is, for the tests: the sidebar row's or the header's.
  where: "sidebar" | "header";
}) {
  const [open, setOpen] = useState(false);
  const [at, setAt] = useState<{ top: number; right: number; up: boolean } | null>(null);
  const button = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLDivElement>(null);

  // The list is drawn on the body and placed against the window, not inside the row: the sidebar
  // scrolls and its blur would hold a fixed list inside it. It opens upwards when there is no room
  // below.
  const place = () => {
    const r = button.current?.getBoundingClientRect();
    if (!r) {
      return;
    }
    const up = innerHeight - r.bottom < 160 && r.top > 160;
    setAt({ top: up ? r.top - 4 : r.bottom + 4, right: Math.max(8, innerWidth - r.right), up });
  };
  useLayoutEffect(() => {
    if (!open) {
      return;
    }
    place();
    menu.current?.querySelector<HTMLElement>("[role=menuitem]")?.focus({ preventScroll: true });
  }, [open]);
  useEffect(() => {
    if (!open) {
      return;
    }
    const away = (e: Event) => {
      const n = e.target as Node;
      if (!menu.current?.contains(n) && !button.current?.contains(n)) {
        setOpen(false);
      }
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        setOpen(false);
        button.current?.focus();
        return;
      }
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        const items = [...(menu.current?.querySelectorAll<HTMLElement>("[role=menuitem]") || [])];
        const i = items.indexOf(document.activeElement as HTMLElement);
        const next =
          e.key === "ArrowDown" ? (i + 1) % items.length : (i - 1 + items.length) % items.length;
        items[next]?.focus();
      }
    };
    const close = () => setOpen(false);
    addEventListener("pointerdown", away, true);
    addEventListener("keydown", onKey, true);
    addEventListener("resize", close);
    addEventListener("scroll", close, true);
    return () => {
      removeEventListener("pointerdown", away, true);
      removeEventListener("keydown", onKey, true);
      removeEventListener("resize", close);
      removeEventListener("scroll", close, true);
    };
  }, [open]);

  const pick = (fn: () => void) => () => {
    setOpen(false);
    fn();
  };
  return (
    <>
      <button
        ref={button}
        class={`plain chat-menu-btn ${open ? "is-open" : ""}`}
        onClick={(e) => {
          e.stopPropagation();
          setOpen(!open);
        }}
        aria-label={t("Options of {title}", { title: chat.title })}
        title={t("Chat options")}
        aria-haspopup="menu"
        aria-expanded={open}
        data-testid={`chat-menu-${where}`}
      >
        <IconDots size={16} />
      </button>
      {open &&
        createPortal(
          <div
            ref={menu}
            class="chat-menu"
            role="menu"
            aria-label={t("Chat options")}
            style={
              at
                ? `right:${at.right}px;${at.up ? `bottom:${innerHeight - at.top}px` : `top:${at.top}px`}`
                : "visibility:hidden"
            }
            data-testid="chat-menu"
          >
            <button role="menuitem" class="plain" onClick={pick(() => onEdit(chat, "title"))}>
              <IconPencil size={14} /> {t("Rename")}
            </button>
            <button role="menuitem" class="plain" onClick={pick(() => onEdit(chat, "description"))}>
              <IconText size={14} />{" "}
              {chat.description ? t("Edit description") : t("Add a description")}
            </button>
            <button role="menuitem" class="plain" onClick={pick(() => onEdit(chat, "suggest"))}>
              <IconSparkles size={14} /> {t("Write them from the chat")}
            </button>
            <div class="chat-menu-sep" role="separator" />
            <button role="menuitem" class="plain is-danger" onClick={pick(() => onDelete(chat))}>
              <IconTrash size={14} /> {t("Delete chat")}
            </button>
          </div>,
          document.body,
        )}
    </>
  );
}

// ChatEditModal changes a chat's title and description; focus says which field the person asked
// for. onSaved gets the chat as the gateway keeps it.
export function ChatEditModal({
  chat,
  focus,
  onClose,
  onSaved,
}: {
  chat: Session;
  focus: ChatField;
  onClose: () => void;
  onSaved: (c: Session) => void;
}) {
  const [title, setTitle] = useState(chat.title);
  const [description, setDescription] = useState(chat.description || "");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  // reading is while a kitty reads the chat to write the title and the description.
  const [reading, setReading] = useState(false);
  const [written, setWritten] = useState(false);
  const first = useRef<HTMLInputElement>(null);
  const second = useRef<HTMLTextAreaElement>(null);
  const suggest = async () => {
    setErr("");
    setReading(true);
    try {
      const { suggestion } = await api.suggestChat(chat.id);
      setTitle(suggestion.title);
      setDescription(suggestion.description);
      setWritten(true);
      second.current?.focus();
    } catch (e) {
      setErr(
        e instanceof ApiError && e.status === 409
          ? t("There is nothing to read in this chat yet: write to it first.")
          : errText(e),
      );
    } finally {
      setReading(false);
    }
  };
  useLayoutEffect(() => {
    const el = focus === "title" ? first.current : second.current;
    el?.focus();
    el?.select();
    if (focus === "suggest") {
      suggest();
    }
  }, []);
  const changed = title.trim() !== chat.title || description.trim() !== (chat.description || "");
  const save = async () => {
    if (!title.trim()) {
      setErr(t("A chat needs a title."));
      return;
    }
    if (!changed) {
      onClose();
      return;
    }
    setBusy(true);
    try {
      const body: { title?: string; description?: string } = {};
      if (title.trim() !== chat.title) {
        body.title = title;
      }
      if (description.trim() !== (chat.description || "")) {
        body.description = description;
      }
      onSaved(await api.editChat(chat.id, body));
    } catch (e) {
      setErr(errText(e));
      setBusy(false);
    }
  };
  return (
    <Modal
      title={
        focus === "title"
          ? t("Rename the chat")
          : focus === "suggest"
            ? t("Title and description")
            : t("Chat description")
      }
      onClose={onClose}
    >
      <form
        onSubmit={(e) => {
          e.preventDefault();
          save();
        }}
        data-testid="chat-edit"
      >
        <label
          class="block text-xs text-[var(--t3)] mb-1 mt-1 uppercase tracking-wider font-mono"
          for="chat-title"
        >
          {t("Title")}
        </label>
        <input
          id="chat-title"
          ref={first}
          class="w-full px-3 py-2 text-sm"
          value={title}
          maxLength={MAX_TITLE}
          disabled={reading}
          onInput={(e) => setTitle((e.target as HTMLInputElement).value)}
          data-testid="chat-edit-title"
        />
        <label
          class="block text-xs text-[var(--t3)] mb-1 mt-4 uppercase tracking-wider font-mono"
          for="chat-description"
        >
          {t("Description")}
        </label>
        <textarea
          id="chat-description"
          ref={second}
          class="w-full px-3 py-2 text-sm min-h-[6.5rem] resize-y"
          value={description}
          maxLength={MAX_DESCRIPTION}
          disabled={reading}
          placeholder={t(
            "What this chat is for, e.g. Plan the trip to Chiloé in March: budget, ferry and lodging.",
          )}
          onInput={(e) => setDescription((e.target as HTMLTextAreaElement).value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
              e.preventDefault();
              save();
            }
          }}
          data-testid="chat-edit-description"
        />
        <p class="text-xs text-[var(--t4)] mt-1 flex justify-between gap-3">
          <span>
            {t(
              "The kitties of this chat read it before every answer: it is the base of what they do here. Change it and they work on the new one from the next message.",
            )}
          </span>
          <span class="font-mono flex-none">
            {description.length}/{MAX_DESCRIPTION}
          </span>
        </p>
        {(chat.descriptions?.length ?? 0) > 0 && (
          <details class="chat-history mt-3" data-testid="chat-edit-history">
            <summary class="text-xs text-[var(--t3)] cursor-pointer">
              {t("Earlier descriptions ({count})", { count: chat.descriptions!.length })}
            </summary>
            <ul class="mt-2 flex flex-col gap-1.5 max-h-48 overflow-y-auto scroll-thin">
              {chat.descriptions!.map((d, i) => (
                <li key={i} class="chat-history-item" data-testid="chat-edit-version">
                  <span class="flex-1 min-w-0">
                    <span class="block text-xs text-[var(--t2)]">{d.text}</span>
                    <span class="block text-[0.625rem] text-[var(--t4)] font-mono">
                      {byLine(d.by)}
                      {d.at && !d.at.startsWith("0001") ? ` · ${when(d.at)}` : ""}
                    </span>
                  </span>
                  <button
                    type="button"
                    class="btn-ghost px-2 py-1 text-xs flex-none"
                    onClick={() => {
                      setDescription(d.text);
                      second.current?.focus();
                    }}
                    disabled={reading}
                  >
                    {t("Use this one")}
                  </button>
                </li>
              ))}
            </ul>
          </details>
        )}
        {written && !reading && (
          <p class="text-xs text-[var(--t3)] mt-2" role="status" data-testid="chat-edit-written">
            {t("Written from the chat: check it, change what you want and save.")}
          </p>
        )}
        {err && <p class="text-sm text-[var(--c-red)] mt-2">{err}</p>}
        <div class="flex justify-end gap-2 mt-5 flex-wrap">
          <button
            type="button"
            class="btn-ghost px-3 py-2 text-sm flex items-center gap-1.5 mr-auto"
            onClick={suggest}
            disabled={reading || busy}
            title={t(
              "A kitty of this chat reads all of it and writes a title and a description of what it is for now",
            )}
            data-testid="chat-edit-suggest"
          >
            {reading ? <span class="spinner" /> : <IconSparkles size={14} />}
            {reading ? t("Reading the chat…") : t("Write them from the chat")}
          </button>
          <button type="button" class="btn-ghost px-4 py-2 text-sm" onClick={onClose}>
            {t("Cancel")}
          </button>
          <button
            type="submit"
            class="btn-accent px-4 py-2 text-sm"
            disabled={busy || reading || !title.trim()}
          >
            {t("Save")}
          </button>
        </div>
      </form>
    </Modal>
  );
}
