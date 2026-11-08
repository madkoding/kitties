import { useEffect, useState } from "preact/hooks";

// The person's display preferences, kept in this browser: whether to see technical details
// (coordinates, commands, tokens, errors as the services wrote them), and whether a task's actions
// show one by one instead of folded under their count.

const TECH = "kitties.tech";
const STEPS = "kitties.steps";
const listeners = new Set<() => void>();

function read(key: string): string {
  try {
    return localStorage.getItem(key) || "";
  } catch {
    return "";
  }
}

let tech = read(TECH) === "1";
let steps = read(STEPS) === "1";

function save(key: string, on: boolean): void {
  try {
    localStorage.setItem(key, on ? "1" : "");
  } catch {
    /* kept for this visit only */
  }
  listeners.forEach((l) => l());
}

// usePref is a preference for a component, which draws again when it changes.
function usePref(get: () => boolean): boolean {
  const [on, setOn] = useState(get());
  useEffect(() => {
    const l = () => setOn(get());
    listeners.add(l);
    return () => {
      listeners.delete(l);
    };
  }, []);
  return on;
}

// techDetails reports whether the technical details are shown.
export function techDetails(): boolean {
  return tech;
}

// setTechDetails shows or hides them, everywhere at once.
export function setTechDetails(on: boolean): void {
  tech = on;
  save(TECH, on);
}

// useTechDetails is techDetails for a component, which draws again when it changes.
export function useTechDetails(): boolean {
  return usePref(() => tech);
}

// setAllSteps has every action of a task show one by one (on), or folded under its count (off,
// the default: the person follows the kitty's own lines).
export function setAllSteps(on: boolean): void {
  steps = on;
  save(STEPS, on);
}

// useAllSteps is whether a task's actions show unfolded, for a component.
export function useAllSteps(): boolean {
  return usePref(() => steps);
}
