import { useCallback, useEffect, useMemo, useRef, useState } from "preact/hooks";
import type { Goal, Kitty, ProviderInfo, Session } from "./api";
import {
  ApiError,
  api,
  chatKitties,
  cookieLogin,
  errText,
  inWails,
  isListed,
  watchChat,
} from "./api";
import ArtifactReader from "./ArtifactReader";
import { ConfirmHost, ask } from "./Confirm";
import Login from "./Login";
import Welcome from "./Welcome";
import Composer from "./Composer";
import Conversation from "./Conversation";
import {
  IconMenu,
  IconMonitor,
  IconPlus,
  IconShield,
  IconSparkles,
  IconUsers,
  IconX,
} from "./icons";
import { ChatEditModal, ChatMenu, type ChatField } from "./ChatMenu";
import LiveDesktop from "./LiveDesktop";
import { ConnectModal, KittyDrawer, NewChatModal, NewKittyModal } from "./Modals";
import SettingsModal from "./Settings";
import LibraryModal from "./Library";
import ApprovalsModal from "./Approvals";
import ScheduledTasksModal from "./ScheduledTasks";
import ThreadNote from "./ThreadNote";
import Sidebar, { moodOf } from "./Sidebar";
import PixelCat from "./PixelCat";
import { cycleMode } from "./theme";
import { t } from "./i18n";
import { UpdateModal, skippedVersion, useUpdateNews } from "./Updates";

type ModalKind =
  | "none"
  | "chat"
  | "kitty"
  | "connect"
  | "login"
  | "settings"
  | "connectors"
  | "connectors-add"
  | "models-add"
  | "library"
  | "artifacts"
  | "shared"
  | "connector"
  | "artifact"
  | "approvals"
  | "notifications"
  | "machines"
  | "preferences"
  | "tasks";

// The route is the hash: #chat/<id> or #kitty/<id>, so a view can be linked and reloaded.
function routeFromHash(): {
  chat: string | null;
  kitty: string | null;
  modal: ModalKind;
  connector: string;
  artifact: string;
} {
  const m = location.hash.match(
    /^#(chat|kitty)\/([a-z0-9-]+)(?:\/new-(chat|kitty|connectors-add|connectors|models-add|connect|settings|library|artifacts|shared|approvals|notifications|machines|preferences|tasks))?/,
  );
  const n = location.hash.match(
    /^#new-(chat|kitty|connectors-add|connectors|models-add|connect|settings|library|artifacts|shared|approvals|notifications|machines|preferences|tasks)/,
  );
  const c = location.hash.match(/^#connector\/([a-z0-9-]+)/);
  // #artifact/<name> opens a file of the library in the reader: a link that can be shared.
  const a = location.hash.match(/^#artifact\/([^/?#]+)$/);
  let artifact = "";
  try {
    artifact = a ? decodeURIComponent(a[1]) : "";
  } catch {
    artifact = "";
  }
  const modal = (c ? "connector" : artifact ? "artifact" : m?.[3] || n?.[1] || "none") as ModalKind;
  return {
    chat: m?.[1] === "chat" ? m[2] : null,
    kitty: m?.[1] === "kitty" ? m[2] : null,
    modal,
    connector: c?.[1] || "",
    artifact,
  };
}

export default function App() {
  const [kitties, setKitties] = useState<Kitty[]>([]);
  const [chats, setChats] = useState<Session[]>([]);
  // goals are the scheduled tasks of every kitty, null when the gateway has no agenda; taskFocus
  // is the one to show with its history open.
  const [goals, setGoals] = useState<Goal[] | null>(null);
  const [taskFocus, setTaskFocus] = useState<string | undefined>(undefined);
  const [session, setSession] = useState<Session | null>(null);
  const [current, setCurrent] = useState<string | null>(routeFromHash().chat);
  const [kittyOpen, setKittyOpen] = useState<string | null>(routeFromHash().kitty);
  const [modal, setModal] = useState<ModalKind>(routeFromHash().modal);
  // editing is the chat whose title or description is being changed, from its ⋯ menu.
  const [editing, setEditing] = useState<{ chat: Session; field: ChatField } | null>(null);
  // live is the kitty whose desktop is open live, over the chat.
  const [live, setLive] = useState<Kitty | null>(null);
  // #chat/<id>/live (the link of a notification about a task at work) opens the chat with its
  // kitty's desktop live once the chat and the kitties are there; the address then drops it.
  const wantsLive = useRef(/^#chat\/[a-z0-9-]+\/live$/.test(location.hash));
  useEffect(() => {
    const onHash = () => {
      if (/^#chat\/[a-z0-9-]+\/live$/.test(location.hash)) {
        wantsLive.current = true;
      }
    };
    addEventListener("hashchange", onHash);
    return () => removeEventListener("hashchange", onHash);
  }, []);
  useEffect(() => {
    if (!wantsLive.current || !session || kitties.length === 0) {
      return;
    }
    wantsLive.current = false;
    const k = kitties.find(
      (k) => session.members.includes(k.id) && k.desktop && k.status === "running",
    );
    if (k) {
      setLive(k);
    }
    history.replaceState(null, "", `#chat/${session.id}`);
  }, [session, kitties]);
  // lastChat is the chat open before a dialog's address took its place, to go back to.
  const lastChat = useRef<string | null>(routeFromHash().chat);
  // connectDismissed: the person closed the gateway dialog while the gateway is away; it is not
  // opened again by itself until the gateway has come back.
  const connectDismissed = useRef(false);
  useEffect(() => {
    if (current) {
      lastChat.current = current;
    }
  }, [current]);
  const [pending, setPending] = useState<string | null>(null);
  // pendingFrom is how many turns the chat had when the message went: the sending waits for the
  // kitties' answer, but the message itself arrives live before that, and is shown once.
  const [pendingFrom, setPendingFrom] = useState(0);
  const [error, setError] = useState("");
  // note is something to know that is not a failure of the page (a desktop that did not start).
  const [note, setNote] = useState("");
  const [version, setVersion] = useState("");
  const [sidebar, setSidebar] = useState(false);
  const [online, setOnline] = useState<boolean | null>(null);
  // How many actions wait for a yes; null when this gateway holds none back.
  const [waiting, setWaiting] = useState<number | null>(null);
  // Whether this page holds a session that can be ended (a gateway served on a network).
  const [canSignOut, setCanSignOut] = useState(false);
  // The models (to guide a first run) and whether the gateway can give kitties a desktop.
  const [models, setModels] = useState<ProviderInfo[]>([]);
  const [containers, setContainers] = useState(true);
  const [dockerState, setDockerState] = useState<"not_installed" | "not_running" | undefined>();
  // The news of a newer Kitties, and whether its window is open.
  const news = useUpdateNews(online === true);
  const [upgrading, setUpgrading] = useState(false);
  // The version the person chose not to be told about (in this browser).
  const [skipped, setSkipped] = useState(skippedVersion);

  const user = t("You");

  const refresh = useCallback(async () => {
    try {
      const [k, c, h] = await Promise.all([api.kitties(), api.chats(), api.health()]);
      setKitties(k);
      setChats(c);
      api
        .goals()
        .then(setGoals)
        .catch(() => setGoals(null));
      setVersion(h.version || "");
      setContainers(h.containers !== false);
      setDockerState(h.containers_state);
      api
        .providers()
        .then(setModels)
        .catch(() => {});
      setOnline(true);
      setError("");
      connectDismissed.current = false;
      api
        .approvals("pending")
        .then((l) => setWaiting(l.length))
        .catch(() => setWaiting(null));
      if (cookieLogin()) {
        api
          .session()
          .then((s) => setCanSignOut(s.signed_in && !s.local_token))
          .catch(() => setCanSignOut(false));
      }
    } catch (e) {
      setOnline(false);
      if (e instanceof ApiError && e.status === 401) {
        setModal(cookieLogin() ? "login" : "connect");
      } else {
        // In the app, no answer at all means no gateway: say what the app did about it and let
        // the person point it at another.
        let why = errText(e);
        const app = window.go?.main?.App;
        if (app && e instanceof ApiError && e.status === 0) {
          const note = await app.GatewayNote().catch(() => "");
          why = note ? `${why} (${note})` : why;
          if (!connectDismissed.current) {
            setModal((m) => (m === "none" ? "connect" : m));
          }
        }
        setError(why);
      }
    }
  }, []);

  const loadSession = useCallback(async (id: string) => {
    try {
      setSession(await api.chat(id));
    } catch (e) {
      setError((e as Error).message);
    }
  }, []);

  // Inside the macOS app the title bar is hidden and the window's buttons float over the page:
  // the CSS leaves room for them (see .mac-inset) and lets the top bars drag the window.
  useEffect(() => {
    if (inWails() && /Mac/i.test(navigator.userAgent)) {
      document.documentElement.classList.add("mac-inset");
    }
  }, []);

  // Whatever the page fails to do and nobody caught (a screen that cannot be drawn, a promise
  // nobody handled) is said in the banner, so a blank or stuck screen always has a reason.
  useEffect(() => {
    const onError = (e: ErrorEvent) =>
      setError(t("Something broke in the page: {message}", { message: e.message }));
    const onRejection = (e: PromiseRejectionEvent) =>
      setError(
        t("Something failed: {message}", {
          message: (e.reason as Error)?.message || String(e.reason),
        }),
      );
    addEventListener("error", onError);
    addEventListener("unhandledrejection", onRejection);
    return () => {
      removeEventListener("error", onError);
      removeEventListener("unhandledrejection", onRejection);
    };
  }, []);

  useEffect(() => {
    refresh();
    const onHash = () => {
      const r = routeFromHash();
      // A dialog's own address keeps the chat that was open behind it.
      setCurrent(
        r.chat || (/^#(new-|connector\/|artifact\/)/.test(location.hash) ? lastChat.current : null),
      );
      setKittyOpen(r.kitty);
      // The address decides the dialog: one that no longer names it closes it, except the
      // sign-in and the gateway's, which the page opens itself.
      setModal((m) => (r.modal === "none" && (m === "login" || m === "connect") ? m : r.modal));
    };
    addEventListener("hashchange", onHash);
    return () => removeEventListener("hashchange", onHash);
  }, [refresh]);

  useEffect(() => {
    if (current) {
      loadSession(current);
    } else {
      setSession(null);
    }
  }, [current, loadSession]);

  // The chat a kitty's drawer shows beside its desktop: the direct chat with it, when there is one
  // and no other chat is open (it is the chat of the page when it is the open one).
  const [drawerSession, setDrawerSession] = useState<Session | null>(null);
  const drawerKitty = kittyOpen
    ? kitties.find((k) => k.id === kittyOpen || k.name === kittyOpen)
    : undefined;
  const drawerChatId = !current
    ? chats.find(
        (c) => isListed(c) && !c.goal && c.members.length === 1 && c.members[0] === drawerKitty?.id,
      )?.id
    : undefined;
  useEffect(() => {
    if (!drawerChatId) {
      setDrawerSession(null);
      return;
    }
    let alive = true;
    api
      .chat(drawerChatId)
      .then((s) => alive && setDrawerSession(s))
      .catch(() => undefined);
    const stop = watchChat(drawerChatId, (ev) => {
      if (ev.turn === undefined || ev.index === undefined) {
        return;
      }
      setDrawerSession((prev) => {
        if (!prev || prev.id !== ev.session) {
          return prev;
        }
        const turns = [...(prev.turns || [])];
        turns[ev.index!] = ev.turn!;
        return { ...prev, turns };
      });
    });
    return () => {
      alive = false;
      stop();
    };
  }, [drawerChatId]);
  // chatNow is the chat messages go to: the open one, or the drawer's.
  const chatNow = current ? session : drawerSession;
  const working = useMemo(() => Boolean(chatNow?.turns?.some((t) => t.working)), [chatNow]);
  // unshown is the message being sent until the chat itself has it.
  const unshown =
    pending !== null &&
    !(chatNow?.turns || [])
      .slice(pendingFrom)
      .some((t) => !t.kitty && t.text.trim() === pending.trim())
      ? pending
      : null;

  // The open chat is watched live: every new turn, step and finish arrives as an event and is
  // merged into the session. A slow poll remains as the safety net.
  useEffect(() => {
    if (!current) {
      return;
    }
    return watchChat(current, (ev) => {
      if (ev.type === "deleted") {
        lastChat.current = null;
        location.hash = "";
        refresh();
        return;
      }
      if (ev.type === "helping") {
        loadSession(current);
        return;
      }
      if (ev.type === "edited") {
        loadSession(current);
        refresh();
        return;
      }
      if (ev.turn === undefined || ev.index === undefined) {
        return;
      }
      setSession((prev) => {
        if (!prev || prev.id !== ev.session) {
          return prev;
        }
        const turns = [...(prev.turns || [])];
        turns[ev.index!] = ev.turn!;
        return { ...prev, turns };
      });
      if (ev.type === "done") {
        refresh();
      }
    });
  }, [current, refresh, loadSession]);
  useEffect(() => {
    const id = setInterval(() => {
      refresh();
      if (current) {
        loadSession(current);
      }
    }, 15000);
    return () => clearInterval(id);
  }, [current, refresh, loadSession]);

  const workingChats = useMemo(() => {
    const set = new Set<string>();
    if (working && session) {
      set.add(session.id);
    }
    return set;
  }, [working, session]);
  // The sidebar lists the chats with the person: a scheduled task's thread is in its history, a
  // hand-over's answer in the chat that asked (unless the person kept the thread).
  const listedChats = useMemo(() => chats.filter(isListed), [chats]);
  const workingKitties = useMemo(() => {
    const set = new Set<string>();
    session?.turns?.forEach((t) => t.working && t.kitty && set.add(t.kitty));
    return set;
  }, [session]);

  // go opens a route; the same route again still opens its dialog (no hashchange fires then).
  const go = (hash: string) => {
    if (location.hash === hash) {
      setModal(routeFromHash().modal);
    } else {
      location.hash = hash;
    }
  };
  // closeModal closes a dialog, and takes it out of the address, so a reload or the same link
  // does not open it again; the chat that was open stays.
  const closeModal = () => {
    setModal("none");
    const h = location.hash;
    const nested = h.match(/^(#(?:chat|kitty)\/[a-z0-9-]+)\/new-[a-z-]+$/);
    if (nested) {
      location.hash = nested[1];
    } else if (/^#(new-|connector\/|artifact\/)/.test(h)) {
      location.hash = lastChat.current ? `#chat/${lastChat.current}` : "";
    }
  };

  const openChat = (id: string) => {
    location.hash = `#chat/${id}`;
    setSidebar(false);
  };
  const openKitty = (id: string) => {
    location.hash = `#kitty/${id}`;
    setSidebar(false);
  };

  const signOut = async () => {
    await api.logout().catch(() => {});
    setCanSignOut(false);
    setSession(null);
    setChats([]);
    setKitties([]);
    lastChat.current = null;
    location.hash = "";
    setModal("login");
  };

  const send = async (text: string, files: File[] = []) => {
    if (!chatNow) {
      return;
    }
    setPendingFrom((chatNow.turns || []).length);
    // A message of files alone shows their names until the chat has it.
    setPending(text || files.map((f) => f.name).join(", "));
    setError("");
    let failed = "";
    try {
      await api.send(chatNow.id, text, files);
    } catch (e) {
      failed = (e as Error).message;
    }
    setPending(null);
    if (current) {
      await loadSession(chatNow.id);
    } else {
      setDrawerSession(await api.chat(chatNow.id).catch(() => chatNow));
    }
    // A refresh clears the page's error when it reaches the gateway, so the refusal of a message
    // (a file the gateway will not take) is shown after it, and stays.
    await refresh();
    if (failed) {
      setError(failed);
    }
  };

  const cancel = async () => {
    if (chatNow) {
      await api.cancel(chatNow.id).catch(() => {});
      if (current) {
        loadSession(chatNow.id);
      } else {
        api
          .chat(chatNow.id)
          .then(setDrawerSession)
          .catch(() => undefined);
      }
    }
  };

  // removeChat deletes a chat, the open one or any in the sidebar, after a yes.
  const removeChat = async (c: Session) => {
    if (
      !(await ask(
        t('Delete the chat "{title}"? Its messages, screenshots and files go for good.', {
          title: c.title,
        }),
        t("Delete"),
      ))
    ) {
      return;
    }
    try {
      await api.deleteChat(c.id);
    } catch (e) {
      setError(errText(e));
      return;
    }
    if (c.id === current) {
      lastChat.current = null;
      location.hash = "";
    }
    refresh();
  };

  // answerProposal takes or drops the description a kitty proposed for the open chat.
  const answerProposal = async (accept: boolean) => {
    if (!session) {
      return;
    }
    try {
      const c = await api.answerProposal(session.id, accept);
      setSession((prev) => (prev && prev.id === c.id ? c : prev));
      refresh();
    } catch (e) {
      setError(errText(e));
    }
  };

  // A renamed chat, or one with a new description, is shown as the gateway keeps it.
  const edited = (c: Session) => {
    setEditing(null);
    if (c.id === current) {
      setSession((prev) => (prev && prev.id === c.id ? c : prev));
    }
    refresh();
  };

  // toggleExposed marks the chat as one with people who are not the person (the guardrails apply)
  // or takes the mark off, after a yes.
  const toggleExposed = async () => {
    if (!session) {
      return;
    }
    const on = !session.exposed;
    const question = on
      ? t(
          "Mark this chat as one with other people? The kitty gets no desktop, files, secrets or notes about you here, and what it says is filtered.",
        )
      : t(
          "Take the guardrails off this chat? Do it only if everyone who writes here is you: the kitty gets everything back.",
        );
    if (!(await ask(question, on ? t("Mark it") : t("Take them off")))) {
      return;
    }
    try {
      setSession(await api.exposeChat(session.id, on, session.audience || ""));
    } catch (e) {
      setError(errText(e));
    }
  };

  // promote makes a kitty that helped through a hand-over a member of the chat: from then on it
  // answers what the person writes there too.
  const promote = async (k: Kitty) => {
    if (!session) {
      return;
    }
    if (
      !(await ask(
        t("Make {name} a member of this chat? It will answer what you write here too.", {
          name: k.name,
        }),
        t("Make it a member"),
      ))
    ) {
      return;
    }
    try {
      setSession(await api.addMembers(session.id, [k.id]));
      refresh();
    } catch (e) {
      setError(errText(e));
    }
  };

  // dismiss takes a kitty that helped through a hand-over off the chat: it is no longer shown as
  // one of its kitties, until it is handed work there again.
  const dismiss = async (k: Kitty) => {
    if (!session) {
      return;
    }
    if (
      !(await ask(
        t(
          "Take {name} off this chat? Its answers stay; it shows again if it is handed work here.",
          { name: k.name },
        ),
        t("Take it off"),
      ))
    ) {
      return;
    }
    try {
      setSession(await api.removeMember(session.id, k.id));
      refresh();
    } catch (e) {
      setError(errText(e));
    }
  };

  // restore brings back a kitty the person took off the chat.
  const restore = async (k: Kitty) => {
    if (!session) {
      return;
    }
    try {
      setSession(await api.restoreDelegate(session.id, k.id));
      refresh();
    } catch (e) {
      setError(errText(e));
    }
  };

  // Every kitty of the chat: its members and the ones handed a subtask in it.
  const members = session
    ? chatKitties(session)
        .map((id) => kitties.find((k) => k.id === id))
        .filter(Boolean)
    : [];
  const kitty = kittyOpen
    ? kitties.find((k) => k.id === kittyOpen || k.name === kittyOpen)
    : undefined;

  return (
    <div class="app-bg h-full flex overflow-hidden">
      <ConfirmHost />
      {editing && (
        <ChatEditModal
          chat={editing.chat}
          focus={editing.field}
          onClose={() => setEditing(null)}
          onSaved={edited}
        />
      )}
      {upgrading && news && (
        <UpdateModal
          status={news}
          onClose={() => setUpgrading(false)}
          onSkip={() => {
            setSkipped(news.latest || "");
            setUpgrading(false);
          }}
        />
      )}
      <Sidebar
        chats={listedChats}
        goals={goals}
        onOpenTasks={(goal) => {
          setTaskFocus(goal);
          go("#new-tasks");
          setSidebar(false);
        }}
        kitties={kitties}
        current={current}
        workingChats={workingChats}
        workingKitties={workingKitties}
        version={version}
        open={sidebar}
        onClose={() => setSidebar(false)}
        onOpenChat={openChat}
        onEditChat={(c, field) => setEditing({ chat: c, field })}
        onDeleteChat={removeChat}
        onOpenKitty={openKitty}
        onNewChat={() => setModal("chat")}
        onNewKitty={() => setModal("kitty")}
        onSettings={() => setModal("settings")}
        onLibrary={() => setModal("library")}
        onApprovals={() => {
          location.hash = "#new-approvals";
        }}
        approvals={waiting}
        onTheme={cycleMode}
        onSignOut={canSignOut ? signOut : undefined}
        update={news?.available && news.latest !== skipped ? news.latest || "" : ""}
        onUpdate={() => setUpgrading(true)}
      />
      <div class="flex-1 flex flex-col min-w-0">
        <header class="titlebar frosted flex items-center gap-3 px-3 sm:px-5 py-2.5 border-b border-white/5 flex-none z-10">
          <button
            class="plain p-1.5 md:hidden"
            onClick={() => setSidebar(true)}
            aria-label={t("Menu")}
          >
            <IconMenu />
          </button>
          <div class="min-w-0 flex-1 flex flex-col">
            <h1
              class="text-sm sm:text-base font-semibold tracking-wide truncate"
              data-testid="chat-title"
            >
              {session ? session.title : "Kitties"}
            </h1>
            {session?.description && (
              <button
                class="plain chat-description truncate text-left"
                onClick={() => setEditing({ chat: session, field: "description" })}
                title={session.description}
                data-testid="chat-description"
              >
                {session.description}
              </button>
            )}
          </div>
          {session && members.length > 1 && (
            <span
              class="tag tag-accent"
              data-testid="chat-group"
              title={t("{count} kitties", { count: members.length })}
            >
              <IconUsers size={10} /> {t("group")}
            </span>
          )}
          {session && (
            <button
              class={
                session.exposed
                  ? "tag tag-accent flex items-center gap-1"
                  : "btn-ghost p-2 text-[var(--t4)]"
              }
              onClick={toggleExposed}
              title={
                session.exposed
                  ? t("Guarded: whoever writes here is not you ({who})", {
                      who: session.audience || t("other people"),
                    })
                  : t("Mark as a chat with other people")
              }
              aria-pressed={!!session.exposed}
              data-testid="chat-exposed"
            >
              <IconShield size={session.exposed ? 10 : 14} />
              {session.exposed && <span>{t("guarded")}</span>}
            </button>
          )}
          {members.map((k) => (
            <span
              key={k!.id}
              class={`hidden sm:inline-flex tag items-center ${session?.members.includes(k!.id) ? "" : "tag-delegate"}`}
              title={
                session?.members.includes(k!.id)
                  ? undefined
                  : t("{name} helps in this chat: a subtask was handed to it", { name: k!.name })
              }
              data-testid="chat-kitty"
            >
              <PixelCat
                breed={k!.avatar?.breed}
                color={k!.avatar?.color}
                mood={workingKitties.has(k!.id) ? "working" : pending ? "thinking" : moodOf(k!)}
                size={18}
              />{" "}
              {k!.name}
              {session?.delegates?.includes(k!.id) && (
                <button
                  class="plain tag-promote"
                  onClick={() => promote(k!)}
                  title={t("Make {name} a member: it answers here too", { name: k!.name })}
                  aria-label={t("Make {name} a member: it answers here too", { name: k!.name })}
                  data-testid="promote-delegate"
                >
                  <IconPlus size={10} />
                </button>
              )}
              {session?.delegates?.includes(k!.id) && (
                <button
                  class="plain tag-promote"
                  onClick={() => dismiss(k!)}
                  title={t("Take {name} off this chat", { name: k!.name })}
                  aria-label={t("Take {name} off this chat", { name: k!.name })}
                  data-testid="dismiss-delegate"
                >
                  <IconX size={10} />
                </button>
              )}
            </span>
          ))}
          {session?.dismissed
            ?.map((id) => kitties.find((k) => k.id === id))
            .filter(Boolean)
            .map((k) => (
              <button
                key={"off-" + k!.id}
                class="hidden sm:inline-flex tag tag-dismissed items-center gap-1"
                onClick={() => restore(k!)}
                title={t("Bring {name} back to this chat", { name: k!.name })}
                data-testid="restore-delegate"
              >
                <IconPlus size={10} /> {k!.name}
              </button>
            ))}
          {members
            .filter((k) => k!.desktop && k!.status === "running")
            .map((k) => (
              <button
                key={"live-" + k!.id}
                class="btn-ghost px-2 py-1 text-xs flex items-center gap-1"
                onClick={() => setLive(k!)}
                title={t("See {name}'s desktop live, and take control of it", { name: k!.name })}
                data-testid="open-desktop"
              >
                <IconMonitor size={13} />
                <span class="hidden sm:inline">{members.length > 1 ? k!.name : t("Desktop")}</span>
              </button>
            ))}
          <span
            class={`state-pill ${working ? "is-working" : online === false ? "is-off" : "is-ok"}`}
          >
            {working && <span class="spinner" />}
            {working ? t("working") : online === false ? t("offline") : t("connected")}
          </span>
          {session && (
            <ChatMenu
              chat={session}
              where="header"
              onEdit={(c, field) => setEditing({ chat: c, field })}
              onDelete={removeChat}
            />
          )}
        </header>
        {error && (
          <div class="px-4 py-2 text-sm bg-[rgba(255,107,107,0.12)] text-[var(--c-red)] border-b border-[rgba(255,107,107,0.3)]">
            {error}
          </div>
        )}
        {session?.proposal && (
          <div
            class="callout chat-proposal !border-x-0 !border-t-0 flex flex-wrap items-center gap-2"
            role="status"
            data-testid="chat-proposal"
          >
            <IconSparkles size={14} class="text-accent flex-none mt-0.5" />
            <span class="flex-1 min-w-0">
              <span class="text-[var(--t2)]">
                {t("{name} proposes a new description for this chat:", {
                  name: session.proposal.by,
                })}
              </span>{" "}
              <span class="text-[var(--t1)]">{session.proposal.description}</span>
            </span>
            <span class="flex gap-1.5 flex-none">
              <button class="btn-ghost px-3 py-1 text-xs" onClick={() => answerProposal(false)}>
                {t("Dismiss")}
              </button>
              <button class="btn-accent px-3 py-1 text-xs" onClick={() => answerProposal(true)}>
                {t("Use it")}
              </button>
            </span>
          </div>
        )}
        {note && (
          <div
            class="callout callout-warn !border-x-0 !border-t-0 flex items-start gap-2"
            role="status"
            data-testid="note"
          >
            <span class="flex-1">{note}</span>
            <button
              class="plain text-xs text-[var(--t3)]"
              onClick={() => setNote("")}
              aria-label={t("Dismiss")}
            >
              ✕
            </button>
          </div>
        )}
        {live && (
          <LiveDesktop
            kitty={live}
            onClose={() => setLive(null)}
            chat={
              session && chatKitties(session).includes(live.id)
                ? {
                    session,
                    kitties,
                    user,
                    pending: unshown,
                    waiting: pending !== null && !working,
                    busy: pending !== null || working,
                    onSend: send,
                    onCancel: cancel,
                  }
                : undefined
            }
            onStartChat={async () => {
              const s = await api.createChat("", [live.id]);
              setLive(null);
              refresh();
              openChat(s.id);
            }}
          />
        )}
        {session && session.kind && session.kind !== "chat" && (
          <ThreadNote
            session={session}
            kitties={kitties}
            goals={goals || []}
            onOpenTask={(goal) => {
              setTaskFocus(goal);
              go("#new-tasks");
            }}
            onChanged={(s) => {
              setSession(s);
              refresh();
            }}
          />
        )}
        {current ? (
          <Conversation
            session={session}
            kitties={kitties}
            user={user}
            pending={unshown}
            waiting={pending !== null && !working}
            onSend={send}
          />
        ) : (
          <Welcome
            models={models}
            kitties={kitties}
            chats={listedChats}
            onAddModel={() => go("#new-models-add")}
            onNewKitty={() => setModal("kitty")}
            onNewChat={() => setModal("chat")}
            onConnections={() => go("#new-connectors-add")}
            onNotifications={() => go("#new-notifications")}
            onQuickStarted={(id) => {
              refresh();
              openChat(id);
            }}
          />
        )}
        {session && (
          <Composer
            disabled={pending !== null || working}
            working={working}
            group={session.members.length > 1}
            drops={!live}
            onSend={send}
            onCancel={cancel}
          />
        )}
      </div>

      {modal === "chat" && (
        <NewChatModal
          kitties={kitties}
          onNeedKitty={() => setModal("kitty")}
          onClose={closeModal}
          onCreated={(id) => {
            setModal("none");
            refresh();
            openChat(id);
          }}
        />
      )}
      {modal === "kitty" && (
        <NewKittyModal
          containers={containers}
          dockerState={dockerState}
          taken={kitties.map((k) => k.name)}
          onNeedModel={() => {
            go("#new-models-add");
          }}
          onClose={closeModal}
          onCreated={(note) => {
            setModal("none");
            refresh();
            setNote(note || "");
          }}
        />
      )}
      {(modal === "library" || modal === "artifacts" || modal === "shared") && (
        <LibraryModal
          initial={modal === "artifacts" || modal === "shared" ? modal : "skills"}
          onClose={closeModal}
        />
      )}
      {modal === "artifact" && routeFromHash().artifact && (
        <ArtifactReader name={routeFromHash().artifact} onClose={closeModal} />
      )}
      {modal === "tasks" && (
        <ScheduledTasksModal
          kitties={kitties}
          goals={goals || []}
          focus={taskFocus}
          onClose={() => {
            setTaskFocus(undefined);
            closeModal();
          }}
          onChanged={refresh}
          onOpenChat={(id) => {
            setTaskFocus(undefined);
            setModal("none");
            openChat(id);
          }}
        />
      )}
      {modal === "approvals" && (
        <ApprovalsModal
          kitties={kitties}
          onClose={closeModal}
          onChanged={refresh}
          onOpenChat={openChat}
        />
      )}
      {(modal === "settings" ||
        modal === "models-add" ||
        modal === "connectors" ||
        modal === "connectors-add" ||
        modal === "connector" ||
        modal === "notifications" ||
        modal === "machines" ||
        modal === "preferences") && (
        <SettingsModal
          initial={
            modal === "settings" || modal === "models-add"
              ? "models"
              : modal === "notifications" || modal === "machines" || modal === "preferences"
                ? modal
                : "connectors"
          }
          addModel={modal === "models-add"}
          addConnector={modal === "connectors-add"}
          editConnector={modal === "connector" ? routeFromHash().connector : undefined}
          kitties={kitties}
          onKittiesChanged={refresh}
          onClose={() => {
            closeModal();
            refresh();
          }}
        />
      )}
      {modal === "login" && (
        <Login
          onDone={() => {
            setModal("none");
            refresh();
          }}
        />
      )}
      {modal === "connect" && (
        <ConnectModal
          onClose={() => {
            connectDismissed.current = online === false;
            closeModal();
          }}
          reason={
            online === false
              ? error || t("The gateway did not answer or refused the token.")
              : undefined
          }
          onDone={() => {
            setModal("none");
            refresh();
          }}
        />
      )}
      {kitty && (
        <KittyDrawer
          kitty={kitty}
          kitties={kitties}
          onOpenChat={openChat}
          chat={
            drawerSession
              ? {
                  session: drawerSession,
                  kitties,
                  user,
                  pending: unshown,
                  waiting: pending !== null && !working,
                  busy: pending !== null || working,
                  onSend: send,
                  onCancel: cancel,
                }
              : undefined
          }
          onClose={() => {
            location.hash = lastChat.current ? `#chat/${lastChat.current}` : "";
          }}
          onChanged={refresh}
          onChat={async () => {
            const s = await api.createChat("", [kitty.id]);
            refresh();
            openChat(s.id);
          }}
        />
      )}
    </div>
  );
}
