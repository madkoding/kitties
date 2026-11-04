import { useEffect, useRef, useState } from "preact/hooks";
import type { Kitty, Session } from "./api";
import Composer from "./Composer";
import Conversation from "./Conversation";
import { t } from "./i18n";
import { IconChat } from "./icons";

// LIVE CHAT: the kitty's chat as a sidebar of its live desktop window. It is the same chat as the
// page's (the same turns, steps, attachments and composer), so what is written here and what the
// kitty does on the display read in one place, and nothing is kept twice.

export interface LiveChatProps {
  session: Session;
  kitties: Kitty[];
  user: string;
  pending: string | null;
  waiting: boolean;
  // busy is a message on its way or a kitty at work: the composer waits.
  busy: boolean;
  onSend: (text: string, files: File[]) => void;
  onCancel: () => void;
}

const FOLDED = "kitties.desktop.chat.folded";
const wasFolded = (): boolean => {
  try {
    return localStorage.getItem(FOLDED) === "1";
  } catch {
    return false;
  }
};

export default function LiveChat(p: LiveChatProps & { kitty: Kitty }) {
  // The phone starts with the chat folded: the picture has the room until it is asked for.
  const [folded, setFoldedNow] = useState(
    () =>
      wasFolded() || (typeof matchMedia === "function" && matchMedia("(max-width: 767px)").matches),
  );
  const setFolded = (on: boolean) => {
    setFoldedNow(on);
    try {
      localStorage.setItem(FOLDED, on ? "1" : "");
    } catch {
      /* kept for this visit only */
    }
  };
  // What the kitty said while the chat was folded shows as a dot on the bar until it is opened.
  const seen = useRef(p.session.turns?.length ?? 0);
  const count = p.session.turns?.length ?? 0;
  const [unread, setUnread] = useState(false);
  useEffect(() => {
    if (folded && count > seen.current) {
      setUnread(true);
    }
    if (!folded) {
      setUnread(false);
    }
    seen.current = count;
  }, [count, folded]);
  return (
    <aside
      class={`live-chat ${folded ? "is-folded" : ""}`}
      aria-label={t("Chat with {name}", { name: p.kitty.name })}
      data-testid="live-chat"
    >
      <button
        class="plain flex items-center gap-2 px-3 py-2 text-sm text-[var(--t2)] w-full"
        aria-expanded={!folded}
        onClick={() => setFolded(!folded)}
        title={folded ? t("Show the chat") : t("Hide the chat")}
        data-testid="live-chat-toggle"
      >
        <IconChat size={14} />
        <span class="flex-1 text-left truncate">{t("Chat")}</span>
        {unread && (
          <span
            class="live-dot"
            role="status"
            aria-label={t("New message")}
            data-testid="live-chat-unread"
          />
        )}
        <span aria-hidden="true">{folded ? "‹" : "›"}</span>
      </button>
      {!folded && (
        <>
          <Conversation
            session={p.session}
            kitties={p.kitties}
            user={p.user}
            pending={p.pending}
            waiting={p.waiting}
            onSend={(text) => p.onSend(text, [])}
            sidebar
          />
          <Composer
            disabled={p.busy}
            working={p.busy}
            group={p.session.members.length > 1}
            onSend={p.onSend}
            onCancel={p.onCancel}
          />
        </>
      )}
    </aside>
  );
}
