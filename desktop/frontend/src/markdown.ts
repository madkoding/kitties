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
  | { t: "code"; text: string; lang: string }
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
//
// breaks keeps the line breaks inside a paragraph, the way a chat message reads; a document joins
// them, the way markdown does.
export function markdown(src: string, breaks = false): Block[] {
  const lines = src.replace(/\r\n?/g, "\n").split("\n");
  const out: Block[] = [];
  let para: string[] = [];
  const flush = () => {
    if (para.length) {
      out.push({ t: "p", text: para.join(breaks ? "\n" : " ") });
      para = [];
    }
  };
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    let m: RegExpExecArray | null;
    if (/^\s*(```|~~~)/.test(line)) {
      flush();
      const fence = line.trim().slice(0, 3);
      const lang = line.trim().slice(3).trim().split(/\s+/)[0].toLowerCase();
      const body: string[] = [];
      for (i++; i < lines.length && !lines[i].trim().startsWith(fence); i++) {
        body.push(lines[i]);
      }
      out.push({ t: "code", text: body.join("\n"), lang });
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

// Token is a piece of code, with what it is for its colour: a keyword, a string, a comment, a
// number, or "" for the rest.
export interface Token {
  k: "" | "kw" | "str" | "com" | "num";
  v: string;
}

const KEYWORDS = new Set(
  (
    "break case catch class const continue def default defer do elif else enum except export " +
    "extends false finally fn for func function go if import in interface let match module new " +
    "nil none null package pass private pub public raise return select self static struct super " +
    "switch this throw true try type typeof use var void while with yield async await from as " +
    "lambda not and or is true false True False None SELECT FROM WHERE INSERT UPDATE DELETE " +
    "INTO VALUES JOIN ON GROUP BY ORDER LIMIT AND OR NOT NULL CREATE TABLE"
  ).split(" "),
);
const HASH = new Set([
  "py",
  "python",
  "sh",
  "bash",
  "shell",
  "zsh",
  "yaml",
  "yml",
  "toml",
  "rb",
  "ruby",
  "r",
  "dockerfile",
  "makefile",
  "conf",
  "ini",
]);
const DASH = new Set(["sql", "lua", "haskell", "hs"]);

// highlight cuts a code block into tokens for its colours, from the language of its fence. A
// block with no language is left as it is. The tokens are text the page draws itself: nothing
// in the code becomes markup.
export function highlight(code: string, lang: string): Token[] {
  if (!lang) {
    return code ? [{ k: "", v: code }] : [];
  }
  const line = HASH.has(lang)
    ? "#[^\\n]*"
    : DASH.has(lang)
      ? "--[^\\n]*"
      : "\\/\\/[^\\n]*|\\/\\*[\\s\\S]*?(?:\\*\\/|$)";
  const re = new RegExp(
    `(${line})|("(?:[^"\\\\\\n]|\\\\.)*"?|'(?:[^'\\\\\\n]|\\\\.)*'?|\`[^\`]*\`?)|(\\b\\d[\\d_]*(?:\\.\\d+)?\\b)|([A-Za-z_][\\w]*)`,
    "g",
  );
  const out: Token[] = [];
  let last = 0;
  const push = (k: Token["k"], v: string) => {
    const prev = out[out.length - 1];
    if (prev && prev.k === k) {
      prev.v += v;
    } else {
      out.push({ k, v });
    }
  };
  for (const m of code.matchAll(re)) {
    if (m.index! > last) {
      push("", code.slice(last, m.index));
    }
    if (m[1] !== undefined) {
      push("com", m[1]);
    } else if (m[2] !== undefined) {
      push("str", m[2]);
    } else if (m[3] !== undefined) {
      push("num", m[3]);
    } else {
      push(KEYWORDS.has(m[4]) ? "kw" : "", m[4]);
    }
    last = m.index! + m[0].length;
  }
  if (last < code.length) {
    push("", code.slice(last));
  }
  return out;
}

// slug is a heading's anchor: lower case, words joined by dashes.
export function slug(text: string): string {
  return (
    text
      .toLowerCase()
      .replace(/[`*_[\]()]/g, "")
      .replace(/[^\p{L}\p{N}]+/gu, "-")
      .replace(/^-+|-+$/g, "") || "section"
  );
}

export interface Heading {
  level: number;
  text: string;
  id: string;
}

// headings lists a document's headings with an anchor each, the same one twice made unique, for
// its table of contents.
export function headings(blocks: Block[]): Heading[] {
  const seen = new Map<string, number>();
  const out: Heading[] = [];
  for (const b of blocks) {
    if (b.t !== "h") {
      continue;
    }
    const base = slug(b.text);
    const n = (seen.get(base) || 0) + 1;
    seen.set(base, n);
    out.push({ level: b.level, text: b.text, id: n > 1 ? `${base}-${n}` : base });
  }
  return out;
}
