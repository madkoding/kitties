// Small helpers for what the page shows.

export function when(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) {
    return "";
  }
  const now = new Date();
  const sameDay = d.toDateString() === now.toDateString();
  const time = d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  return sameDay ? time : d.toLocaleDateString([], { month: "short", day: "numeric" }) + " " + time;
}

// stepArgs renders the arguments of a step in a few words.
export function stepArgs(action: string, args?: Record<string, unknown>): string {
  if (!args) {
    return "";
  }
  switch (action) {
    case "click":
    case "double_click":
    case "move":
      return `${args.x ?? "?"}, ${args.y ?? "?"}${args.button ? " " + args.button : ""}`;
    case "type":
      return JSON.stringify(args.text ?? "");
    case "key":
      return String(args.keys ?? args.key ?? "");
    case "scroll":
      return `${args.direction ?? "down"} ×${args.amount ?? 3}`;
    case "open":
      return String(args.url ?? "");
    case "run":
      return Array.isArray(args.argv) ? args.argv.join(" ") : String(args.command ?? "");
    case "wait":
      return `${args.seconds ?? 1}s`;
    case "done":
      return "";
    default:
      return Object.keys(args).length ? JSON.stringify(args) : "";
  }
}

// stepSentence says a desktop step the way a person would: no coordinates, no commands. The
// technical form (stepArgs) stays for whoever hovers it.
export function stepSentence(action: string, args?: Record<string, unknown>): string {
  const a = args || {};
  switch (action) {
    case "open": {
      const url = String(a.url ?? "");
      let host = url;
      try {
        host = new URL(url).hostname.replace(/^www\./, "");
      } catch {
        /* not an address: said as it is */
      }
      return host ? `Opened ${host}` : "Opened a page";
    }
    case "click":
      return "Clicked on the screen";
    case "double_click":
      return "Double-clicked on the screen";
    case "move":
      return "Moved the pointer";
    case "type": {
      const text = String(a.text ?? "");
      return text ? `Typed “${text.length > 40 ? text.slice(0, 40) + "…" : text}”` : "Typed";
    }
    case "key":
      return `Pressed ${keyName(String(a.keys ?? a.key ?? ""))}`;
    case "scroll":
      return a.direction === "up" ? "Scrolled up" : "Scrolled down";
    case "wait":
      return "Waited a moment";
    case "screenshot":
      return "Looked at the screen";
    case "run":
      return "Ran a program";
    case "done":
      return "Finished";
  }
  const words = action.replace(/_/g, " ");
  return words.charAt(0).toUpperCase() + words.slice(1);
}

// keyName says a key combination as it reads on a keyboard: Return is Enter, ctrl+l is Ctrl+L.
function keyName(keys: string): string {
  if (!keys) {
    return "a key";
  }
  return keys
    .split("+")
    .map((k) => {
      const low = k.trim().toLowerCase();
      if (low === "return") {
        return "Enter";
      }
      if (low === "ctrl" || low === "control") {
        return "Ctrl";
      }
      return low.length === 1 ? low.toUpperCase() : low.charAt(0).toUpperCase() + low.slice(1);
    })
    .join("+");
}

// inline turns **bold** and `code` into elements; everything else stays text.
export function inline(text: string): (string | { t: "b" | "c"; v: string })[] {
  const out: (string | { t: "b" | "c"; v: string })[] = [];
  const re = /\*\*([^*]+)\*\*|`([^`]+)`/g;
  let last = 0;
  for (const m of text.matchAll(re)) {
    if (m.index! > last) {
      out.push(text.slice(last, m.index));
    }
    out.push(m[1] !== undefined ? { t: "b", v: m[1] } : { t: "c", v: m[2] });
    last = m.index! + m[0].length;
  }
  if (last < text.length) {
    out.push(text.slice(last));
  }
  return out;
}
