import { useEffect, useState } from "preact/hooks";
import { artifactText, shotURL } from "./api";
import { inline, markdown, type Inline } from "./markdown";
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
  return (
    <div class={chat ? "md md-chat" : "md"} data-testid="markdown">
      {markdown(text, chat).map((b, i) => {
        switch (b.t) {
          case "h": {
            const H = `h${Math.min(b.level + 1, 6)}` as "h2";
            return (
              <H key={i}>
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
            return <pre key={i}>{b.text}</pre>;
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
export default function ArtifactReader({ name, onClose }: { name: string; onClose: () => void }) {
  const [current, setCurrent] = useState(name);
  const [text, setText] = useState<string | null>(null);
  const [err, setErr] = useState("");
  useEffect(() => setCurrent(name), [name]);
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
    setErr("");
    artifactText(current)
      .then((x) => alive && setText(x))
      .catch(() => alive && setErr(t("This file is not in the library.")));
    return () => {
      alive = false;
    };
  }, [current]);
  const [raw, setRaw] = useState(false);
  const [copied, setCopied] = useState(false);
  const copy = () =>
    text !== null &&
    navigator.clipboard
      ?.writeText(text)
      .then(() => {
        setCopied(true);
        setTimeout(() => setCopied(false), 1500);
      })
      .catch(() => undefined);
  const lower = current.toLowerCase();
  const isMd = lower.endsWith(".md") || lower.endsWith(".markdown");
  return (
    <Modal title={current} onClose={onClose} wide>
      <div data-testid="artifact-reader">
        {err && <div class="callout callout-warn">{err}</div>}
        {text === null && !err && <div class="text-sm text-[var(--t3)]">{t("Loading…")}</div>}
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
            <button class="btn-ghost px-2 py-1 text-xs" data-testid="artifact-copy" onClick={copy}>
              {copied ? t("copied") : t("copy")}
            </button>
          )}
          <a
            class="btn-ghost px-2 py-1 text-xs"
            href={shotURL(`/v1/artifacts/${encodeURIComponent(current)}`)}
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
