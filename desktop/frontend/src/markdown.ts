// Reading text the way a kitty writes it: no imports, so the tests run it as it is.

export type Inline =
  | string
  | { t: "b" | "c" | "i"; v: string }
  | { t: "a"; v: string; name: string }
  | { t: "l"; v: string; href: string };

// artifactName is the name of the artifact an address points at: the gateway's
// /v1/artifacts/{name} on any host (a kitty writes the address it knows, which inside the desktop
// app is http://wails.localhost/..., a host that serves the page and not the gateway), with or
// without a token, or "" when it is not one.
export function artifactName(href: string): string {
  const m = /^(?:https?:\/\/[^\s/]+)?\/v1\/artifacts\/([^\s/?#)]+)(?:[?#]\S*)?$/.exec(href.trim());
  if (!m) {
    return "";
  }
  try {
    return decodeURIComponent(m[1]);
  } catch {
    return m[1];
  }
}

// inline cuts a line of text into plain text, **bold**, `code`, *italic*, links to artifacts (the
// address of one, or [label](address)) and other http(s) links.
export function inline(text: string): Inline[] {
  const out: Inline[] = [];
  const re =
    /\*\*([^*]+)\*\*|`([^`]+)`|\[([^\]]+)\]\(([^)\s]+)\)|((?:https?:\/\/[^\s<>()]+|\/v1\/artifacts\/[^\s<>()]+))|(?<![\w*])\*([^*\s][^*]*)\*(?![\w*])/g;
  let last = 0;
  const link = (label: string, href: string): Inline => {
    const name = artifactName(href);
    if (name) {
      return { t: "a", v: label, name };
    }
    return /^https?:\/\//.test(href) ? { t: "l", v: label, href } : label;
  };
  for (const m of text.matchAll(re)) {
    if (m.index! > last) {
      out.push(text.slice(last, m.index));
    }
    if (m[1] !== undefined) {
      out.push({ t: "b", v: m[1] });
    } else if (m[2] !== undefined) {
      out.push({ t: "c", v: m[2] });
    } else if (m[3] !== undefined) {
      out.push(link(m[3], m[4]));
    } else if (m[5] !== undefined) {
      // A full stop or a comma after an address is the sentence's, not the address's.
      const href = m[5].replace(/[.,;:!?]+$/, "");
      const name = artifactName(href);
      out.push(link(name || href, href));
      if (href.length < m[5].length) {
        out.push(m[5].slice(href.length));
      }
    } else {
      out.push({ t: "i", v: m[6] });
    }
    last = m.index! + m[0].length;
  }
  if (last < text.length) {
    out.push(text.slice(last));
  }
  return out;
}

export type Block =
  | { t: "h"; level: number; text: string }
  | { t: "p"; text: string }
  | { t: "ul" | "ol"; items: string[] }
  | { t: "code"; text: string }
  | { t: "quote"; text: string }
  | { t: "hr" }
  | { t: "table"; head: string[]; rows: string[][] };

const cells = (line: string) =>
  line
    .trim()
    .replace(/^\||\|$/g, "")
    .split("|")
    .map((c) => c.trim());

// markdown cuts a document into blocks: headings, paragraphs, lists, fenced code, quotes, rules and
// tables. The page draws the blocks itself (no HTML comes out of a document), so a file a kitty
// wrote cannot put markup on the page.
export function markdown(src: string): Block[] {
  const lines = src.replace(/\r\n?/g, "\n").split("\n");
  const out: Block[] = [];
  let para: string[] = [];
  const flush = () => {
    if (para.length) {
      out.push({ t: "p", text: para.join(" ") });
      para = [];
    }
  };
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    let m: RegExpExecArray | null;
    if (/^\s*(```|~~~)/.test(line)) {
      flush();
      const fence = line.trim().slice(0, 3);
      const body: string[] = [];
      for (i++; i < lines.length && !lines[i].trim().startsWith(fence); i++) {
        body.push(lines[i]);
      }
      out.push({ t: "code", text: body.join("\n") });
    } else if (!line.trim()) {
      flush();
    } else if ((m = /^(#{1,6})\s+(.*?)\s*#*$/.exec(line))) {
      flush();
      out.push({ t: "h", level: m[1].length, text: m[2] });
    } else if (/^\s{0,3}([-*_])(\s*\1){2,}\s*$/.test(line)) {
      flush();
      out.push({ t: "hr" });
    } else if (/^\s*>/.test(line)) {
      flush();
      const body: string[] = [];
      for (; i < lines.length && /^\s*>/.test(lines[i]); i++) {
        body.push(lines[i].replace(/^\s*>\s?/, ""));
      }
      i--;
      out.push({ t: "quote", text: body.join(" ") });
    } else if (
      line.includes("|") &&
      /^\s*\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$/.test(lines[i + 1] ?? "")
    ) {
      flush();
      const head = cells(line);
      const rows: string[][] = [];
      for (i += 2; i < lines.length && lines[i].includes("|") && lines[i].trim(); i++) {
        rows.push(cells(lines[i]));
      }
      i--;
      out.push({ t: "table", head, rows });
    } else if (/^\s*([-*+]|\d+[.)])\s+/.test(line)) {
      flush();
      const ordered = /^\s*\d/.test(line);
      const items: string[] = [];
      for (; i < lines.length; i++) {
        const li = /^\s*(?:[-*+]|\d+[.)])\s+(.*)$/.exec(lines[i]);
        if (li) {
          items.push(li[1]);
        } else if (/^\s+\S/.test(lines[i]) && items.length) {
          items[items.length - 1] += " " + lines[i].trim();
        } else {
          break;
        }
      }
      i--;
      out.push({ t: ordered ? "ol" : "ul", items });
    } else {
      para.push(line.trim());
    }
  }
  flush();
  return out;
}
