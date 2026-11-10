import type { JSX } from "preact";
import { useState } from "preact/hooks";
import { t } from "./i18n";
import {
  IconActivity,
  IconArrowDown,
  IconArrowLeft,
  IconArrowRight,
  IconArrowUp,
  IconAt,
  IconBackspace,
  IconCloseWindow,
  IconCopyFrom,
  IconEnter,
  IconGrid,
  IconKeyboard,
  IconPasteTo,
  IconShift,
  IconSwitch,
  IconTab,
  IconUndo,
} from "./icons";
import { chord, type Modifier } from "./keys";

// KEY BAR: the keys a person cannot press on the kitty's desktop from their own computer or
// phone (Ctrl+Alt+Delete, Alt+Tab, Super, the F keys, a phone's missing Esc and Tab), the
// characters that are hard to type in another layout (@, #, \, |…), and copy and paste between
// their clipboard and the kitty's. Ctrl, Alt, Shift and Super stay held for the next key.

type Icon = (p: { size?: number }) => JSX.Element;

// A key of the bar: what it shows, what it is called, and the xdotool keys it presses.
interface BarKey {
  label: string;
  name: string;
  keys: string;
  icon?: Icon;
}

const MAIN: BarKey[] = [
  { label: "Esc", name: "Escape", keys: "Escape" },
  { label: "Tab", name: "Tab", keys: "Tab", icon: IconTab },
  { label: "Enter", name: "Enter", keys: "Return", icon: IconEnter },
  { label: "", name: "Backspace", keys: "BackSpace", icon: IconBackspace },
  { label: "Alt+Tab", name: "Alt+Tab", keys: "alt+Tab", icon: IconSwitch },
  { label: "Ctrl+Alt+Del", name: "Ctrl+Alt+Delete", keys: "ctrl+alt+Delete", icon: IconActivity },
];

const MORE: BarKey[] = [
  ...Array.from({ length: 12 }, (_, i) => ({
    label: `F${i + 1}`,
    name: `F${i + 1}`,
    keys: `F${i + 1}`,
  })),
  { label: "", name: "Up", keys: "Up", icon: IconArrowUp },
  { label: "", name: "Down", keys: "Down", icon: IconArrowDown },
  { label: "", name: "Left", keys: "Left", icon: IconArrowLeft },
  { label: "", name: "Right", keys: "Right", icon: IconArrowRight },
  { label: "Home", name: "Home", keys: "Home" },
  { label: "End", name: "End", keys: "End" },
  { label: "PgUp", name: "Page Up", keys: "Prior" },
  { label: "PgDn", name: "Page Down", keys: "Next" },
  { label: "Del", name: "Delete", keys: "Delete" },
  { label: "Ins", name: "Insert", keys: "Insert" },
  { label: "Ctrl+Z", name: "Ctrl+Z", keys: "ctrl+z", icon: IconUndo },
  { label: "Ctrl+A", name: "Ctrl+A", keys: "ctrl+a" },
  { label: "Ctrl+X", name: "Ctrl+X", keys: "ctrl+x" },
  { label: "Ctrl+S", name: "Ctrl+S", keys: "ctrl+s" },
  { label: "Ctrl+F", name: "Ctrl+F", keys: "ctrl+f" },
  { label: "Ctrl+L", name: "Ctrl+L", keys: "ctrl+l" },
  { label: "Alt+F4", name: "Alt+F4", keys: "alt+F4", icon: IconCloseWindow },
  { label: "PrtSc", name: "Print Screen", keys: "Print" },
];

// SYMBOLS are typed as text, whatever the person's layout and the desktop's.
const SYMBOLS = [..."@#$%&|\\/~^`'\"{}[]<>€£¿¡ñÑáéíóúüç"];

const MODS: { mod: Modifier; label: string; icon?: Icon }[] = [
  { mod: "ctrl", label: "Ctrl" },
  { mod: "alt", label: "Alt" },
  { mod: "shift", label: "Shift", icon: IconShift },
  { mod: "super", label: "Super" },
];

const BTN = "key-btn btn-ghost px-2 py-1 text-xs font-mono flex items-center gap-1 shrink-0";

export default function KeyBar({
  onKeys,
  onText,
  onCopy,
  onPaste,
  hint,
}: {
  // onKeys presses a key or a combination (xdotool's names), onText types text.
  onKeys: (keys: string) => void;
  onText: (text: string) => void;
  // onCopy brings the kitty's clipboard to the person's; onPaste the other way, and pastes it.
  onCopy: () => void;
  onPaste: () => void;
  hint?: string;
}) {
  const [held, setHeld] = useState<Modifier[]>([]);
  const [more, setMore] = useState(false);

  const press = (keys: string) => {
    onKeys(chord(keys, held));
    setHeld([]);
  };
  const toggle = (mod: Modifier) =>
    setHeld((h) => (h.includes(mod) ? h.filter((m) => m !== mod) : [...h, mod]));
  // A character with a modifier held is a shortcut (Ctrl+@ is not an @): only plain letters
  // take one, the rest is typed.
  const symbol = (ch: string) => {
    if (held.length && /^[a-z]$/i.test(ch)) {
      press(ch.toLowerCase());
      return;
    }
    onText(ch);
    setHeld([]);
  };

  const key = (k: BarKey) => (
    <button
      key={k.keys}
      class={BTN}
      onClick={() => press(k.keys)}
      aria-label={k.name}
      title={k.name}
    >
      {k.icon && <k.icon size={14} />}
      {k.label && <span>{k.label}</span>}
    </button>
  );

  return (
    <div class="key-bar flex flex-col gap-1.5 shrink-0" data-testid="quick-keys">
      <div class="key-row flex gap-1.5 items-center overflow-x-auto">
        {MAIN.map(key)}
        <button
          class={BTN}
          onClick={() => press("alt+F1")}
          aria-label={t("Apps menu")}
          title={t("Apps menu (Alt+F1)")}
        >
          <IconGrid size={14} />
        </button>
        <span class="key-sep" aria-hidden="true" />
        <button
          class={BTN}
          onClick={onCopy}
          aria-label={t("Copy")}
          title={t("Copy what is selected on the desktop to your clipboard")}
          data-testid="key-copy"
        >
          <IconCopyFrom size={14} />
          <span class="font-sans">{t("Copy")}</span>
        </button>
        <button
          class={BTN}
          onClick={onPaste}
          aria-label={t("Paste")}
          title={t("Paste your clipboard on the desktop")}
          data-testid="key-paste"
        >
          <IconPasteTo size={14} />
          <span class="font-sans">{t("Paste")}</span>
        </button>
        <button
          class={BTN}
          onClick={() => symbol("@")}
          aria-label="@"
          title={t("Type {ch}", { ch: "@" })}
        >
          <IconAt size={14} />
        </button>
        <span class="key-sep" aria-hidden="true" />
        {MODS.map((m) => (
          <button
            key={m.mod}
            class={`${BTN} ${held.includes(m.mod) ? "is-held" : ""}`}
            onClick={() => toggle(m.mod)}
            aria-pressed={held.includes(m.mod)}
            aria-label={m.label}
            title={t("Hold {key} for the next key", { key: m.label })}
            data-testid={`key-mod-${m.mod}`}
          >
            {m.icon && <m.icon size={14} />}
            <span>{m.label}</span>
          </button>
        ))}
        <button
          class={`${BTN} ${more ? "is-held" : ""}`}
          onClick={() => setMore((v) => !v)}
          aria-expanded={more}
          aria-label={t("More keys")}
          title={t("More keys")}
          data-testid="key-more"
        >
          <IconKeyboard size={14} />
        </button>
      </div>
      {more && (
        <>
          <div
            class="key-row flex gap-1.5 items-center overflow-x-auto"
            data-testid="key-more-keys"
          >
            {MORE.map(key)}
          </div>
          <div class="key-row flex gap-1 items-center overflow-x-auto" data-testid="key-symbols">
            {SYMBOLS.map((ch) => (
              <button
                key={ch}
                class={`${BTN} min-w-[1.75rem] justify-center`}
                onClick={() => symbol(ch)}
                aria-label={ch}
                title={t("Type {ch}", { ch })}
              >
                {ch}
              </button>
            ))}
          </div>
        </>
      )}
      {hint && <p class="text-xs text-[var(--t3)]">{hint}</p>}
    </div>
  );
}
