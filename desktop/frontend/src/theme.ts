// The colour mode, saved in localStorage and applied to <html> as data attributes. The palette is
// the Motita design system (themes.css); public/theme-init.js applies the saved choice before the
// first paint.
export type Mode = "system" | "light" | "dark";
const KEY = "kitties.ui.theme";

export function savedMode(): Mode {
  const q = new URLSearchParams(location.search).get("theme");
  if (q === "light" || q === "dark") {
    return q;
  }
  try {
    const raw = JSON.parse(localStorage.getItem(KEY) || "{}");
    return raw.mode === "light" || raw.mode === "dark" ? raw.mode : "system";
  } catch {
    return "system";
  }
}

export function applyMode(mode: Mode): void {
  const resolved =
    mode === "system"
      ? matchMedia("(prefers-color-scheme: light)").matches
        ? "light"
        : "dark"
      : mode;
  const root = document.documentElement;
  root.dataset.mode = resolved;
  root.dataset.scheme = "motita";
  root.style.colorScheme = resolved;
  try {
    localStorage.setItem(KEY, JSON.stringify({ mode }));
  } catch {
    /* private window */
  }
}

export function cycleMode(): Mode {
  const next: Record<Mode, Mode> = { system: "dark", dark: "light", light: "system" };
  const mode = next[savedMode()];
  applyMode(mode);
  return mode;
}
