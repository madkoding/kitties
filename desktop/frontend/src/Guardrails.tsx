import { useEffect, useState } from "preact/hooks";
import type { GuardEvent, GuardSettings, GuardStatus } from "./api";
import { api, errText } from "./api";
import { ask } from "./Confirm";
import { when } from "./format";
import { t } from "./i18n";
import { IconShield, IconX } from "./icons";

// THE GUARDRAILS OF THE KITTIES THAT ANSWER OTHER PEOPLE, docs/GUARDRAILS.md.
//
// A chat is exposed when whoever writes is not the person: a WhatsApp contact or group, a chat
// marked public. There a kitty has no desktop, files, vault, notes or other kitties, only the
// connections picked here; what it says is filtered (secrets, credentials, internal paths and,
// by default, personal data) and what looks like an attempt to turn it is not answered. The
// person's own numbers keep the whole kitty. The record below never holds a secret.

const LABEL = "block text-xs text-[var(--t3)] mb-1 mt-4 uppercase tracking-wider font-mono";
const digits = (n: string) => n.replace(/\D/g, "").replace(/^00/, "");

function pick(s: GuardStatus): GuardSettings {
  return {
    enabled: s.enabled,
    connectors: [...(s.connectors || [])],
    trusted: [...(s.trusted || [])],
    max_per_hour: s.max_per_hour,
    block_injection: s.block_injection,
    redact_personal: s.redact_personal,
  };
}

const same = (a: GuardSettings, b: GuardSettings) => JSON.stringify(a) === JSON.stringify(b);

const KINDS: Record<GuardEvent["kind"], { label: string; tone: string }> = {
  redacted: { label: "Taken out", tone: "text-[var(--c-amber,#d9a441)]" },
  blocked_tool: { label: "Tool refused", tone: "text-[var(--t2)]" },
  rate_limited: { label: "Too many", tone: "text-[var(--t2)]" },
  injection: { label: "Attempt", tone: "text-[var(--c-red)]" },
};

export default function Guardrails({ connections }: { connections: string[] }) {
  const [cur, setCur] = useState<GuardStatus | null>(null);
  const [want, setWant] = useState<GuardSettings | null>(null);
  const [number, setNumber] = useState("");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [note, setNote] = useState("");
  const take = (s: GuardStatus) => {
    setCur(s);
    setWant(pick(s));
  };
  useEffect(() => {
    api
      .guard()
      .then(take)
      .catch((e) => setErr(errText(e)));
  }, []);
  if (!cur || !want) {
    return err ? (
      <p class="text-sm text-[var(--c-red)]" role="alert">
        {err}
      </p>
    ) : (
      <p class="text-sm text-[var(--t4)]">{t("Loading…")}</p>
    );
  }
  const add = () => {
    const d = digits(number);
    if (d.length < 8 || d.length > 15) {
      setErr(t("Write the number with its country code, like +56 9 1234 5678."));
      return;
    }
    setErr("");
    setNumber("");
    if (!want.trusted.includes(d)) {
      setWant({ ...want, trusted: [...want.trusted, d] });
    }
  };
  const save = async () => {
    if (
      !want.enabled &&
      cur.enabled &&
      !(await ask(
        t(
          "Turn the guardrails off? The kitties that answer other people will have their whole desktop, files, notes and connections there, and nothing they say will be filtered.",
        ),
        t("Turn off"),
      ))
    ) {
      return;
    }
    setBusy(true);
    setErr("");
    try {
      take(await api.setGuard(want));
      setNote(t("Saved."));
      setTimeout(() => setNote(""), 2500);
    } catch (e) {
      setErr(errText(e));
    } finally {
      setBusy(false);
    }
  };
  const clear = async () => {
    try {
      take(await api.clearGuardLog());
    } catch (e) {
      setErr(errText(e));
    }
  };
  const box = (key: "block_injection" | "redact_personal", title: string, hint: string) => (
    <label class="flex items-start gap-2 mt-2 text-sm cursor-pointer" data-testid={`guard-${key}`}>
      <input
        type="checkbox"
        class="mt-1"
        checked={want[key]}
        disabled={!want.enabled}
        onChange={() => setWant({ ...want, [key]: !want[key] })}
      />
      <span>
        <span class="font-semibold">{title}</span>
        <span class="block text-xs text-[var(--t3)]">{hint}</span>
      </span>
    </label>
  );
  const counts = cur.counts || {};
  return (
    <div data-testid="guardrails">
      <p class="text-sm text-[var(--t3)]">
        {t(
          "Some kitties answer people who are not you: WhatsApp contacts and groups, or a chat you made public. There they get only what is safe to share, and what they say is checked before it leaves.",
        )}
      </p>
      <label class="callout flex items-start gap-2 cursor-pointer mt-3" data-testid="guard-enabled">
        <input
          type="checkbox"
          class="mt-1"
          checked={want.enabled}
          onChange={() => setWant({ ...want, enabled: !want.enabled })}
        />
        <span>
          <span class="font-semibold text-sm flex items-center gap-1.5">
            <IconShield size={14} /> {t("Guard the chats with other people")}
          </span>
          <span class="block text-xs text-[var(--t3)]">
            {t(
              "No desktop, files, saved secrets, notes about you or other kitties there; secrets, keys, passwords and internal paths are taken out of every answer.",
            )}
          </span>
        </span>
      </label>

      {box(
        "redact_personal",
        t("Take personal data out"),
        t("E-mails, phone numbers, card numbers and RUTs the other side did not write themselves."),
      )}
      {box(
        "block_injection",
        t("Do not answer attempts"),
        t(
          "A message that tries to make the kitty ignore its rules, show its instructions, hand over secrets or run commands gets a short no. Off, it is answered with care and recorded.",
        ),
      )}

      <span class={LABEL}>{t("Messages per hour from one chat")}</span>
      <input
        type="number"
        min={1}
        max={1000}
        class="w-32 px-3 py-2 text-sm"
        value={want.max_per_hour}
        disabled={!want.enabled}
        onInput={(e) =>
          setWant({ ...want, max_per_hour: Number((e.target as HTMLInputElement).value) || 0 })
        }
        data-testid="guard-per-hour"
      />

      <span class={LABEL}>{t("Connections they may still use")}</span>
      <div class="flex flex-wrap gap-2" role="group" data-testid="guard-connectors">
        {connections.length === 0 && (
          <p class="text-xs text-[var(--t4)]">{t("You have no connections yet.")}</p>
        )}
        {connections.map((c) => {
          const on = want.connectors.includes(c);
          return (
            <label key={c} class={`chip ${on ? "on" : ""}`}>
              <input
                type="checkbox"
                class="sr-only"
                checked={on}
                disabled={!want.enabled}
                onChange={() =>
                  setWant({
                    ...want,
                    connectors: on
                      ? want.connectors.filter((x) => x !== c)
                      : [...want.connectors, c],
                  })
                }
              />
              {c}
            </label>
          );
        })}
      </div>
      <p class="text-xs text-[var(--t4)] mt-1">
        {t(
          "None by default. Pick only what anyone who writes may use, like a calendar to say when you are free.",
        )}
      </p>

      <span class={LABEL}>{t("Your own WhatsApp numbers")}</span>
      <p class="text-xs text-[var(--t3)] mb-2">
        {t("Their chats are yours, not exposed: the kitty keeps everything there.")}
      </p>
      <div class="flex gap-2 max-w-md">
        <input
          class="grow min-w-0 px-3 py-2 text-sm"
          placeholder="+56 9 1234 5678"
          inputMode="tel"
          value={number}
          onInput={(e) => setNumber((e.target as HTMLInputElement).value)}
          onKeyDown={(e) => e.key === "Enter" && add()}
          data-testid="guard-number"
        />
        <button class="btn-outline px-3 py-1.5 text-sm" disabled={!number.trim()} onClick={add}>
          {t("Add")}
        </button>
      </div>
      <div class="flex flex-wrap gap-2 mt-2" data-testid="guard-trusted">
        {want.trusted.map((n) => (
          <span key={n} class="chip on">
            +{n}
            <button
              class="opacity-70 hover:opacity-100"
              aria-label={t("Remove {number}", { number: "+" + n })}
              onClick={() => setWant({ ...want, trusted: want.trusted.filter((x) => x !== n) })}
            >
              <IconX size={12} />
            </button>
          </span>
        ))}
      </div>

      {err && (
        <p class="text-sm text-[var(--c-red)] mt-3" role="alert">
          {err}
        </p>
      )}
      <div class="flex items-center justify-end gap-3 mt-4">
        {note && <span class="text-xs text-[var(--t3)]">{note}</span>}
        <button
          class="btn-accent px-3 py-1.5 text-sm"
          disabled={busy || same(want, pick(cur))}
          onClick={save}
          data-testid="guard-save"
        >
          {t("Save")}
        </button>
      </div>

      <div class="flex items-center justify-between mt-6">
        <div class="section-title !mx-0">{t("What the guardrails did")}</div>
        {cur.log.length > 0 && (
          <button class="btn-ghost px-2 py-1 text-xs" onClick={clear} data-testid="guard-clear">
            {t("Clear")}
          </button>
        )}
      </div>
      <div class="flex flex-wrap gap-3 text-xs text-[var(--t3)] mb-2" data-testid="guard-counts">
        {(Object.keys(KINDS) as GuardEvent["kind"][]).map((k) => (
          <span key={k}>
            {t(KINDS[k].label)}: <b class="text-[var(--t1)]">{counts[k] || 0}</b>
          </span>
        ))}
      </div>
      {cur.log.length === 0 ? (
        <p class="text-sm text-[var(--t4)]">{t("Nothing yet.")}</p>
      ) : (
        <div class="flex flex-col gap-1.5" data-testid="guard-log">
          {cur.log.slice(0, 50).map((e) => (
            <div key={e.id} class="goal text-xs">
              <div class="flex items-center gap-2">
                <span class={`font-semibold ${KINDS[e.kind]?.tone || ""}`}>
                  {t(KINDS[e.kind]?.label || e.kind)}
                </span>
                <span class="text-[var(--t3)] truncate flex-1">{e.who || ""}</span>
                <span class="text-[var(--t4)] shrink-0">{when(e.at)}</span>
              </div>
              <div class="text-[var(--t2)] mt-0.5">{e.detail}</div>
              {e.text && <div class="text-[var(--t4)] mt-0.5 italic truncate">“{e.text}”</div>}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
