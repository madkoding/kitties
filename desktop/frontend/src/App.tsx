import { useCallback, useEffect, useMemo, useState } from "preact/hooks";
import type { Kitty, ProviderInfo, Session } from "./api";
import { ApiError, api, cookieLogin, errText, inWails, watchChat } from "./api";
import { ConfirmHost, ask } from "./Confirm";
import Login from "./Login";
import Welcome from "./Welcome";
import Composer from "./Composer";
import Conversation from "./Conversation";
import { IconMenu, IconTrash, IconUsers } from "./icons";
import { ConnectModal, KittyDrawer, NewChatModal, NewKittyModal } from "./Modals";
import SettingsModal from "./Settings";
import LibraryModal from "./Library";
import ApprovalsModal from "./Approvals";
import Sidebar, { moodOf } from "./Sidebar";
import PixelCat from "./PixelCat";
import { cycleMode } from "./theme";

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
  | "connector"
  | "approvals"
  | "notifications";

// The route is the hash: #chat/<id> or #kitty/<id>, so a view can be linked and reloaded.
function routeFromHash(): {
  chat: string | null;
  kitty: string | null;
  modal: ModalKind;
  connector: string;
} {
  const m = location.hash.match(
    /^#(chat|kitty)\/([a-z0-9-]+)(?:\/new-(chat|kitty|connectors-add|connectors|models-add|connect|settings|library|artifacts|approvals|notifications))?/,
  );
  const n = location.hash.match(
    /^#new-(chat|kitty|connectors-add|connectors|models-add|connect|settings|library|artifacts|approvals|notifications)/,
  );
  const c = location.hash.match(/^#connector\/([a-z0-9-]+)/);
  const modal = (c ? "connector" : m?.[3] || n?.[1] || "none") as ModalKind;
  return {
    chat: m?.[1] === "chat" ? m[2] : null,
    kitty: m?.[1] === "kitty" ? m[2] : null,
    modal,
    connector: c?.[1] || "",
  };
}

export default function App() {
  const [kitties, setKitties] = useState<Kitty[]>([]);
  const [chats, setChats] = useState<Session[]>([]);
  const [session, setSession] = useState<Session | null>(null);
  const [current, setCurrent] = useState<string | null>(routeFromHash().chat);
  const [kittyOpen, setKittyOpen] = useState<string | null>(routeFromHash().kitty);
  const [modal, setModal] = useState<ModalKind>(routeFromHash().modal);
  const [pending, setPending] = useState<string | null>(null);
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

  const user = "You";

  const refresh = useCallback(async () => {
    try {
      const [k, c, h] = await Promise.all([api.kitties(), api.chats(), api.health()]);
      setKitties(k);
      setChats(c);
      setVersion(h.version || "");
      setContainers(h.containers !== false);
      api
        .providers()
        .then(setModels)
        .catch(() => {});
      setOnline(true);
      setError("");
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
          setModal((m) => (m === "none" ? "connect" : m));
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
    const onError = (e: ErrorEvent) => setError(`Something broke in the page: ${e.message}`);
    const onRejection = (e: PromiseRejectionEvent) =>
      setError(`Something failed: ${(e.reason as Error)?.message || String(e.reason)}`);
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
      setCurrent(r.chat);
      setKittyOpen(r.kitty);
      if (r.modal !== "none") {
        setModal(r.modal);
      }
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

  const working = useMemo(() => Boolean(session?.turns?.some((t) => t.working)), [session]);

  // The open chat is watched live: every new turn, step and finish arrives as an event and is
  // merged into the session. A slow poll remains as the safety net.
  useEffect(() => {
    if (!current) {
      return;
    }
    return watchChat(current, (ev) => {
      if (ev.type === "deleted") {
        location.hash = "";
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
  }, [current, refresh]);
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
  // leave closes a dialog that came from a route, so the address no longer points at it.
  const leave = () => {
    setModal("none");
    if (/^#(new-|connector\/)/.test(location.hash)) {
      location.hash = current ? `#chat/${current}` : "";
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
    location.hash = "";
    setModal("login");
  };

  const send = async (text: string) => {
    if (!session) {
      return;
    }
    setPending(text);
    setError("");
    try {
      await api.send(session.id, text);
    } catch (e) {
      setError((e as Error).message);
    }
    setPending(null);
    await loadSession(session.id);
    refresh();
  };

  const cancel = async () => {
    if (session) {
      await api.cancel(session.id).catch(() => {});
      loadSession(session.id);
    }
  };

  const deleteChat = async () => {
    if (session && (await ask(`Delete the chat "${session.title}"?`, "Delete"))) {
      await api.deleteChat(session.id).catch((e) => setError(e.message));
      location.hash = "";
      refresh();
    }
  };

  const members = session
    ? session.members.map((id) => kitties.find((k) => k.id === id)).filter(Boolean)
    : [];
  const kitty = kittyOpen
    ? kitties.find((k) => k.id === kittyOpen || k.name === kittyOpen)
    : undefined;

  return (
    <div class="app-bg h-full flex overflow-hidden">
      <ConfirmHost />
      <Sidebar
        chats={chats}
        kitties={kitties}
        current={current}
        workingChats={workingChats}
        workingKitties={workingKitties}
        version={version}
        open={sidebar}
        onClose={() => setSidebar(false)}
        onOpenChat={openChat}
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
      />
      <div class="flex-1 flex flex-col min-w-0">
        <header class="titlebar frosted flex items-center gap-3 px-3 sm:px-5 py-2.5 border-b border-white/5 flex-none z-10">
          <button class="plain p-1.5 md:hidden" onClick={() => setSidebar(true)} aria-label="Menu">
            <IconMenu />
          </button>
          <h1 class="text-sm sm:text-base font-semibold tracking-wide truncate min-w-0 flex-1">
            {session ? session.title : "Kitties"}
          </h1>
          {session && session.members.length > 1 && (
            <span class="tag tag-accent">
              <IconUsers size={10} /> group
            </span>
          )}
          {members.map((k) => (
            <span key={k!.id} class="hidden sm:inline-flex tag items-center">
              <PixelCat
                breed={k!.avatar?.breed}
                color={k!.avatar?.color}
                mood={workingKitties.has(k!.id) ? "working" : pending ? "thinking" : moodOf(k!)}
                size={18}
              />{" "}
              {k!.name}
            </span>
          ))}
          <span
            class={`state-pill ${working ? "is-working" : online === false ? "is-off" : "is-ok"}`}
          >
            {working && <span class="spinner" />}
            {working ? "working" : online === false ? "offline" : "connected"}
          </span>
          {session && (
            <button class="btn-ghost p-2" onClick={deleteChat} aria-label="Delete chat">
              <IconTrash size={14} />
            </button>
          )}
        </header>
        {error && (
          <div class="px-4 py-2 text-sm bg-[rgba(255,107,107,0.12)] text-[var(--c-red)] border-b border-[rgba(255,107,107,0.3)]">
            {error}
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
              aria-label="Dismiss"
            >
              ✕
            </button>
          </div>
        )}
        {current ? (
          <Conversation session={session} kitties={kitties} user={user} pending={pending} />
        ) : (
          <Welcome
            models={models}
            kitties={kitties}
            chats={chats}
            onAddModel={() => go("#new-models-add")}
            onNewKitty={() => setModal("kitty")}
            onNewChat={() => setModal("chat")}
            onConnections={() => go("#new-connectors-add")}
            onNotifications={() => go("#new-notifications")}
          />
        )}
        {session && (
          <Composer
            disabled={pending !== null || working}
            working={working}
            group={session.members.length > 1}
            onSend={send}
            onCancel={cancel}
          />
        )}
      </div>

      {modal === "chat" && (
        <NewChatModal
          kitties={kitties}
          onNeedKitty={() => setModal("kitty")}
          onClose={() => setModal("none")}
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
          taken={kitties.map((k) => k.name)}
          onNeedModel={() => {
            go("#new-models-add");
          }}
          onClose={() => setModal("none")}
          onCreated={(note) => {
            setModal("none");
            refresh();
            setNote(note || "");
          }}
        />
      )}
      {(modal === "library" || modal === "artifacts") && (
        <LibraryModal
          initial={modal === "artifacts" ? "artifacts" : "skills"}
          onClose={() => setModal("none")}
        />
      )}
      {modal === "approvals" && (
        <ApprovalsModal
          kitties={kitties}
          onClose={() => {
            location.hash = current ? `#chat/${current}` : "";
          }}
          onChanged={refresh}
          onOpenChat={openChat}
        />
      )}
      {(modal === "settings" ||
        modal === "models-add" ||
        modal === "connectors" ||
        modal === "connectors-add" ||
        modal === "connector" ||
        modal === "notifications") && (
        <SettingsModal
          initial={
            modal === "settings" || modal === "models-add"
              ? "models"
              : modal === "notifications"
                ? "notifications"
                : "connectors"
          }
          addModel={modal === "models-add"}
          addConnector={modal === "connectors-add"}
          editConnector={modal === "connector" ? routeFromHash().connector : undefined}
          kitties={kitties}
          onKittiesChanged={refresh}
          onClose={() => {
            leave();
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
          onClose={() => setModal("none")}
          reason={
            online === false
              ? error || "The gateway did not answer or refused the token."
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
          onClose={() => {
            location.hash = current ? `#chat/${current}` : "";
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
