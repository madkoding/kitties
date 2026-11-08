import { useLayoutEffect, useRef } from "preact/hooks";
import { t } from "./i18n";
import { IconX } from "./icons";

// Modal is the window over the page: a title, a close button, Escape and a click outside close it.
// A window that must not be closed now (an update being made) says so with closable={false}.
export function Modal({
  title,
  onClose,
  children,
  wide,
  closable = true,
}: {
  title: string;
  onClose: () => void;
  children: preact.ComponentChildren;
  wide?: boolean;
  closable?: boolean;
}) {
  const dialog = useRef<HTMLDivElement>(null);
  const close = () => closable && onClose();
  // The focus lives in the window while it is open, and goes back to where it was when it closes.
  useLayoutEffect(() => {
    const before = document.activeElement as HTMLElement | null;
    const box = dialog.current;
    if (box && !box.contains(document.activeElement)) {
      box.focus({ preventScroll: true });
    }
    return () => {
      if (before?.isConnected) {
        before.focus({ preventScroll: true });
      }
    };
  }, []);
  // Tab cycles inside the window, so a key never moves the focus to the page behind it.
  const trap = (e: KeyboardEvent) => {
    if (e.key !== "Tab" || !dialog.current) {
      return;
    }
    const items = [
      ...dialog.current.querySelectorAll<HTMLElement>(
        'a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])',
      ),
    ];
    if (!items.length) {
      return;
    }
    const first = items[0];
    const last = items[items.length - 1];
    const at = document.activeElement;
    if (e.shiftKey && (at === first || at === dialog.current)) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && at === last) {
      e.preventDefault();
      first.focus();
    }
  };
  useLayoutEffect(() => {
    // A window inside this one that used the key (the live desktop) keeps this one open.
    const onKey = (e: KeyboardEvent) =>
      e.key === "Escape" && !e.defaultPrevented && closable && onClose();
    addEventListener("keydown", onKey);
    return () => removeEventListener("keydown", onKey);
  }, [onClose, closable]);
  return (
    <div
      class="fixed inset-0 z-40 bg-black/60 flex items-center justify-center p-4 backdrop"
      onClick={close}
      data-modal
    >
      <div
        class={`chat-modal w-full ${wide ? "max-w-3xl" : "max-w-md"} p-5 max-h-[90vh] overflow-y-auto scroll-thin`}
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-modal="true"
        tabIndex={-1}
        ref={dialog}
        onKeyDown={trap}
      >
        <div class="flex items-center justify-between mb-4">
          <h2 class="font-mono text-base tracking-wide">{title}</h2>
          {closable && (
            <button
              class="plain p-1 text-[var(--t3)] hover:text-[var(--t1)]"
              onClick={onClose}
              aria-label={t("Close")}
            >
              <IconX />
            </button>
          )}
        </div>
        {children}
      </div>
    </div>
  );
}
