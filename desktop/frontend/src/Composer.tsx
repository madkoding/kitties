import { useState } from "preact/hooks";
import { IconSend, IconStop } from "./icons";

interface Props {
  disabled: boolean;
  working: boolean;
  group: boolean;
  onSend: (text: string) => void;
  onCancel: () => void;
}

export default function Composer(p: Props) {
  const [text, setText] = useState("");
  const submit = (e: Event) => {
    e.preventDefault();
    const t = text.trim();
    if (!t || p.disabled) {
      return;
    }
    setText("");
    p.onSend(t);
  };
  return (
    <form class="frosted flex gap-2 px-3 py-2.5 sm:px-5 sm:py-3 border-t border-white/5 flex-none z-10 relative" onSubmit={submit}>
      <div class="well flex-1 flex items-center min-h-[44px] max-h-[120px] p-2 rounded-2xl focus-within:border-accent">
        <textarea
          class="flex-1 min-h-[28px] max-h-[100px] px-1 py-1 bg-transparent border-0 outline-hidden text-[var(--t1)] resize-none font-sans text-[14px] leading-relaxed disabled:opacity-50"
          placeholder={
            p.working
              ? "A kitty is working on its desktop…"
              : p.group
                ? "Say something to the team; @name picks who answers"
                : "Say something…"
          }
          value={text}
          disabled={p.disabled}
          rows={1}
          onInput={(e) => setText((e.target as HTMLTextAreaElement).value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey) {
              submit(e);
            }
          }}
        />
      </div>
      {p.working ? (
        <button type="button" class="btn-danger min-h-[44px] min-w-[44px] px-4 flex items-center gap-2 text-sm" onClick={p.onCancel}>
          <IconStop size={14} /> Stop
        </button>
      ) : (
        <button type="submit" class="btn-accent min-h-[44px] min-w-[44px] px-5 flex items-center justify-center" disabled={p.disabled || !text.trim()} aria-label="Send">
          <IconSend size={16} />
        </button>
      )}
    </form>
  );
}
