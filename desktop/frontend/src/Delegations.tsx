import { useState } from "preact/hooks";
import type { Delegation, Helper, Kitty } from "./api";
import { Markdown } from "./ArtifactReader";
import { gatewayText } from "./gatewayText";
import { t } from "./i18n";
import PixelCat from "./PixelCat";

// THE KITTIES A TURN HANDED WORK TO.
//
// When a kitty delegates, the other kitty takes part in the chat: its answer shows right under the
// turn that asked, with its own name and cat, as in a group chat. The whole exchange stays in the
// thread between the two, a click away.
export default function Delegations({
  delegations,
  from,
  kitties,
  onOpen,
}: {
  delegations: Delegation[];
  // from is the kitty that handed the work over.
  from: string;
  kitties: Map<string, Kitty>;
  onOpen?: (name: string) => void;
}) {
  return (
    <>
      {delegations.map((d, i) => {
        const k = kitties.get(d.kitty_id);
        return (
          <div
            key={i}
            class={`msg agent delegate-reply ${d.error ? "failed" : ""}`}
            data-who={d.kitty}
            data-testid="delegate-reply"
          >
            <span class="msg-cat">
              <PixelCat
                breed={k?.avatar?.breed}
                color={k?.avatar?.color}
                mood={d.error ? "error" : "happy"}
                size={32}
              />
            </span>
            <div class="delegate-task" title={[d.task, d.limits].filter(Boolean).join("\n")}>
              {t("{from} asked: {task}", { from, task: d.task })}
            </div>
            {d.error ? (
              <div class="text">{t("Could not do it: {error}", { error: d.error })}</div>
            ) : (
              d.result && <Answer d={d} onOpen={onOpen} />
            )}
            <a class="delegate-open" href={`#chat/${d.chat}`}>
              {t("Open the thread between {from} and {name}", { from, name: d.kitty })}
            </a>
          </div>
        );
      })}
    </>
  );
}

// Answer is a delegate's answer: the start of it, and all of it on request when it was long.
function Answer({ d, onOpen }: { d: Delegation; onOpen?: (name: string) => void }) {
  const [all, setAll] = useState(false);
  const text = all && d.answer ? d.answer : d.result || "";
  return (
    <>
      <div class="text text-md" data-testid="delegate-answer">
        <Markdown text={gatewayText(text)} onOpen={onOpen} chat />
      </div>
      {d.answer && (
        <button
          class="plain delegate-more"
          onClick={() => setAll(!all)}
          aria-expanded={all}
          data-testid="delegate-more"
        >
          {all ? t("Show less") : t("Show the whole answer")}
        </button>
      )}
    </>
  );
}

// Helping shows the kitties at work right now on a subtask handed over from the chat, with
// their cat working, until they answer.
export function Helping({ helping, kitties }: { helping: Helper[]; kitties: Map<string, Kitty> }) {
  return (
    <>
      {helping.map((h) => {
        const k = kitties.get(h.kitty_id);
        return (
          <div
            key={h.kitty_id + h.chat}
            class="msg agent delegate-reply working"
            data-who={h.kitty}
            data-testid="delegate-working"
          >
            <span class="msg-cat">
              <PixelCat
                breed={k?.avatar?.breed}
                color={k?.avatar?.color}
                mood="working"
                size={32}
              />
            </span>
            <div class="delegate-task" title={h.task}>
              {t("{from} asked: {task}", { from: h.from, task: h.task })}
            </div>
            <div class="working-line !mt-0">
              <span class="spinner" /> {t("{name} is working on it…", { name: h.kitty })}
            </div>
            <a class="delegate-open" href={`#chat/${h.chat}`}>
              {t("Follow it in the thread between {from} and {name}", {
                from: h.from,
                name: h.kitty,
              })}
            </a>
          </div>
        );
      })}
    </>
  );
}
