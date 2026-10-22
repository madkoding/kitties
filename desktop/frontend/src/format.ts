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
