import { useState } from "preact/hooks";
import { chatKitties, type Goal, type Kitty, type Session } from "./api";
import { describeEvery } from "./Agenda";
import { when } from "./format";
import {
  IconBook,
  IconBrain,
  IconMonitor,
  IconPlus,
  IconSettings,
  IconUpgrade,
  IconSun,
  IconUsers,
  IconX,
  IconShield,
  IconCrown,
  IconClock,
  IconSearch,
} from "./icons";
import PixelCat, { type Mood } from "./PixelCat";
import { t } from "./i18n";
import { ChatMenu, type ChatField } from "./ChatMenu";

// moodOf is how a kitty looks in a list, from its state.
export function moodOf(k: Kitty, working?: boolean): Mood {
  if (working) {
    return "working";
  }
  switch (k.status) {
    case "running":
      return "idle";
    case "error":
      return "error";
    default:
      return "sleeping";
  }
}

// gistLine is what a chat's row says under its title: its last words, and who said them when
// that is not obvious (the person, or one kitty of a group).
export function gistLine(c: Session): string {
  if (!c.gist) {
    return "";
  }
  if (c.gist_by === "user") {
    return t("You: {text}", { text: c.gist });
  }
  if (c.gist_by && chatKitties(c).length > 1) {
    return `${c.gist_by}: ${c.gist}`;
  }
  return c.gist;
}

interface Props {
  // chats are the ones the sidebar lists: no thread of a scheduled task, no hand-over not kept.
  chats: Session[];
  // goals are the scheduled tasks of every kitty; null when the gateway has no agenda.
  goals: Goal[] | null;
  // onOpenTasks opens the scheduled tasks, with one of them in view when it is given, or with a
  // filter by how their last run ended.
  onOpenTasks: (goal?: string, filter?: string) => void;
  kitties: Kitty[];
  current: string | null;
  workingChats: Set<string>;
  workingKitties: Set<string>;
  version: string;
  open: boolean;
  onClose: () => void;
  onOpenChat: (id: string) => void;
  // onEditChat and onDeleteChat are the chat row's ⋯ menu.
  onEditChat: (c: Session, field: ChatField) => void;
  onDeleteChat: (c: Session) => void;
  // onOpenKitty opens a kitty's drawer, on one of its tabs when given.
  onOpenKitty: (id: string, tab?: string) => void;
  onNewChat: () => void;
  onNewKitty: () => void;
  onSettings: () => void;
  onLibrary: () => void;
  onApprovals: () => void;
  // approvals is how many actions wait for a yes; null when this gateway holds none back.
  approvals: number | null;
  // approvalsBy is how many actions wait for a yes, per kitty id (and name).
  approvalsBy?: Record<string, number>;
  onTheme: () => void;
  // update is the newer version of Kitties that is out ("" when there is none); onUpdate offers it.
  update: string;
  onUpdate: () => void;
}

// FEW_TASKS is how many scheduled tasks the sidebar lists before "see all".
const FEW_TASKS = 4;

// taskTag is the colour of how a task's last run ended.
function taskTag(g: Goal): string {
  switch (g.last_result) {
    case "done":
      return "tag-green";
    case "running":
      return "tag-accent";
    case "delegated":
      return "tag-magenta";
    case undefined:
    case "":
      return "";
    default:
      return "tag-red";
  }
}

// fold makes a text easy to search: lower case, without accents.
export function fold(s: string): string {
  return s
    .normalize("NFD")
    .replace(/[\u0300-\u036f]/g, "")
    .toLowerCase();
}

// SEARCH_FROM is how many chats the sidebar holds before it offers to search them.
const SEARCH_FROM = 4;

// GROUPS is where the sidebar keeps, in this browser, whether it lists only the group chats.
const GROUPS = "kitties.chats.groups";

function savedGroups(): boolean {
  try {
    return localStorage.getItem(GROUPS) === "1";
  } catch {
    return false;
  }
}

export default function Sidebar(p: Props) {
  const byId = new Map(p.kitties.map((k) => [k.id, k]));
  // onlyGroups lists only the chats with more than one kitty: groups and hand-overs.
  const [onlyGroups, setOnlyGroups] = useState(savedGroups);
  const toggleGroups = () => {
    const on = !onlyGroups;
    setOnlyGroups(on);
    try {
      localStorage.setItem(GROUPS, on ? "1" : "");
    } catch {
      /* kept for this visit only */
    }
  };
  // The lead's Reports thread has a section of its own, after the chats.
  const reports = p.chats.filter((c) => c.reports);
  const plain = p.chats.filter((c) => !c.reports);
  // attention is how many scheduled tasks ended their last run badly: a look is due.
  const attention = (p.goals || []).filter((g) =>
    ["blocked", "escalated", "failed"].includes(g.last_result || ""),
  ).length;
  // pendingOf is what waits for the person in a kitty: its scheduled tasks that need a look and
  // its actions waiting for a yes.
  const goalsOf = (k: Kitty) =>
    (p.goals || []).filter(
      (g) =>
        (g.kitty === k.id || g.kitty === k.name) &&
        ["blocked", "escalated", "failed"].includes(g.last_result || ""),
    ).length;
  const approvalsOf = (k: Kitty) =>
    p.approvalsBy?.[k.id] || (k.name !== k.id && p.approvalsBy?.[k.name]) || 0;
  const pendingOf = (k: Kitty) => goalsOf(k) + approvalsOf(k);
  // openPending takes the person to what waits: the kitty's goals first, then the approvals.
  const openPending = (k: Kitty) =>
    goalsOf(k) > 0 ? p.onOpenKitty(k.id, "goals") : p.onApprovals();
  const groups = plain.filter((c) => chatKitties(c).length > 1);
  // query finds chats by their title, description or last words.
  const [query, setQuery] = useState("");
  const words = fold(query).split(/\s+/).filter(Boolean);
  const chats = (onlyGroups ? groups : plain).filter(
    (c) =>
      words.length === 0 ||
      words.every((w) => fold(`${c.title} ${c.description || ""} ${c.gist || ""}`).includes(w)),
  );
  return (
    <>
      {p.open && (
        <div class="fixed inset-0 bg-black/50 z-20 md:hidden backdrop" onClick={p.onClose} />
      )}
      <aside
        class={`sidebar fixed md:relative inset-y-0 left-0 w-72 max-w-[85vw] z-30 flex flex-col ${p.open ? "" : "max-md:hidden"}`}
      >
        <div class="titlebar flex items-center gap-2 px-4 py-3 border-b border-white/5 flex-none">
          <span class="text-accent text-lg">🐱</span>
          <h1 class="text-base font-semibold tracking-wide">Kitties</h1>
          <span class="text-[0.625rem] text-[var(--t4)] font-mono mt-0.5">{p.version}</span>
          <button class="plain ml-auto p-1.5 md:hidden" onClick={p.onClose} aria-label={t("Close")}>
            <IconX />
          </button>
        </div>

        <div class="flex-1 overflow-y-auto px-2 pb-2 scroll-thin">
          <section aria-labelledby="sidebar-kitties" data-testid="sidebar-kitties">
            <h2 id="sidebar-kitties" class="section-title">
              {t("Kitties")}
            </h2>
            {p.kitties.length === 0 && (
              <div class="px-3 text-xs text-[var(--t4)]">{t("No kitties yet.")}</div>
            )}
            {p.kitties.map((k, i) => (
              <div key={k.id} class="row" style={`animation-delay: ${Math.min(i, 10) * 30}ms`}>
                <button class="row-hit" onClick={() => p.onOpenKitty(k.id)} aria-label={k.name} />
                <PixelCat
                  breed={k.avatar?.breed}
                  color={k.avatar?.color}
                  mood={moodOf(k, p.workingKitties.has(k.id))}
                  size={26}
                />
                <span class={`dot ${k.status}`} />
                <span class="flex-1 min-w-0 truncate text-sm">{k.name}</span>
                {pendingOf(k) > 0 && (
                  <button
                    class="plain kitty-pending"
                    data-testid="kitty-pending"
                    onClick={() => openPending(k)}
                    title={t("{n} waiting for you: tasks to look at and actions to approve", {
                      n: pendingOf(k),
                    })}
                    aria-label={t("{n} waiting for you: tasks to look at and actions to approve", {
                      n: pendingOf(k),
                    })}
                  >
                    {pendingOf(k)}
                  </button>
                )}
                {k.lead && (
                  <span class="text-accent" title={t("Lead kitty")} data-testid="lead-badge">
                    <IconCrown size={12} />
                  </span>
                )}
                <span class="text-[0.625rem] font-mono text-[var(--t4)] truncate max-w-[4.375rem]">
                  {k.provider}
                </span>
                {k.desktop ? (
                  <IconMonitor size={12} class="text-accent/70" />
                ) : (
                  <IconBrain size={12} class="text-[var(--t4)]" />
                )}
              </div>
            ))}
            <button
              class="btn-outline w-full mt-2 flex items-center gap-2 px-3 py-2 text-sm"
              onClick={p.onNewKitty}
            >
              <IconPlus size={14} /> {t("New kitty")}
            </button>
          </section>

          <section aria-labelledby="sidebar-chats" data-testid="sidebar-chats">
            <div class="flex items-center justify-between">
              <h2 id="sidebar-chats" class="section-title">
                {t("Chats")}
              </h2>
              {(groups.length > 0 || onlyGroups) && (
                <button
                  class={`tag chat-filter ${onlyGroups ? "tag-accent" : ""}`}
                  onClick={toggleGroups}
                  aria-pressed={onlyGroups}
                  title={
                    onlyGroups
                      ? t("Show every chat")
                      : t("Show only the chats with more than one kitty")
                  }
                  data-testid="chats-groups"
                >
                  <IconUsers size={10} /> {t("groups")}
                </button>
              )}
            </div>
            {(plain.length >= SEARCH_FROM || query) && (
              <label class="chat-search">
                <IconSearch size={12} class="text-[var(--t4)] flex-none" />
                <input
                  type="search"
                  value={query}
                  onInput={(e) => setQuery((e.target as HTMLInputElement).value)}
                  onKeyDown={(e) =>
                    e.key === "Escape" && query && (e.stopPropagation(), setQuery(""))
                  }
                  placeholder={t("Search chats")}
                  aria-label={t("Search chats by title, description or last words")}
                  data-testid="chat-search"
                />
              </label>
            )}
            {plain.length === 0 && (
              <div class="px-3 text-xs text-[var(--t4)]">{t("No chats yet.")}</div>
            )}
            {words.length > 0 && chats.length === 0 && (
              <div class="px-3 text-xs text-[var(--t4)]" data-testid="chat-search-none">
                {t("No chat matches “{query}”.", { query: query.trim() })}
              </div>
            )}
            {onlyGroups && words.length === 0 && plain.length > 0 && chats.length === 0 && (
              <div class="px-3 text-xs text-[var(--t4)]">{t("No group chats yet.")}</div>
            )}
            {chats.map((c, i) => (
              <div
                key={c.id}
                class={`row chat-row ${p.current === c.id ? "on" : ""}`}
                style={`animation-delay: ${Math.min(i, 10) * 30}ms`}
              >
                <button
                  class="row-hit"
                  onClick={() => p.onOpenChat(c.id)}
                  aria-label={c.title}
                  aria-current={p.current === c.id ? "page" : undefined}
                  aria-describedby={c.gist ? `gist-${c.id}` : undefined}
                  title={c.description || undefined}
                />
                <span
                  class={`chat-cats n${Math.min(chatKitties(c).length, 3)}`}
                  data-testid="chat-cats"
                >
                  {chatKitties(c)
                    .slice(0, 3)
                    .map((id) => {
                      const k = byId.get(id);
                      return k ? (
                        <PixelCat
                          key={id}
                          breed={k.avatar?.breed}
                          color={k.avatar?.color}
                          mood={p.workingChats.has(c.id) ? "working" : moodOf(k)}
                          size={chatKitties(c).length > 1 ? 24 : 36}
                        />
                      ) : null;
                    })}
                </span>
                <span class="flex-1 min-w-0 flex flex-col">
                  <span class="flex items-center gap-1.5 min-w-0">
                    <span class="chat-badges" data-testid="chat-badges">
                      {c.needs_help && <span class="tag tag-amber">{t("help")}</span>}
                      {c.goal && !c.needs_help && <span class="tag tag-magenta">{t("goal")}</span>}
                      {c.kind === "handover" && !c.needs_help && (
                        <span class="tag" title={t("Kept: a hand-over between two kitties")}>
                          {t("kept")}
                        </span>
                      )}
                      {c.reports && !c.needs_help && (
                        <span class="tag tag-accent">{t("reports")}</span>
                      )}
                      {chatKitties(c).length > 1 && (
                        <span
                          class="tag"
                          data-testid="chat-count"
                          title={chatKitties(c)
                            .map((id) => byId.get(id)?.name)
                            .filter(Boolean)
                            .join(", ")}
                          aria-label={t("{count} kitties", { count: chatKitties(c).length })}
                        >
                          <IconUsers size={10} />
                          {chatKitties(c).length}
                        </span>
                      )}
                    </span>
                    <span class="truncate text-sm">{c.title}</span>
                  </span>
                  {c.gist && (
                    <span
                      id={`gist-${c.id}`}
                      class="chat-gist"
                      data-testid="chat-gist"
                      title={gistLine(c)}
                    >
                      {gistLine(c)}
                    </span>
                  )}
                </span>
                <span class="row-actions chat-row-menu">
                  <ChatMenu
                    chat={c}
                    where="sidebar"
                    onEdit={p.onEditChat}
                    onDelete={p.onDeleteChat}
                  />
                </span>
              </div>
            ))}
            <button
              class="btn-outline w-full mt-2 flex items-center gap-2 px-3 py-2 text-sm"
              onClick={p.onNewChat}
            >
              <IconPlus size={14} /> {t("New chat")}
            </button>
          </section>

          {reports.length > 0 && (
            <section aria-labelledby="sidebar-reports" data-testid="sidebar-reports">
              <h2 id="sidebar-reports" class="section-title">
                {t("Reports")}
              </h2>
              {reports.map((c) => {
                const k = byId.get(c.members[0]);
                return (
                  <div key={c.id} class={`row chat-row ${p.current === c.id ? "on" : ""}`}>
                    <button
                      class="row-hit"
                      onClick={() => p.onOpenChat(c.id)}
                      aria-label={c.title}
                      aria-current={p.current === c.id ? "page" : undefined}
                    />
                    {k && (
                      <PixelCat
                        breed={k.avatar?.breed}
                        color={k.avatar?.color}
                        mood={p.workingChats.has(c.id) ? "working" : moodOf(k)}
                        size={26}
                      />
                    )}
                    <span class="flex-1 min-w-0 flex flex-col">
                      <span class="flex items-center gap-1.5 min-w-0">
                        {c.needs_help && <span class="tag tag-amber">{t("help")}</span>}
                        <span class="truncate text-sm">{c.title}</span>
                        {k && <span class="text-[0.625rem] text-[var(--t4)]">{k.name}</span>}
                      </span>
                      {c.gist && (
                        <span class="chat-gist" data-testid="chat-gist" title={gistLine(c)}>
                          {gistLine(c)}
                        </span>
                      )}
                    </span>
                  </div>
                );
              })}
            </section>
          )}

          {p.goals !== null && p.kitties.length > 0 && (
            <section aria-labelledby="sidebar-tasks" data-testid="sidebar-tasks">
              <div class="flex items-center justify-between">
                <h2 id="sidebar-tasks" class="section-title">
                  {t("Scheduled tasks")}
                </h2>
                <span class="flex-1" />
                {attention > 0 && (
                  <button
                    class="tag tag-amber chat-filter mr-1"
                    onClick={() => p.onOpenTasks(undefined, "attention")}
                    title={t("Scheduled tasks whose last run was blocked, escalated or failed")}
                    data-testid="tasks-attention"
                  >
                    {t("{count} to look at", { count: attention })}
                  </button>
                )}
                {p.goals.length > FEW_TASKS && (
                  <button
                    class="tag chat-filter"
                    onClick={() => p.onOpenTasks()}
                    data-testid="tasks-all"
                  >
                    {t("all {count}", { count: p.goals.length })}
                  </button>
                )}
              </div>
              {p.goals.length === 0 && (
                <div class="px-3 text-xs text-[var(--t4)]">{t("No scheduled tasks yet.")}</div>
              )}
              {p.goals.slice(0, FEW_TASKS).map((g) => {
                const k = byId.get(g.kitty);
                const next = g.enabled && g.every && g.next_run ? when(g.next_run) : "";
                return (
                  <div
                    key={g.id}
                    class={`row task-row ${g.enabled ? "" : "off"}`}
                    data-testid="task-row"
                  >
                    <button
                      class="row-hit"
                      onClick={() => p.onOpenTasks(g.id)}
                      aria-label={g.title}
                    />
                    {k && (
                      <PixelCat
                        breed={k.avatar?.breed}
                        color={k.avatar?.color}
                        mood={g.last_result === "running" ? "working" : moodOf(k)}
                        size={22}
                      />
                    )}
                    <span class="flex-1 min-w-0 flex flex-col">
                      <span class="truncate text-sm">{g.title}</span>
                      <span class="task-when">
                        <IconClock size={10} />
                        {!g.enabled
                          ? t("paused")
                          : next
                            ? t("next {time}", { time: next })
                            : g.every
                              ? describeEvery(g.every)
                              : t("on events")}
                      </span>
                    </span>
                    {g.last_result && <span class={`tag ${taskTag(g)}`}>{t(g.last_result)}</span>}
                  </div>
                );
              })}
              <button
                class="btn-outline w-full mt-2 flex items-center gap-2 px-3 py-2 text-sm"
                onClick={() => p.onOpenTasks()}
                data-testid="tasks-open"
              >
                <IconClock size={14} />{" "}
                {p.goals.length > 0 ? t("See scheduled tasks") : t("New scheduled task")}
              </button>
            </section>
          )}
        </div>

        {p.update && (
          <div class="px-3 pt-2 flex-none">
            <button
              class="btn-outline w-full flex items-center gap-2 px-3 py-2 text-xs"
              onClick={p.onUpdate}
              data-testid="update-notice"
            >
              <IconUpgrade size={14} /> {t("New version")}
              <span class="tag tag-accent ml-auto">{p.update}</span>
            </button>
          </div>
        )}
        {p.approvals !== null && (
          <div class="px-3 pt-2 flex-none">
            <button
              class={`${p.approvals > 0 ? "btn-outline" : "btn-ghost"} w-full flex items-center gap-2 px-3 py-2 text-xs`}
              onClick={p.onApprovals}
              data-testid="approvals-button"
            >
              <IconShield size={14} /> {t("Approvals")}
              {p.approvals > 0 && (
                <span class="tag tag-amber ml-auto">
                  {t("{count} waiting", { count: p.approvals })}
                </span>
              )}
            </button>
          </div>
        )}
        <div class="px-3 py-2 border-t border-white/5 flex-none flex gap-1">
          <button
            class="btn-ghost flex-1 flex items-center gap-2 px-3 py-2 text-xs"
            onClick={p.onLibrary}
          >
            <IconBook size={14} /> {t("Library")}
          </button>
          <button
            class="btn-ghost flex-1 flex items-center gap-2 px-3 py-2 text-xs"
            onClick={p.onSettings}
          >
            <IconSettings size={14} /> {t("Settings")}
          </button>
          <button class="btn-ghost px-3 py-2 text-xs" onClick={p.onTheme} aria-label={t("Theme")}>
            <IconSun size={14} />
          </button>
        </div>
      </aside>
    </>
  );
}
