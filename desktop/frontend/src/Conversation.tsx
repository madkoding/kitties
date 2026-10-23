import { useEffect, useRef, useState } from "preact/hooks";
import type { Kitty, Session, Step, Turn } from "./api";
import { api, shotData, shotURL } from "./api";
import { inline, stepArgs, stepSentence, when } from "./format";
import { actionLabel, plainError } from "./friendly";
import { gatewayText } from "./gatewayText";
import { t } from "./i18n";
import { useTechDetails } from "./prefs";
import PixelCat, { type Mood } from "./PixelCat";
import ConnectorLogo from "./ConnectorLogo";
import { IconHand, IconShield, IconThumbDown, IconThumbUp } from "./icons";
import { RULE_LABEL, ruleTarget } from "./Approvals";

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
    return <img class="live-shot" src={src} alt={t("The kitty's display")} />;
  }
  return (
    <button
      class="plain shot-thumb"
      onClick={() => onOpen?.(src)}
      aria-label={t("Open the screenshot")}
    >
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
  const tech = useTechDetails();
  const cls = step.error ? "failed" : step.action === "done" ? "done" : "";
  // The step reads as a sentence; what it did exactly (coordinates, the command, the result) is
  // on hover, or written out with the technical details on.
  const args = stepArgs(step.action, step.args);
  const result = step.action !== "done" ? step.result?.split("\n")[0].slice(0, 120) : "";
  const detail = [step.action, args, result].filter(Boolean).join(" · ");
  return (
    <div class="step" title={detail}>
      <span class={`what ${cls}`} aria-hidden="true" />
      <span class="how">
        {stepSentence(step.action, step.args)}
        {tech && (
          <code class="tech" data-testid="step-detail">
            {" "}
            {detail}
          </code>
        )}
        {step.error && (
          <span class="text-[var(--c-red)]">
            {" "}
            — {t("it did not work: {error}", { error: tech ? step.error : plainError(step.error) })}
          </span>
        )}
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

// LimitNote shows a limit the kitty set on itself because the person asked, with a way to take
// it back: undoing puts back the mode there was before (none, for a new limit).
function LimitNote({
  kitty,
  limit,
}: {
  kitty: string;
  limit: { connector: string; action: string; mode: string; previous?: string };
}) {
  const [state, setState] = useState<"" | "busy" | "undone">("");
  const [err, setErr] = useState("");
  const target = ruleTarget(limit);
  const undo = async () => {
    setState("busy");
    setErr("");
    try {
      const action = limit.action === "*" ? "" : limit.action;
      await api.setRule(
        kitty,
        limit.connector,
        action,
        (limit.previous || "") as "ask" | "never" | "",
      );
      setState("undone");
    } catch (e) {
      setErr((e as Error).message);
      setState("");
    }
  };
  return (
    <div class="limit-note" data-testid="limit-note">
      <IconShield size={13} />
      <span class="flex-1">
        {limit.mode
          ? t("Limit set: {target} · {mode}", {
              target,
              mode: RULE_LABEL[limit.mode] || limit.mode,
            })
          : t("Limit lifted: {target}", { target })}
        {state === "undone" && <span class="text-[var(--t3)]"> · {t("undone")}</span>}
        {err && <span class="text-[var(--c-red)]"> · {err}</span>}
      </span>
      {state !== "undone" && (
        <button
          class="btn-ghost px-2 py-1 text-xs"
          disabled={state === "busy"}
          onClick={undo}
          data-testid="limit-undo"
        >
          {t("Undo")}
        </button>
      )}
    </div>
  );
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
        aria-label={t("Good answer")}
        aria-pressed={rating === "up"}
        onClick={() => send(rating === "up" ? "" : "up")}
      >
        <IconThumbUp size={13} />
      </button>
      <button
        class={`plain fb ${rating === "down" ? "on down" : ""}`}
        aria-label={t("Not good")}
        aria-pressed={rating === "down"}
        onClick={() => (rating === "down" ? send("") : setAsking(true))}
      >
        <IconThumbDown size={13} />
      </button>
      {asking && (
        <span class="feedback-ask">
          <input
            value={note}
            aria-label={t("What was wrong")}
            placeholder={t("what was wrong? (it learns from this)")}
            onInput={(e) => setNote((e.target as HTMLInputElement).value)}
            onKeyDown={(e) => e.key === "Enter" && send("down", note.trim())}
          />
          <button class="btn-ghost px-2 py-0.5 text-xs" onClick={() => send("down", note.trim())}>
            {t("send")}
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
  const tech = useTechDetails();
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
          <IconHand size={14} /> {t("{name} needs your help with this one.", { name: turn.from })}
        </div>
      )}
      {turn.text && <Text text={mine ? turn.text : gatewayText(turn.text)} />}
      {turn.kitty &&
        turn.limits?.map((l, i) => <LimitNote key={i} kitty={turn.kitty!} limit={l} />)}
      {tech && turn.detail && (
        <pre class="tech-detail" data-testid="turn-detail">
          {turn.detail}
        </pre>
      )}
      {turn.steps && turn.steps.length > 0 && (
        <div class="trail" aria-label={t("steps")}>
          {turn.steps.map((s, i) => (
            <StepRow key={i} step={s} chat={chat} onOpen={onOpen} />
          ))}
        </div>
      )}
      {turn.working && (
        <div class="working-line">
          <span class="spinner" /> {t("working on its computer…")}
        </div>
      )}
      {(turn.calls?.length || turn.usage?.input || turn.artifacts?.length || !mine) && (
        <div class="meta">
          <span
            title={
              turn.usage && (turn.usage.input || turn.usage.output)
                ? t("{input} → {output} tokens", {
                    input: turn.usage.input,
                    output: turn.usage.output,
                  })
                : undefined
            }
          >
            {when(turn.at)}
          </span>
          {turn.calls?.map((c, i) => (
            <span
              key={i}
              class={`tag tag-logo ${c.error ? "tag-red" : "tag-accent"}`}
              title={tech ? c.error || c.output : c.error ? plainError(c.error) : undefined}
            >
              {c.kind && <ConnectorLogo kind={c.kind} size={12} mono />}
              {c.name}
            </span>
          ))}
          {tech && turn.usage && (turn.usage.input || turn.usage.output) ? (
            <span data-testid="tokens">
              {t("{input}→{output} tokens", { input: turn.usage.input, output: turn.usage.output })}
            </span>
          ) : null}
          {turn.approvals?.map((a) => (
            <a key={a.id} class="tag tag-amber tag-logo" href="#new-approvals" title={a.summary}>
              <IconShield size={11} />{" "}
              {t("waiting for your yes: {action} ({connector})", {
                action: actionLabel(a.action),
                connector: a.connector,
              })}
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
              {t("skill · {name}", { name: n })}
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
  // pending is the message being sent, until the chat has it; waiting shows the kitties
  // thinking until one of them is at work.
  pending: string | null;
  waiting?: boolean;
}

export default function Conversation({ session, kitties, user, pending, waiting }: Props) {
  const byId = new Map(kitties.map((k) => [k.id, k]));
  const [zoom, setZoom] = useState<string>("");
  const end = useRef<HTMLDivElement>(null);
  const count = session?.turns?.length ?? 0;
  // Escape closes a screenshot opened big, like a click on it does.
  useEffect(() => {
    if (!zoom) {
      return;
    }
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && setZoom("");
    addEventListener("keydown", onKey);
    return () => removeEventListener("keydown", onKey);
  }, [zoom]);
  useEffect(() => {
    end.current?.scrollIntoView({ block: "end" });
  }, [count, pending, session?.id]);

  if (!session) {
    return (
      <main class="flex-1 flex items-center justify-center p-6">
        <div class="empty-card max-w-md p-6 text-center">
          <div class="text-3xl mb-2">🐱</div>
          <h2 class="font-mono text-lg mb-1">{t("No chat open")}</h2>
          <p class="text-sm text-[var(--t3)]">
            {t(
              "Pick a chat on the left, or start one with the kitties you want in it. In a group, write @name to pick who answers.",
            )}
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
      {(session.turns || []).length === 0 && !pending && (
        <div class="empty-card max-w-md mx-auto mt-10 p-5 text-center" data-testid="chat-empty">
          <div class="text-2xl mb-1">👋</div>
          <p class="text-sm text-[var(--t2)]">
            {session.members.length > 1
              ? t("Say hello to the team, or give them a task. Write @name to pick who answers.")
              : t("Say hello, ask a question, or give it a task to work on.")}
          </p>
        </div>
      )}
      {(session.turns || []).map((turn, i, all) => (
        <TurnCard
          key={i}
          turn={turn}
          index={i}
          chat={session.id}
          user={user}
          kitty={turn.kitty ? byId.get(turn.kitty) : undefined}
          latest={i >= all.length - session.members.length}
          onOpen={setZoom}
        />
      ))}
      {pending && (
        <div class="msg user" data-who={user}>
          <Text text={pending} />
        </div>
      )}
      {(pending || waiting) && (
        <>
          <div class="flex items-end gap-2 px-2 mt-2" data-testid="thinking">
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
              <span class="spinner" /> {t("thinking…")}
            </span>
          </div>
        </>
      )}
      <div ref={end} />
      {zoom && (
        <div
          class="fixed inset-0 z-40 bg-black/80 flex items-center justify-center p-6 backdrop"
          onClick={() => setZoom("")}
          data-testid="zoom"
        >
          <img
            src={zoom}
            alt={t("Screenshot")}
            class="max-w-full max-h-full border border-[var(--neon-cyan)] shadow-[var(--glow-cyan)]"
          />
        </div>
      )}
    </div>
  );
}
