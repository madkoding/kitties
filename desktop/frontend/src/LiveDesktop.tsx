import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import { api, type Kitty, shotData } from "./api";
import { plainError } from "./friendly";
import { t } from "./i18n";
import { IconX } from "./icons";

// LIVE DESKTOP: the kitty's display as it is now, refreshed while it is open. The person can
// take it over: their clicks, drags, the wheel and the keyboard go to the display, like on a
// remote screen, and the kitty's own actions wait until they give it back. Nothing is opened
// on the container: every frame and every input goes through the gateway's API.

// How often a frame is asked for: faster while the person has the controls.
const WATCH_EVERY = 1000;
const CONTROL_EVERY = 350;

// keyName is the X name of a key the browser names, or "" for one that is typed as text.
const NAMED: Record<string, string> = {
  Enter: "Return",
  Backspace: "BackSpace",
  Tab: "Tab",
  Escape: "Escape",
  Delete: "Delete",
  Insert: "Insert",
  Home: "Home",
  End: "End",
  PageUp: "Prior",
  PageDown: "Next",
  ArrowUp: "Up",
  ArrowDown: "Down",
  ArrowLeft: "Left",
  ArrowRight: "Right",
  " ": "space",
};

// xKeys turns a key press into what xdotool presses, or "" when it is plain text to type.
export function xKeys(e: {
  key: string;
  ctrlKey: boolean;
  altKey: boolean;
  metaKey: boolean;
  shiftKey: boolean;
}): string {
  if (["Control", "Alt", "Shift", "Meta", "CapsLock", "Dead", "Unidentified"].includes(e.key)) {
    return "";
  }
  const named = NAMED[e.key] || (/^F\d{1,2}$/.test(e.key) ? e.key : "");
  const chord = e.ctrlKey || e.altKey || e.metaKey;
  if (!named && !chord) {
    return "";
  }
  const mods = [
    e.ctrlKey && "ctrl",
    e.altKey && "alt",
    e.metaKey && "super",
    e.shiftKey && named && "shift",
  ].filter(Boolean);
  const key = named || (e.key.length === 1 ? e.key.toLowerCase() : e.key);
  return [...mods, key].join("+");
}

// loadFrame fetches one frame and resolves once it can be shown without a blank.
async function loadFrame(path: string): Promise<string> {
  const src = await shotData(path);
  if (src.startsWith("data:")) {
    return src;
  }
  await new Promise<void>((done, fail) => {
    const img = new Image();
    img.onload = () => done();
    img.onerror = () => fail(new Error(t("The desktop did not answer")));
    img.src = src;
  });
  return src;
}

const QUICK: [string, string][] = [
  ["Enter", "Return"],
  ["Tab", "Tab"],
  ["Esc", "Escape"],
  ["⌫", "BackSpace"],
  ["Ctrl+C", "ctrl+c"],
  ["Ctrl+A", "ctrl+a"],
  ["Alt+Tab", "alt+Tab"],
  ["Alt+F4", "alt+F4"],
];

export default function LiveDesktop({ kitty, onClose }: { kitty: Kitty; onClose: () => void }) {
  const [frame, setFrame] = useState("");
  const [control, setControl] = useState(false);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const img = useRef<HTMLImageElement>(null);
  const surface = useRef<HTMLDivElement>(null);
  const controlRef = useRef(false);
  controlRef.current = control;
  const down = useRef<{ x: number; y: number; button: number } | null>(null);
  const pendingClick = useRef<number | undefined>(undefined);
  const typed = useRef("");
  const typeTimer = useRef<number | undefined>(undefined);
  const wheelAt = useRef(0);

  // The frames: one after another, never two at once.
  useEffect(() => {
    let alive = true;
    let n = 0;
    (async () => {
      while (alive) {
        try {
          const src = await loadFrame(`/v1/kitties/${kitty.id}/screenshot?live=${n++}`);
          if (alive) {
            setFrame(src);
            setErr((e) => (e === t("The desktop did not answer") ? "" : e));
          }
        } catch (e) {
          if (alive) {
            setErr(t("The desktop did not answer"));
          }
        }
        await new Promise((r) => setTimeout(r, controlRef.current ? CONTROL_EVERY : WATCH_EVERY));
      }
    })();
    return () => {
      alive = false;
    };
  }, [kitty.id]);

  // Closing gives the display back, so the kitty never waits on a closed window.
  useEffect(
    () => () => {
      if (controlRef.current) {
        api.controlKitty(kitty.id, false).catch(() => undefined);
      }
    },
    [kitty.id],
  );

  // Escape closes, unless the person has the controls: then it is a key of the display.
  useLayoutEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && !controlRef.current && onClose();
    addEventListener("keydown", onKey);
    return () => removeEventListener("keydown", onKey);
  }, [onClose]);

  const send = (action: string, args: Record<string, unknown>) => {
    api.inputKitty(kitty.id, action, args).catch((e) => setErr(plainError((e as Error).message)));
  };

  const take = async (on: boolean) => {
    setBusy(true);
    setErr("");
    try {
      setControl(await api.controlKitty(kitty.id, on));
      if (on) {
        setTimeout(() => surface.current?.focus(), 0);
      }
    } catch (e) {
      setErr(plainError((e as Error).message));
    } finally {
      setBusy(false);
    }
  };

  // point is where an event falls on the display, in its own pixels.
  const point = (e: MouseEvent) => {
    const el = img.current!;
    const r = el.getBoundingClientRect();
    const w = el.naturalWidth || 1280;
    const h = el.naturalHeight || 800;
    return {
      x: Math.max(0, Math.min(w - 1, Math.round(((e.clientX - r.left) * w) / r.width))),
      y: Math.max(0, Math.min(h - 1, Math.round(((e.clientY - r.top) * h) / r.height))),
    };
  };

  const flushTyped = () => {
    clearTimeout(typeTimer.current);
    if (typed.current) {
      send("type", { text: typed.current });
      typed.current = "";
    }
  };

  const onMouseDown = (e: MouseEvent) => {
    if (!control) {
      return;
    }
    e.preventDefault();
    surface.current?.focus();
    down.current = { ...point(e), button: e.button };
  };

  const onMouseUp = (e: MouseEvent) => {
    const from = down.current;
    down.current = null;
    if (!control || !from) {
      return;
    }
    e.preventDefault();
    flushTyped();
    const to = point(e);
    if (Math.abs(to.x - from.x) > 4 || Math.abs(to.y - from.y) > 4) {
      send("drag", { x: from.x, y: from.y, to_x: to.x, to_y: to.y });
      return;
    }
    const button = ["left", "middle", "right"][from.button] || "left";
    if (button !== "left") {
      send("click", { ...to, button });
      return;
    }
    // A second click soon after is a double click: the first waits a moment to see.
    if (pendingClick.current !== undefined) {
      clearTimeout(pendingClick.current);
      pendingClick.current = undefined;
      send("double_click", to);
      return;
    }
    pendingClick.current = window.setTimeout(() => {
      pendingClick.current = undefined;
      send("click", to);
    }, 250);
  };

  const onWheel = (e: WheelEvent) => {
    if (!control) {
      return;
    }
    e.preventDefault();
    const now = Date.now();
    if (now - wheelAt.current < 120) {
      return;
    }
    wheelAt.current = now;
    send("move", point(e));
    send("scroll", { direction: e.deltaY < 0 ? "up" : "down", amount: 2 });
  };

  const onKeyDown = (e: KeyboardEvent) => {
    if (!control) {
      return;
    }
    // Ctrl+V (Cmd+V) pastes the person's own clipboard: the paste event types it.
    if ((e.ctrlKey || e.metaKey) && !e.altKey && e.key.toLowerCase() === "v") {
      return;
    }
    const keys = xKeys(e);
    if (keys) {
      e.preventDefault();
      flushTyped();
      send("key", { keys });
      return;
    }
    if (e.key.length === 1) {
      e.preventDefault();
      typed.current += e.key;
      clearTimeout(typeTimer.current);
      typeTimer.current = window.setTimeout(flushTyped, 150);
    }
  };

  const onPaste = (e: ClipboardEvent) => {
    const text = e.clipboardData?.getData("text") || "";
    if (control && text) {
      e.preventDefault();
      flushTyped();
      send("type", { text: text.slice(0, 2000) });
    }
  };

  return (
    <div
      class="fixed inset-0 z-50 bg-black/80 flex items-center justify-center p-3 sm:p-6 backdrop"
      onClick={onClose}
      data-modal
      data-testid="live-desktop"
    >
      <div
        class="chat-modal w-full max-w-6xl p-3 sm:p-4 max-h-[96vh] flex flex-col gap-3"
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-label={t("{name}'s desktop", { name: kitty.name })}
      >
        <div class="flex items-center gap-2 flex-wrap">
          <h2 class="font-mono text-base tracking-wide flex-1 min-w-0 truncate">
            {t("{name}'s desktop", { name: kitty.name })}
          </h2>
          <span class={`state-pill ${control ? "is-working" : "is-ok"}`} data-testid="live-state">
            {control
              ? t("You have the controls: {name} waits", { name: kitty.name })
              : t("Watching live")}
          </span>
          {control ? (
            <button
              class="btn-accent px-3 py-1.5 text-sm"
              onClick={() => take(false)}
              disabled={busy}
              data-testid="give-back"
            >
              {t("Give it back to {name}", { name: kitty.name })}
            </button>
          ) : (
            <button
              class="btn-accent px-3 py-1.5 text-sm"
              onClick={() => take(true)}
              disabled={busy || !frame}
              data-testid="take-control"
            >
              {t("Take control")}
            </button>
          )}
          <button
            class="plain p-1 text-[var(--t3)] hover:text-[var(--t1)]"
            onClick={onClose}
            aria-label={t("Close")}
          >
            <IconX />
          </button>
        </div>
        <div
          ref={surface}
          tabIndex={0}
          class={`live-surface relative min-h-0 flex-1 flex items-center justify-center outline-none ${control ? "is-control" : ""}`}
          onKeyDown={onKeyDown}
          onPaste={onPaste}
          onContextMenu={(e) => control && e.preventDefault()}
        >
          {frame ? (
            <img
              ref={img}
              src={frame}
              alt={t("{name}'s desktop", { name: kitty.name })}
              class="max-w-full max-h-[70vh] select-none"
              draggable={false}
              onMouseDown={onMouseDown}
              onMouseUp={onMouseUp}
              onWheel={onWheel}
              data-testid="live-frame"
            />
          ) : (
            <div class="text-sm text-[var(--t3)] py-20">
              <span class="spinner" /> {t("Connecting to the desktop…")}
            </div>
          )}
        </div>
        {control && (
          <div class="flex gap-1.5 flex-wrap items-center" data-testid="quick-keys">
            {QUICK.map(([label, keys]) => (
              <button
                key={keys}
                class="btn-ghost px-2 py-1 text-xs font-mono"
                onClick={() => {
                  flushTyped();
                  send("key", { keys });
                  surface.current?.focus();
                }}
              >
                {label}
              </button>
            ))}
            <span class="text-xs text-[var(--t3)] ml-auto">
              {t("Click, drag, scroll and type on the picture; Ctrl+V pastes your own text.")}
            </span>
          </div>
        )}
        {!control && (
          <p class="text-xs text-[var(--t3)]">
            {t(
              "Take control to click and type in it yourself, for what {name} cannot do alone (a password, a captcha). {name} waits meanwhile.",
              { name: kitty.name },
            )}
          </p>
        )}
        {err && <p class="text-sm text-[var(--c-red)]">{err}</p>}
      </div>
    </div>
  );
}
