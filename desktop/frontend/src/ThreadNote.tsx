import { useState } from "preact/hooks";
import type { Goal, Kitty, Session } from "./api";
import { api, errText } from "./api";
import { when } from "./format";
import { t } from "./i18n";
import { IconClock, IconUsers } from "./icons";

// WHAT A THREAD THAT IS NO CHAT IS.
//
// A scheduled task's thread is its history of runs, and a hand-over's thread the detail of what
// one kitty asked another (its answer already shows in the chat that asked). Neither is in the
// sidebar; opened, the line over it says what it is, and a hand-over can be kept from here.
export default function ThreadNote({
  session,
  kitties,
  goals,
  onOpenTask,
  onChanged,
}: {
  session: Session;
  kitties: Kitty[];
  goals: Goal[];
  onOpenTask: (goal: string) => void;
  onChanged: (s: Session) => void;
}) {
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  if (session.kind === "goal") {
    const goal = goals.find((g) => g.id === session.goal);
    return (
      <div class="thread-note" data-testid="thread-note" data-kind="goal">
        <IconClock size={12} />
        <span class="flex-1 min-w-0">
          {t(
            "Every run of the scheduled task “{title}”. It is not a chat: it stays out of the sidebar.",
            {
              title: goal?.title || session.title,
            },
          )}
        </span>
        {goal && (
          <button class="btn-ghost px-2 py-1 text-xs" onClick={() => onOpenTask(goal.id)}>
            {t("See its history")}
          </button>
        )}
      </div>
    );
  }
  if (session.kind !== "handover") {
    return null;
  }
  const name = (id?: string) => kitties.find((k) => k.id === id)?.name || "";
  const from = name(session.owner);
  const to = session.members.map(name).filter(Boolean).join(", ");
  const keep = async (on: boolean) => {
    setBusy(true);
    setErr("");
    try {
      onChanged(await api.keepChat(session.id, on));
    } catch (e) {
      setErr(errText(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div class="thread-note" data-testid="thread-note" data-kind="handover">
      <IconUsers size={12} />
      <span class="flex-1 min-w-0">
        {from && to
          ? t("{from} handed {to} a subtask here; the answer shows in the chat that asked.", {
              from,
              to,
            })
          : t("A hand-over between two kitties; the answer shows in the chat that asked.")}{" "}
        <span class="text-[var(--t4)]" data-testid="thread-expiry">
          {session.keep
            ? t("Kept: it stays among your chats.")
            : session.expires_at
              ? t("Deleted after {time} unless you keep it.", { time: when(session.expires_at) })
              : t("Deleted a while after the work ends, unless you keep it.")}
        </span>
        {err && <span class="text-[var(--c-red)]"> {err}</span>}
      </span>
      <button
        class={`${session.keep ? "btn-ghost" : "btn-outline"} px-2 py-1 text-xs`}
        disabled={busy}
        onClick={() => keep(!session.keep)}
        data-testid="thread-keep"
      >
        {session.keep ? t("Let it go") : t("Keep")}
      </button>
    </div>
  );
}
