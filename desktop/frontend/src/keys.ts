// KEYS: what a key the person presses means on the kitty's desktop, in their own layout.

// A key press, as much of a KeyboardEvent as is read here.
export interface KeyPress {
  key: string;
  code?: string;
  ctrlKey: boolean;
  altKey: boolean;
  metaKey: boolean;
  shiftKey: boolean;
  getModifierState?: (key: string) => boolean;
}

// isMac is the person's computer being a Mac, where Option makes characters (Option+2 is @ on a
// Spanish Mac) and Command is the shortcut key.
export const isMac =
  typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform || "");

// layout is what each physical key types with no modifier, when the browser tells (Chromium's
// Keyboard Map); without it letters and digits are known by their code.
let layout: Map<string, string> | null = null;
type KeyboardMap = { getLayoutMap?: () => Promise<Map<string, string>> };
if (typeof navigator !== "undefined") {
  (navigator as Navigator & { keyboard?: KeyboardMap }).keyboard
    ?.getLayoutMap?.()
    .then((m) => (layout = m))
    .catch(() => undefined);
}

// baseOf is what the key at a code types alone, or undefined when it is not known.
export function baseOf(code = "", map: Map<string, string> | null = layout): string | undefined {
  const known = map?.get(code);
  if (known) {
    return known;
  }
  const m = /^(?:Key([A-Z])|Digit(\d))$/.exec(code);
  return m ? (m[1] ?? m[2]).toLowerCase() : undefined;
}

// composed is a character the person made with AltGr (Windows reports it as Control+Alt) or with
// Option on a Mac, such as @ on Spanish (AltGr+2) and Latin American (AltGr+Q) keyboards: the
// modifiers only chose the character, so it is typed as itself. Sent as Ctrl+Alt+@ it would be
// a shortcut on the desktop, and nothing would be written.
export function composed(
  e: KeyPress,
  mac = isMac,
  map: Map<string, string> | null = layout,
): boolean {
  if ([...e.key].length !== 1 || e.key === " ") {
    return false;
  }
  if (e.getModifierState?.("AltGraph")) {
    return true;
  }
  if (!e.ctrlKey && !e.altKey) {
    return false;
  }
  // Windows' AltGr is Control and Alt together; a Mac's is Option alone.
  if (!e.altKey || e.metaKey || (mac ? e.ctrlKey : !e.ctrlKey)) {
    return false;
  }
  const base = baseOf(e.code, map);
  if (base === undefined) {
    return !/^[a-z0-9]$/i.test(e.key);
  }
  return !e.shiftKey && e.key.toLowerCase() !== base.toLowerCase();
}

// NAMED are the X names of the keys the browser names.
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
};

// xKeys turns a key press into what xdotool presses, or "" when it is plain text to type.
export function xKeys(e: KeyPress, mac = isMac, map: Map<string, string> | null = layout): string {
  if (
    ["Control", "Alt", "AltGraph", "Shift", "Meta", "CapsLock", "Dead", "Unidentified"].includes(
      e.key,
    )
  ) {
    return "";
  }
  if (composed(e, mac, map)) {
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
  const key = named || (e.key === " " ? "space" : e.key.length === 1 ? e.key.toLowerCase() : e.key);
  return [...mods, key].join("+");
}

// Modifier is a key the bar holds for the next one: Ctrl, Alt, Shift or Super (Windows key).
export type Modifier = "ctrl" | "alt" | "shift" | "super";

// chord is a key with the modifiers the person holds on the bar, in xdotool's order.
export function chord(keys: string, held: Iterable<Modifier>): string {
  const parts = keys.split("+");
  const key = parts.pop()!;
  const mods = new Set<string>([...parts, ...held]);
  return [...(["ctrl", "alt", "shift", "super"] as const).filter((m) => mods.has(m)), key].join(
    "+",
  );
}
