import type { Delegation, Kitty } from "./api";
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
              d.result && (
                <div class="text text-md">
                  <Markdown text={gatewayText(d.result)} onOpen={onOpen} chat />
                </div>
              )
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
