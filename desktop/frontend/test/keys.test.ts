import assert from "node:assert/strict";
import { test } from "node:test";
import { baseOf, chord, composed, xKeys } from "../src/keys.ts";

const press = (key: string, code: string, mods: Partial<Record<string, boolean>> = {}) => ({
  key,
  code,
  ctrlKey: !!mods.ctrl,
  altKey: !!mods.alt,
  metaKey: !!mods.meta,
  shiftKey: !!mods.shift,
  getModifierState: (k: string) => k === "AltGraph" && !!mods.altgr,
});

test("AltGr characters are typed as themselves, not sent as Ctrl+Alt shortcuts", () => {
  // Spanish (AltGr+2) and Latin American (AltGr+Q) @ on Windows: Control and Alt are reported.
  assert.equal(xKeys(press("@", "Digit2", { ctrl: true, alt: true }), false, null), "");
  assert.equal(xKeys(press("@", "KeyQ", { ctrl: true, alt: true }), false, null), "");
  assert.equal(xKeys(press("€", "KeyE", { ctrl: true, alt: true }), false, null), "");
  assert.equal(xKeys(press("\\", "Backquote", { ctrl: true, alt: true }), false, null), "");
  // Linux and browsers that tell AltGr apart.
  assert.equal(composed(press("@", "Digit2", { altgr: true }), false, null), true);
  // On a Mac, Option makes the character (Option+2 is @ on a Spanish Mac).
  assert.equal(xKeys(press("@", "Digit2", { alt: true }), true, null), "");
  // With the person's layout known, a key's own character stays a shortcut.
  const map = new Map([
    ["Digit2", "2"],
    ["KeyQ", "q"],
    ["Semicolon", "ñ"],
  ]);
  assert.equal(composed(press("@", "KeyQ", { ctrl: true, alt: true }), false, map), true);
  assert.equal(composed(press("ñ", "Semicolon", { ctrl: true, alt: true }), false, map), false);
});

test("shortcuts stay shortcuts", () => {
  assert.equal(xKeys(press("t", "KeyT", { ctrl: true, alt: true }), false, null), "ctrl+alt+t");
  assert.equal(xKeys(press("c", "KeyC", { ctrl: true }), false, null), "ctrl+c");
  assert.equal(xKeys(press("Tab", "Tab", { alt: true }), false, null), "alt+Tab");
  assert.equal(xKeys(press("F4", "F4", { alt: true }), false, null), "alt+F4");
  assert.equal(
    xKeys(press("Delete", "Delete", { ctrl: true, alt: true }), false, null),
    "ctrl+alt+Delete",
  );
  // Alt alone is not AltGr off a Mac: Alt+f opens a menu.
  assert.equal(xKeys(press("f", "KeyF", { alt: true }), false, null), "alt+f");
  // Plain text is typed; modifiers alone send nothing.
  assert.equal(xKeys(press("a", "KeyA"), false, null), "");
  assert.equal(xKeys(press("AltGraph", "AltRight", { ctrl: true, alt: true }), false, null), "");
  assert.equal(xKeys(press("Enter", "Enter", { shift: true }), false, null), "shift+Return");
});

test("baseOf knows letters and digits without a layout map", () => {
  assert.equal(baseOf("KeyQ", null), "q");
  assert.equal(baseOf("Digit2", null), "2");
  assert.equal(baseOf("Backquote", null), undefined);
});

test("chord adds the modifiers held on the bar, once and in order", () => {
  assert.equal(chord("Tab", ["alt"]), "alt+Tab");
  assert.equal(chord("ctrl+c", ["shift", "ctrl"]), "ctrl+shift+c");
  assert.equal(chord("Delete", []), "Delete");
  assert.equal(chord("F4", ["super", "alt"]), "alt+super+F4");
});
