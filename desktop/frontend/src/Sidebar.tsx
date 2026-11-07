import type { Kitty, Session } from "./api";
import {
  IconBook,
  IconBrain,
  IconMonitor,
  IconPlus,
  IconSettings,
  IconSun,
  IconUsers,
  IconX,
  IconShield,
  IconLogOut,
  IconCrown,
} from "./icons";
import PixelCat, { type Mood } from "./PixelCat";
import { t } from "./i18n";

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
  if (c.gist_by && c.members.length > 1) {
    return `${c.gist_by}: ${c.gist}`;
  }
  return c.gist;
}

interface Props {
  chats: Session[];
  kitties: Kitty[];
  current: string | null;
  workingChats: Set<string>;
  workingKitties: Set<string>;
  version: string;
  open: boolean;
  onClose: () => void;
  onOpenChat: (id: string) => void;
  onOpenKitty: (id: string) => void;
  onNewChat: () => void;
  onNewKitty: () => void;
  onSettings: () => void;
  onLibrary: () => void;
  onApprovals: () => void;
  // approvals is how many actions wait for a yes; null when this gateway holds none back.
  approvals: number | null;
  onTheme: () => void;
  // onSignOut ends the browser session; absent when there is none to end.
  onSignOut?: () => void;
}

export default function Sidebar(p: Props) {
  const byId = new Map(p.kitties.map((k) => [k.id, k]));
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
            <h2 id="sidebar-chats" class="section-title">
              {t("Chats")}
            </h2>
            {p.chats.length === 0 && (
              <div class="px-3 text-xs text-[var(--t4)]">{t("No chats yet.")}</div>
            )}
            {p.chats.map((c, i) => (
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
                />
                <span class={`chat-cats n${Math.min(c.members.length, 3)}`} data-testid="chat-cats">
                  {c.members.slice(0, 3).map((id) => {
                    const k = byId.get(id);
                    return k ? (
                      <PixelCat
                        key={id}
                        breed={k.avatar?.breed}
                        color={k.avatar?.color}
                        mood={p.workingChats.has(c.id) ? "working" : moodOf(k)}
                        size={c.members.length > 1 ? 24 : 36}
                      />
                    ) : null;
                  })}
                </span>
                <span class="flex-1 min-w-0 flex flex-col">
                  <span class="truncate text-sm">{c.title}</span>
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
                <span class="chat-badges" data-testid="chat-badges">
                  {c.needs_help && <span class="tag tag-amber">{t("help")}</span>}
                  {c.goal && !c.needs_help && <span class="tag tag-magenta">{t("goal")}</span>}
                  {c.reports && !c.needs_help && <span class="tag tag-accent">{t("reports")}</span>}
                  {c.members.length > 1 && (
                    <span class="tag" title={t("{count} kitties", { count: c.members.length })}>
                      <IconUsers size={10} />
                      {c.members.length}
                    </span>
                  )}
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
        </div>

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
          {p.onSignOut && (
            <button
              class="btn-ghost px-3 py-2 text-xs"
              onClick={p.onSignOut}
              aria-label={t("Sign out")}
              title={t("Sign out")}
            >
              <IconLogOut size={14} />
            </button>
          )}
        </div>
      </aside>
    </>
  );
}
