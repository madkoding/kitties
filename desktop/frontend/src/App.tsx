import { useCallback, useEffect, useMemo, useState } from "preact/hooks";
import type { Kitty, Session } from "./api";
import { ApiError, api } from "./api";
import Composer from "./Composer";
import Conversation from "./Conversation";
import { IconMenu, IconTrash, IconUsers } from "./icons";
import { ConnectModal, KittyDrawer, NewChatModal, NewKittyModal } from "./Modals";
import SettingsModal from "./Settings";
import LibraryModal from "./Library";
import Sidebar, { moodOf } from "./Sidebar";
import PixelCat from "./PixelCat";
import { cycleMode } from "./theme";

type ModalKind = "none" | "chat" | "kitty" | "connect" | "settings" | "connectors" | "connectors-add" | "library" | "artifacts";

// The route is the hash: #chat/<id> or #kitty/<id>, so a view can be linked and reloaded.
function routeFromHash(): { chat: string | null; kitty: string | null; modal: ModalKind } {
  const m = location.hash.match(/^#(chat|kitty)\/([a-z0-9-]+)(?:\/new-(chat|kitty|connectors|connect|settings))?/);
  const n = location.hash.match(/^#new-(chat|kitty|connectors-add|connectors|connect|settings|library|artifacts)/);
  const modal = (m?.[3] || n?.[1] || "none") as ModalKind;
  return { chat: m?.[1] === "chat" ? m[2] : null, kitty: m?.[1] === "kitty" ? m[2] : null, modal };
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
  const [version, setVersion] = useState("");
  const [sidebar, setSidebar] = useState(false);
  const [online, setOnline] = useState<boolean | null>(null);

  const user = "You";

  const refresh = useCallback(async () => {
    try {
      const [k, c, h] = await Promise.all([api.kitties(), api.chats(), api.health()]);
      setKitties(k);
      setChats(c);
      setVersion(h.version || "");
      setOnline(true);
      setError("");
    } catch (e) {
      setOnline(false);
      if (e instanceof ApiError && e.status === 401) {
        setModal("connect");
      } else {
        setError((e as Error).message);
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

  // Poll: quickly while a kitty works, slowly otherwise.
  useEffect(() => {
    const every = working || pending ? 1200 : 8000;
    const id = setInterval(() => {
      refresh();
      if (current) {
        loadSession(current);
      }
    }, every);
    return () => clearInterval(id);
  }, [working, pending, current, refresh, loadSession]);

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

  const openChat = (id: string) => {
    location.hash = `#chat/${id}`;
    setSidebar(false);
  };
  const openKitty = (id: string) => {
    location.hash = `#kitty/${id}`;
    setSidebar(false);
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
    if (session && confirm(`Delete the chat "${session.title}"?`)) {
      await api.deleteChat(session.id).catch((e) => setError(e.message));
      location.hash = "";
      refresh();
    }
  };

  const members = session ? session.members.map((id) => kitties.find((k) => k.id === id)).filter(Boolean) : [];
  const kitty = kittyOpen ? kitties.find((k) => k.id === kittyOpen || k.name === kittyOpen) : undefined;

  return (
    <div class="app-bg h-full flex overflow-hidden">
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
        onTheme={cycleMode}
      />
      <div class="flex-1 flex flex-col min-w-0">
        <header class="frosted flex items-center gap-3 px-3 sm:px-5 py-2.5 border-b border-white/5 flex-none z-10">
          <button class="plain p-1.5 md:hidden" onClick={() => setSidebar(true)} aria-label="Menu">
            <IconMenu />
          </button>
          <h1 class="text-sm sm:text-base font-semibold tracking-wide truncate min-w-0 flex-1">{session ? session.title : "Kitties"}</h1>
          {session && session.members.length > 1 && (
            <span class="tag tag-accent">
              <IconUsers size={10} /> group
            </span>
          )}
          {members.map((k) => (
            <span key={k!.id} class="hidden sm:inline-flex tag items-center">
              <PixelCat breed={k!.avatar?.breed} color={k!.avatar?.color} mood={workingKitties.has(k!.id) ? "working" : pending ? "thinking" : moodOf(k!)} size={18} /> {k!.name}
            </span>
          ))}
          <span class={`state-pill ${working ? "is-working" : online === false ? "is-off" : "is-ok"}`}>
            {working && <span class="spinner" />}
            {working ? "working" : online === false ? "offline" : "connected"}
          </span>
          {session && (
            <button class="btn-ghost p-2" onClick={deleteChat} aria-label="Delete chat">
              <IconTrash size={14} />
            </button>
          )}
        </header>
        {error && <div class="px-4 py-2 text-sm bg-[rgba(255,107,107,0.12)] text-[var(--c-red)] border-b border-[rgba(255,107,107,0.3)]">{error}</div>}
        <Conversation session={session} kitties={kitties} user={user} pending={pending} />
        {session && <Composer disabled={pending !== null || working} working={working} group={session.members.length > 1} onSend={send} onCancel={cancel} />}
      </div>

      {modal === "chat" && (
        <NewChatModal
          kitties={kitties}
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
          onClose={() => setModal("none")}
          onCreated={() => {
            setModal("none");
            refresh();
          }}
        />
      )}
      {(modal === "library" || modal === "artifacts") && <LibraryModal initial={modal === "artifacts" ? "artifacts" : "skills"} onClose={() => setModal("none")} />}
      {(modal === "settings" || modal === "connectors" || modal === "connectors-add") && (
        <SettingsModal initial={modal === "settings" ? "models" : "connectors"} addConnector={modal === "connectors-add"} onClose={() => setModal("none")} />
      )}
      {modal === "connect" && (
        <ConnectModal
          onClose={() => setModal("none")}
          reason={online === false ? "The gateway did not answer or refused the token." : undefined}
          onDone={() => {
            setModal("none");
            refresh();
          }}
        />
      )}
      {kitty && (
        <KittyDrawer
          kitty={kitty}
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
