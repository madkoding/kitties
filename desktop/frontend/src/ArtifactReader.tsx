import { useEffect, useState } from "preact/hooks";
import { artifactText, fileText, shotData, shotURL } from "./api";
import { previewOf } from "./library";
import { headings, highlight, inline, markdown, type Inline } from "./markdown";
import { t } from "./i18n";
import { Modal } from "./Modal";

// Inlines draws a line of text: bold, italic, code and links. A link to an artifact opens it in the
// reader (onOpen) instead of leaving the page for an address that only the gateway serves.
export function Inlines({ text, onOpen }: { text: string; onOpen?: (name: string) => void }) {
  return (
    <>
      {inline(text).map((part: Inline, i) => {
        if (typeof part === "string") {
          return part;
        }
        switch (part.t) {
          case "b":
            return <strong key={i}>{part.v}</strong>;
          case "i":
            return <em key={i}>{part.v}</em>;
          case "c":
            return <code key={i}>{part.v}</code>;
          case "a":
            return (
              <button
                key={i}
                class="plain artifact-link"
                data-testid="artifact-link"
                onClick={() => onOpen?.(part.name)}
              >
                {part.v}
              </button>
            );
          default:
            return (
              <a key={i} href={part.href} target="_blank" rel="noopener noreferrer">
                {part.v}
              </a>
            );
        }
      })}
    </>
  );
}

// Code draws a code block with the colours of its language: each token is a span of text.
export function Code({ text, lang }: { text: string; lang: string }) {
  return (
    <code>
      {highlight(text, lang).map((tok, i) =>
        tok.k ? (
          <span key={i} class={`hl-${tok.k}`}>
            {tok.v}
          </span>
        ) : (
          tok.v
        ),
      )}
    </code>
  );
}

// artifactLink is the page's own address for an artifact: #artifact/<name> opens it in the reader.
export function artifactLink(name: string): string {
  return `${location.origin}${location.pathname}${location.search}#artifact/${encodeURIComponent(name)}`;
}

// Markdown draws a document the page cut into blocks, never as HTML.
export function Markdown({
  text,
  onOpen,
  chat,
}: {
  text: string;
  onOpen?: (name: string) => void;
  // chat is a message: its line breaks stay, and it sits in the bubble without outer margins.
  chat?: boolean;
}) {
  const blocks = markdown(text, chat);
  // A document gets anchors on its headings, and a table of contents once it has three or more;
  // a chat message has neither (the same anchor in two messages would clash).
  const heads = chat ? [] : headings(blocks);
  let h = 0;
  return (
    <div class={chat ? "md md-chat" : "md"} data-testid="markdown">
      {heads.length >= 3 && (
        <nav class="md-toc" data-testid="md-toc" aria-label={t("Contents")}>
          <div class="md-toc-title">{t("Contents")}</div>
          <ul>
            {heads.map((hd) => (
              <li key={hd.id} style={{ paddingLeft: `${(hd.level - 1) * 0.75}rem` }}>
                <button
                  class="plain artifact-link"
                  onClick={() =>
                    document
                      .getElementById(`md-${hd.id}`)
                      ?.scrollIntoView({ behavior: "smooth", block: "start" })
                  }
                >
                  {hd.text.replace(/[*`_]/g, "")}
                </button>
              </li>
            ))}
          </ul>
        </nav>
      )}
      {blocks.map((b, i) => {
        switch (b.t) {
          case "h": {
            const H = `h${Math.min(b.level + 1, 6)}` as "h2";
            const id = heads[h++]?.id;
            return (
              <H key={i} id={id ? `md-${id}` : undefined}>
                <Inlines text={b.text} onOpen={onOpen} />
              </H>
            );
          }
          case "ul":
          case "ol": {
            const L = b.t;
            return (
              <L key={i}>
                {b.items.map((it, j) => (
                  <li key={j}>
                    <Inlines text={it} onOpen={onOpen} />
                  </li>
                ))}
              </L>
            );
          }
          case "code":
            return (
              <pre key={i} data-lang={b.lang || undefined}>
                <Code text={b.text} lang={b.lang} />
              </pre>
            );
          case "quote":
            return (
              <blockquote key={i}>
                <Inlines text={b.text} onOpen={onOpen} />
              </blockquote>
            );
          case "hr":
            return <hr key={i} />;
          case "table":
            return (
              <table key={i}>
                <thead>
                  <tr>
                    {b.head.map((c, j) => (
                      <th key={j}>
                        <Inlines text={c} onOpen={onOpen} />
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {b.rows.map((r, j) => (
                    <tr key={j}>
                      {r.map((c, k) => (
                        <td key={k}>
                          <Inlines text={c} onOpen={onOpen} />
                        </td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            );
          default:
            return (
              <p key={i}>
                <Inlines text={b.text} onOpen={onOpen} />
              </p>
            );
        }
      })}
    </div>
  );
}

// ArtifactReader is the window that reads an artifact of the library: a markdown file drawn as a
// document, a picture as a picture, anything else as text. It loads the file through the same
// bridge the rest of the page uses, so it also works inside the desktop app.
//
// With a path, it reads a file the gateway serves elsewhere (a kitty's Shared folder): a picture
// is shown, a text read, and anything else (a PDF, an office document) offered for download.
export default function ArtifactReader({
  name,
  path,
  onClose,
}: {
  name: string;
  path?: string;
  onClose: () => void;
}) {
  const [current, setCurrent] = useState(name);
  const [text, setText] = useState<string | null>(null);
  const [picture, setPicture] = useState("");
  const [err, setErr] = useState("");
  useEffect(() => setCurrent(name), [name]);
  const look = path ? previewOf(name) : "text";
  // Escape closes the reader only, not the window it was opened from (the library): it is taken
  // before the windows under it see it.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !e.defaultPrevented) {
        e.preventDefault();
        onClose();
      }
    };
    addEventListener("keydown", onKey, true);
    return () => removeEventListener("keydown", onKey, true);
  }, [onClose]);
  useEffect(() => {
    let alive = true;
    setText(null);
    setPicture("");
    setErr("");
    if (path && look === "image") {
      shotData(path, typeOf(name))
        .then((src) => alive && setPicture(src))
        .catch(() => alive && setErr(t("This file is not there any more.")));
    } else if (path && look === "none") {
      setErr(t("There is no preview for this kind of file: download it."));
    } else {
      (path ? fileText(path) : artifactText(current))
        .then((x) => alive && setText(x))
        .catch(
          () =>
            alive &&
            setErr(
              path ? t("This file is not there any more.") : t("This file is not in the library."),
            ),
        );
    }
    return () => {
      alive = false;
    };
  }, [current, path]);
  const [raw, setRaw] = useState(false);
  // copied says which of the two copies just went to the clipboard: the text or the link.
  const [copied, setCopied] = useState<"" | "text" | "link">("");
  const copy = (what: "text" | "link", value: string) =>
    navigator.clipboard
      ?.writeText(value)
      .then(() => {
        setCopied(what);
        setTimeout(() => setCopied(""), 1500);
      })
      .catch(() => undefined);
  const lower = current.toLowerCase();
  const isMd = lower.endsWith(".md") || lower.endsWith(".markdown");
  return (
    <Modal title={current} onClose={onClose} wide>
      <div data-testid="artifact-reader">
        {err && <div class="callout callout-warn">{err}</div>}
        {text === null && !picture && !err && (
          <div class="text-sm text-[var(--t3)]">{t("Loading…")}</div>
        )}
        {picture && (
          <img
            class="max-h-[70vh] max-w-full mx-auto"
            src={picture}
            alt={current}
            data-testid="shared-picture"
          />
        )}
        {text !== null &&
          (isMd && !raw ? (
            <Markdown text={text} onOpen={setCurrent} />
          ) : lower.endsWith(".svg") && !raw ? (
            <img
              class="max-h-96 bg-white"
              src={"data:image/svg+xml;utf8," + encodeURIComponent(text)}
              alt={current}
            />
          ) : (
            <pre class="skill-body">{text}</pre>
          ))}
        <div class="mt-3 flex justify-end gap-1">
          {text !== null && (isMd || lower.endsWith(".svg")) && (
            <button
              class="btn-ghost px-2 py-1 text-xs"
              data-testid="artifact-raw"
              aria-pressed={raw}
              onClick={() => setRaw(!raw)}
            >
              {raw ? t("formatted") : t("source")}
            </button>
          )}
          {text !== null && (
            <button
              class="btn-ghost px-2 py-1 text-xs"
              data-testid="artifact-copy"
              onClick={() => copy("text", text)}
            >
              {copied === "text" ? t("copied") : t("copy")}
            </button>
          )}
          {!path && (
            <button
              class="btn-ghost px-2 py-1 text-xs"
              data-testid="artifact-share"
              title={t("Copy a link that opens this file in Kitties")}
              onClick={() => copy("link", artifactLink(current))}
            >
              {copied === "link" ? t("link copied") : t("copy link")}
            </button>
          )}
          <a
            class="btn-ghost px-2 py-1 text-xs"
            href={shotURL(
              path ? `${path}?download=1` : `/v1/artifacts/${encodeURIComponent(current)}`,
            )}
            download={current}
            target="_blank"
          >
            {t("download")}
          </a>
        </div>
      </div>
    </Modal>
  );
}

function typeOf(name: string): string {
  const ext = name.toLowerCase().split(".").pop() || "";
  return ext === "jpg" ? "image/jpeg" : `image/${ext}`;
}
