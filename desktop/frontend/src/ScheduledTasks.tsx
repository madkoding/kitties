import { useState } from "preact/hooks";
import type { Goal, Kitty } from "./api";
import { GoalsPanel, TASK_FILTERS } from "./Agenda";
import { t } from "./i18n";
import { Modal } from "./Modal";
import PixelCat from "./PixelCat";

// THE SCHEDULED TASKS OF EVERY KITTY.
//
// A scheduled task (a goal) runs on its own, on a schedule or on a service's events, in a thread
// of its own. That thread is the task's history, not a chat: it is read here, run by run, and
// the sidebar keeps the chats with the person. One kitty's tasks are also in its drawer.
export default function ScheduledTasksModal({
  kitties,
  goals,
  focus,
  onClose,
  onChanged,
  onOpenChat,
}: {
  kitties: Kitty[];
  goals: Goal[];
  // focus is the task to show with its history open (the one picked in the sidebar).
  focus?: string;
  onClose: () => void;
  onChanged: () => void;
  onOpenChat: (id: string) => void;
}) {
  const focused = goals.find((g) => g.id === focus);
  // pick is the kitty whose tasks show; "" shows every kitty that has some.
  const [pick, setPick] = useState(focused?.kitty || "");
  // filter keeps the tasks by how their last run ended.
  const [filter, setFilter] = useState("all");
  const keep = TASK_FILTERS.find((f) => f.id === filter)?.keep || (() => true);
  const matching = goals.filter(keep);
  const shown = pick
    ? kitties.filter((k) => k.id === pick)
    : kitties.filter((k) => matching.some((g) => g.kitty === k.id));
  return (
    <Modal title={t("Scheduled tasks")} onClose={onClose} wide>
      <p class="text-xs text-[var(--t4)] -mt-1 mb-3">
        {t(
          "What the kitties do on their own, on a schedule or when a service reports something. Each task keeps its history of runs here, out of your chats.",
        )}
      </p>
      <div class="flex flex-wrap gap-1" role="tablist" data-testid="tasks-kitties">
        <button
          role="tab"
          aria-selected={pick === ""}
          class={`${pick === "" ? "btn-outline" : "btn-ghost"} px-2 py-1 text-xs`}
          onClick={() => setPick("")}
        >
          {t("Every kitty")}
        </button>
        {kitties.map((k) => {
          const n = goals.filter((g) => g.kitty === k.id).length;
          return (
            <button
              key={k.id}
              role="tab"
              aria-selected={pick === k.id}
              class={`${pick === k.id ? "btn-outline" : "btn-ghost"} px-2 py-1 text-xs flex items-center gap-1.5`}
              onClick={() => setPick(k.id)}
            >
              <PixelCat breed={k.avatar?.breed} color={k.avatar?.color} size={16} />
              {k.name}
              {n > 0 && <span class="tag">{n}</span>}
            </button>
          );
        })}
      </div>
      <div class="flex flex-wrap items-center gap-1 mt-2" data-testid="tasks-filter">
        <span class="text-xs text-[var(--t4)] mr-1">{t("Last run")}</span>
        {TASK_FILTERS.map((f) => {
          const n = goals.filter(f.keep).length;
          return (
            <button
              key={f.id}
              class={`tag chat-filter ${filter === f.id ? "tag-accent" : ""}`}
              aria-pressed={filter === f.id}
              disabled={f.id !== "all" && n === 0}
              onClick={() => setFilter(f.id)}
              data-filter={f.id}
            >
              {t(f.label)} {f.id !== "all" && n}
            </button>
          );
        })}
      </div>
      {shown.length === 0 && filter !== "all" && (
        <p class="text-sm text-[var(--t3)] mt-4">{t("No scheduled task matches.")}</p>
      )}
      {shown.length === 0 && filter === "all" && (
        <p class="text-sm text-[var(--t3)] mt-4" data-testid="tasks-empty">
          {kitties.length === 0
            ? t("No kitties yet.")
            : t("No scheduled tasks yet. Pick a kitty to give it one.")}
        </p>
      )}
      <div data-testid="scheduled-tasks">
        {shown.map((k) => (
          <GoalsPanel
            key={k.id}
            kitty={k}
            focus={focused?.kitty === k.id ? focused.id : undefined}
            onOpenChat={onOpenChat}
            onChanged={onChanged}
            only={filter === "all" ? undefined : keep}
            heading={
              <span class="flex items-center gap-2">
                <PixelCat breed={k.avatar?.breed} color={k.avatar?.color} size={18} />
                {k.name}
              </span>
            }
          />
        ))}
      </div>
    </Modal>
  );
}
