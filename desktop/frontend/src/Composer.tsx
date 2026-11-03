import { useEffect, useRef, useState } from "preact/hooks";
import { IconPaperclip, IconSend, IconStop } from "./icons";
import { t } from "./i18n";
import { checkFiles, Picked } from "./Attachments";

interface Props {
  disabled: boolean;
  working: boolean;
  group: boolean;
  onSend: (text: string, files: File[]) => void;
  onCancel: () => void;
}

export default function Composer(p: Props) {
  const [text, setText] = useState("");
  const [files, setFiles] = useState<File[]>([]);
  const [error, setError] = useState("");
  const [dragging, setDragging] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  // The latest list, for the page-wide drop handler that is set up once.
  const have = useRef(files);
  have.current = files;
  const locked = useRef(p.disabled);
  locked.current = p.disabled;

  const add = (picked: File[]) => {
    if (picked.length === 0 || locked.current) {
      return;
    }
    const r = checkFiles(have.current, picked);
    have.current = r.files;
    setFiles(r.files);
    setError(r.error);
  };

  // A file dropped anywhere on the page joins the message, and the browser does not open it.
  useEffect(() => {
    const hasFiles = (e: DragEvent) => Array.from(e.dataTransfer?.types || []).includes("Files");
    let depth = 0;
    const enter = (e: DragEvent) => {
      if (hasFiles(e)) {
        e.preventDefault();
        depth++;
        setDragging(true);
      }
    };
    const over = (e: DragEvent) => hasFiles(e) && e.preventDefault();
    const leave = (e: DragEvent) => {
      if (hasFiles(e) && --depth <= 0) {
        depth = 0;
        setDragging(false);
      }
    };
    const drop = (e: DragEvent) => {
      if (!hasFiles(e)) {
        return;
      }
      e.preventDefault();
      depth = 0;
      setDragging(false);
      add(Array.from(e.dataTransfer?.files || []));
    };
    window.addEventListener("dragenter", enter);
    window.addEventListener("dragover", over);
    window.addEventListener("dragleave", leave);
    window.addEventListener("drop", drop);
    return () => {
      window.removeEventListener("dragenter", enter);
      window.removeEventListener("dragover", over);
      window.removeEventListener("dragleave", leave);
      window.removeEventListener("drop", drop);
    };
  }, []);

  const submit = (e: Event) => {
    e.preventDefault();
    const msg = text.trim();
    if ((!msg && files.length === 0) || p.disabled) {
      return;
    }
    setText("");
    setFiles([]);
    setError("");
    p.onSend(msg, files);
  };
  return (
    <form
      class={`frosted flex flex-wrap gap-2 px-3 py-2.5 sm:px-5 sm:py-3 border-t border-white/5 flex-none z-10 relative ${dragging ? "composer-drop" : ""}`}
      onSubmit={submit}
      data-testid="composer"
    >
      {dragging && <div class="drop-hint">{t("Drop the files to attach them")}</div>}
      {(files.length > 0 || error) && (
        <div class="w-full flex flex-wrap items-center gap-2" data-testid="picked-files">
          {files.map((f, i) => (
            <Picked
              key={f.name + i + f.size}
              file={f}
              onRemove={() => {
                setFiles(files.filter((_, j) => j !== i));
                setError("");
              }}
            />
          ))}
          {error && (
            <span class="text-[var(--c-red)] text-xs" role="alert" data-testid="attach-error">
              {error}
            </span>
          )}
        </div>
      )}
      <input
        ref={input}
        type="file"
        multiple
        class="hidden"
        data-testid="file-input"
        onChange={(e) => {
          const el = e.target as HTMLInputElement;
          add(Array.from(el.files || []));
          el.value = "";
        }}
      />
      <button
        type="button"
        class="btn-ghost min-h-[44px] min-w-[44px] flex items-center justify-center"
        onClick={() => input.current?.click()}
        disabled={p.disabled}
        aria-label={t("Attach files")}
        title={t("Attach files")}
        data-testid="attach"
      >
        <IconPaperclip size={16} />
      </button>
      <div class="well flex-1 flex items-center min-h-[44px] max-h-[7.5rem] p-2 rounded-2xl focus-within:border-accent">
        <textarea
          class="flex-1 min-h-[1.75rem] max-h-[6.25rem] px-1 py-1 bg-transparent border-0 outline-hidden text-[var(--t1)] resize-none font-sans text-[0.875rem] leading-relaxed read-only:opacity-50"
          placeholder={
            p.working
              ? t("A kitty is working on its desktop…")
              : p.group
                ? t("Say something to the team; @name picks who answers")
                : t("Say something…")
          }
          value={text}
          // readOnly, not disabled: a disabled field drops the focus (and the caret) the moment a
          // kitty starts working or the poll flips the state, and never gets it back.
          readOnly={p.disabled}
          aria-disabled={p.disabled}
          rows={1}
          onInput={(e) => setText((e.target as HTMLTextAreaElement).value)}
          onPaste={(e) => {
            // A picture or file on the clipboard is attached; pasted text is typed as always.
            const pasted = Array.from(e.clipboardData?.files || []);
            if (pasted.length > 0) {
              e.preventDefault();
              add(pasted);
            }
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey) {
              submit(e);
            }
          }}
        />
      </div>
      {p.working ? (
        <button
          type="button"
          class="btn-danger min-h-[44px] min-w-[44px] px-4 flex items-center gap-2 text-sm"
          onClick={p.onCancel}
        >
          <IconStop size={14} /> {t("Stop")}
        </button>
      ) : (
        <button
          type="submit"
          class="btn-accent min-h-[44px] min-w-[44px] px-5 flex items-center justify-center"
          disabled={p.disabled || (!text.trim() && files.length === 0)}
          aria-label={t("Send")}
        >
          <IconSend size={16} />
        </button>
      )}
    </form>
  );
}
