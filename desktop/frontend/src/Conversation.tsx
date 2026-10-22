import { useEffect, useRef, useState } from "preact/hooks";
import type { Kitty, Session, Step, Turn } from "./api";
import { api, shotData, shotURL } from "./api";
import { inline, stepArgs, when } from "./format";
import PixelCat, { type Mood } from "./PixelCat";
import ConnectorLogo from "./ConnectorLogo";
import { IconHand, IconShield, IconThumbDown, IconThumbUp } from "./icons";

function Text({ text }: { text: string }) {
  return (
    <div class="text">
      {inline(text).map((part, i) =>
        typeof part === "string" ? (
          part
        ) : part.t === "b" ? (
          <strong key={i}>{part.v}</strong>
        ) : (
          <code key={i}>{part.v}</code>
        ),
      )}
    </div>
  );
}

// Shot loads a screenshot (through the API inside Wails) and shows it as a thumbnail.
export function Shot({
  path,
  big,
  onOpen,
}: {
  path: string;
  big?: boolean;
  onOpen?: (src: string) => void;
}) {
  const [src, setSrc] = useState<string>("");
  useEffect(() => {
    let alive = true;
    shotData(path)
      .then((s) => alive && setSrc(s))
      .catch(() => alive && setSrc(""));
    return () => {
      alive = false;
    };
  }, [path]);
  if (!src) {
    return <div class={big ? "live-shot" : "shot-thumb"} />;
  }
  if (big) {
    return <img class="live-shot" src={src} alt="The kitty's display" />;
  }
  return (
    <button class="plain shot-thumb" onClick={() => onOpen?.(src)} aria-label="Open the screenshot">
      <img src={src} alt="" class="w-full h-full object-cover" />
    </button>
  );
}

function StepRow({
  step,
  chat,
  onOpen,
}: {
  step: Step;
  chat: string;
  onOpen: (src: string) => void;
}) {
  const cls = step.error ? "failed" : step.action === "done" ? "done" : "";
  const args = stepArgs(step.action, step.args);
  return (
    <div class="step">
      <span class={`what ${cls}`}>{step.action}</span>
      <span class="how">
        {args && <code>{args}</code>}
        {step.error ? (
          <span class="text-[var(--c-red)]"> {step.error}</span>
        ) : step.result && step.action !== "done" ? (
          <span> · {step.result.split("\n")[0].slice(0, 120)}</span>
        ) : null}
      </span>
      {step.shot && (
        <div class="shot">
          <Shot path={`/v1/chats/${chat}/shots/${step.shot}`} onOpen={onOpen} />
        </div>
      )}
    </div>
  );
}

function turnMood(turn: Turn, latest: boolean): Mood {
  if (turn.working) {
    return "working";
  }
  if (turn.text.includes("could not answer") || turn.text.includes("could not finish")) {
    return "error";
  }
  if (turn.steps?.some((s) => s.action === "done")) {
    return latest ? "happy" : "idle";
  }
  return latest ? "talking" : "idle";
}

// Feedback is the thumbs under what a kitty said: up is kept, down asks for a word on why. They
// teach the kitty what the person likes and how demanding they are.
function Feedback({ chat, index, current }: { chat: string; index: number; current: string }) {
  const [rating, setRating] = useState(current);
  const [asking, setAsking] = useState(false);
  const [note, setNote] = useState("");
  const [err, setErr] = useState("");
  useEffect(() => setRating(current), [current]);
  const send = async (r: "up" | "down" | "", n = "") => {
    setErr("");
    try {
      await api.feedback(chat, index, r, n);
      setRating(r);
      setAsking(false);
      setNote("");
    } catch (e) {
      setErr((e as Error).message);
    }
  };
  return (
    <span class="feedback" data-testid="feedback">
      <button
        class={`plain fb ${rating === "up" ? "on up" : ""}`}
        aria-label="Good answer"
        aria-pressed={rating === "up"}
        onClick={() => send(rating === "up" ? "" : "up")}
      >
        <IconThumbUp size={13} />
      </button>
      <button
        class={`plain fb ${rating === "down" ? "on down" : ""}`}
        aria-label="Not good"
        aria-pressed={rating === "down"}
        onClick={() => (rating === "down" ? send("") : setAsking(true))}
      >
        <IconThumbDown size={13} />
      </button>
      {asking && (
        <span class="feedback-ask">
          <input
            value={note}
            aria-label="What was wrong"
            placeholder="what was wrong? (it learns from this)"
            onInput={(e) => setNote((e.target as HTMLInputElement).value)}
            onKeyDown={(e) => e.key === "Enter" && send("down", note.trim())}
          />
          <button class="btn-ghost px-2 py-0.5 text-xs" onClick={() => send("down", note.trim())}>
            send
          </button>
        </span>
      )}
      {err && <span class="text-[var(--c-red)] text-xs">{err}</span>}
    </span>
  );
}

function TurnCard({
  turn,
  index,
  chat,
  user,
  kitty,
  latest,
  onOpen,
}: {
  turn: Turn;
  index: number;
  chat: string;
  user: string;
  kitty?: Kitty;
  latest: boolean;
  onOpen: (src: string) => void;
}) {
  const mine = turn.from === "user";
  // The scheduler's wake-up call and the answer to an approval read like the person's side,
  // without a cat.
  const scheduler = turn.from === "scheduler";
  const approval = turn.from === "approval";
  const system = scheduler || approval;
  return (
    <div
      class={`msg ${mine || system ? "user" : "agent"} ${scheduler ? "scheduler" : ""} ${approval ? "approval-turn" : ""} ${turn.working ? "working" : ""} ${turn.needs_help ? "needs-help" : ""}`}
      data-who={mine ? user : system ? turn.from : turn.from}
    >
      {!mine && !system && (
        <span class="msg-cat">
          <PixelCat
            breed={kitty?.avatar?.breed}
            color={kitty?.avatar?.color}
            mood={turn.needs_help ? "error" : turnMood(turn, latest)}
            size={40}
          />
        </span>
      )}
      {turn.needs_help && (
        <div class="help-line">
          <IconHand size={14} /> {turn.from} needs your help with this one.
        </div>
      )}
      {turn.text && <Text text={turn.text} />}
      {turn.steps && turn.steps.length > 0 && (
        <div class="trail" aria-label="steps">
          {turn.steps.map((s, i) => (
            <StepRow key={i} step={s} chat={chat} onOpen={onOpen} />
          ))}
        </div>
      )}
      {turn.working && (
        <div class="working-line">
          <span class="spinner" /> working on the desktop…
        </div>
      )}
      {(turn.calls?.length || turn.usage?.input || turn.artifacts?.length || !mine) && (
        <div class="meta">
          <span>{when(turn.at)}</span>
          {turn.calls?.map((c, i) => (
            <span
              key={i}
              class={`tag tag-logo ${c.error ? "tag-red" : "tag-accent"}`}
              title={c.error || c.output}
            >
              {c.kind && <ConnectorLogo kind={c.kind} size={12} mono />}
              {c.name}
            </span>
          ))}
          {turn.approvals?.map((a) => (
            <a key={a.id} class="tag tag-amber tag-logo" href="#new-approvals" title={a.summary}>
              <IconShield size={11} /> waiting for your yes · {a.connector}.{a.action}
            </a>
          ))}
          {turn.delegations?.map((d, i) => (
            <a
              key={"d" + i}
              class={`tag ${d.error ? "tag-red" : "tag-magenta"}`}
              href={`#chat/${d.chat}`}
              title={d.error || d.result || d.task}
            >
              → {d.kitty}
            </a>
          ))}
          {turn.skills?.map((n) => (
            <span key={"s" + n} class="tag tag-amber">
              skill · {n}
            </span>
          ))}
          {turn.artifacts?.map((n) => (
            <a
              key={"a" + n}
              class="tag tag-green"
              href={shotURL(`/v1/artifacts/${encodeURIComponent(n)}`)}
              target="_blank"
            >
              ↓ {n}
            </a>
          ))}
          {turn.usage && (turn.usage.input || turn.usage.output) ? (
            <span>
              {turn.usage.input}→{turn.usage.output} tokens
            </span>
          ) : null}
          {!mine && !system && turn.kitty && !turn.working && (
            <Feedback chat={chat} index={index} current={turn.feedback || ""} />
          )}
        </div>
      )}
    </div>
  );
}

interface Props {
  session: Session | null;
  kitties: Kitty[];
  user: string;
  pending: string | null;
}

export default function Conversation({ session, kitties, user, pending }: Props) {
  const byId = new Map(kitties.map((k) => [k.id, k]));
  const [zoom, setZoom] = useState<string>("");
  const end = useRef<HTMLDivElement>(null);
  const count = session?.turns?.length ?? 0;
  useEffect(() => {
    end.current?.scrollIntoView({ block: "end" });
  }, [count, pending, session?.id]);

  if (!session) {
    return (
      <main class="flex-1 flex items-center justify-center p-6">
        <div class="empty-card max-w-md p-6 text-center">
          <div class="text-3xl mb-2">🐱</div>
          <h2 class="font-mono text-lg mb-1">No chat open</h2>
          <p class="text-sm text-[var(--t3)]">
            Pick a chat on the left, or start one with the kitties you want in it. In a group, write
            @name to pick who answers.
          </p>
        </div>
      </main>
    );
  }
  return (
    <div
      class="flex-1 overflow-y-auto px-4 sm:px-8 py-5 flex flex-col gap-3 scroll-thin"
      role="log"
    >
      {(session.turns || []).map((t, i, all) => (
        <TurnCard
          key={i}
          turn={t}
          index={i}
          chat={session.id}
          user={user}
          kitty={t.kitty ? byId.get(t.kitty) : undefined}
          latest={i >= all.length - session.members.length}
          onOpen={setZoom}
        />
      ))}
      {pending && (
        <>
          <div class="msg user" data-who={user}>
            <Text text={pending} />
          </div>
          <div class="flex items-end gap-2 px-2 mt-2">
            {session.members.map((id) => {
              const k = byId.get(id);
              return k ? (
                <PixelCat
                  key={id}
                  breed={k.avatar?.breed}
                  color={k.avatar?.color}
                  mood="thinking"
                  size={36}
                />
              ) : null;
            })}
            <span class="working-line !mt-0">
              <span class="spinner" /> thinking…
            </span>
          </div>
        </>
      )}
      <div ref={end} />
      {zoom && (
        <div
          class="fixed inset-0 z-40 bg-black/80 flex items-center justify-center p-6 backdrop"
          onClick={() => setZoom("")}
        >
          <img
            src={zoom}
            alt="Screenshot"
            class="max-w-full max-h-full border border-[var(--neon-cyan)] shadow-[var(--glow-cyan)]"
          />
        </div>
      )}
    </div>
  );
}
