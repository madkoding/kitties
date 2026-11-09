import { useEffect, useLayoutEffect, useState } from "preact/hooks";
import type {
  Kitty,
  WhatsAppContact,
  WhatsAppGroup,
  WhatsAppHeld,
  WhatsAppSettings,
  WhatsAppStatus,
} from "./api";
import { api, errText } from "./api";
import { ask } from "./Confirm";
import ConnectorLogo from "./ConnectorLogo";
import { t } from "./i18n";
import { IconHand, IconShield, IconUsers, IconX } from "./icons";

// WHATSAPP AS A LINKED DEVICE, docs/WHATSAPP.md.
//
// The kitties get a number of their own: the gateway links to a phone the way WhatsApp Web does
// (a QR code drawn here), the numbers on the list talk to the chosen kitty and the kitties write
// to them. Off until turned on, with the list empty; turning it on the first time shows a welcome
// that asks the person to accept the risk of an unofficial API. The session never reaches this
// page.

// The states that change by themselves: the page asks again until they settle.
const MOVING = ["starting", "qr", "disconnected"];

function pick(s: WhatsAppStatus): WhatsAppSettings {
  return {
    enabled: s.enabled,
    allow: [...(s.allow || [])],
    kitty: s.kitty || "",
    groups: !!s.groups,
    allow_groups: [...(s.allow_groups || [])],
    kitties: { ...(s.kitties || {}) },
    max_per_hour: s.max_per_hour || 0,
    quiet_from: s.quiet_from || "",
    quiet_to: s.quiet_to || "",
    voice: s.voice || "",
    share_with_team: !!s.share_with_team,
    mention_all: s.mention_all === "off" ? "off" : "needed",
    mention_all_hours: s.mention_all_hours || 0,
  };
}

function same(a: WhatsAppSettings, b: WhatsAppSettings): boolean {
  return (
    a.enabled === b.enabled &&
    a.kitty === b.kitty &&
    a.groups === b.groups &&
    a.allow_groups.join(",") === b.allow_groups.join(",") &&
    a.allow.join(",") === b.allow.join(",") &&
    (a.max_per_hour || 0) === (b.max_per_hour || 0) &&
    (a.quiet_from || "") === (b.quiet_from || "") &&
    (a.quiet_to || "") === (b.quiet_to || "") &&
    (a.voice || "") === (b.voice || "") &&
    !!a.share_with_team === !!b.share_with_team &&
    (a.mention_all || "needed") === (b.mention_all || "needed") &&
    (a.mention_all_hours || 0) === (b.mention_all_hours || 0) &&
    pairs(a.kitties) === pairs(b.kitties)
  );
}

// pairs is a kitty map as one string, to compare two of them.
function pairs(m?: Record<string, string>): string {
  return Object.entries(m || {})
    .filter(([, v]) => v)
    .sort()
    .join(";");
}

// digits is a number as the gateway keeps it, to compare what was typed with the list.
const INPUT = "w-full px-3 py-2 text-sm";

const digits = (n: string) => n.replace(/\D/g, "").replace(/^00/, "");

export default function WhatsAppLink({ kitties }: { kitties: Kitty[] }) {
  // With no kitty chosen, the lead answers.
  const lead = kitties.find((k) => k.lead);
  const [cur, setCur] = useState<WhatsAppStatus | null>(null);
  const [want, setWant] = useState<WhatsAppSettings | null>(null);
  const [number, setNumber] = useState("");
  const [risk, setRisk] = useState(false);
  const [welcome, setWelcome] = useState(false);
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState("");
  const [err, setErr] = useState("");
  const [testTo, setTestTo] = useState("");
  const take = (s: WhatsAppStatus, keepEdits = false) => {
    setCur(s);
    if (!keepEdits) {
      setWant(pick(s));
    }
  };
  useEffect(() => {
    api
      .whatsapp()
      .then((s) => take(s))
      // A gateway without it (an older one) shows nothing.
      .catch(() => setCur(null));
  }, []);
  // While it links or reconnects, the code and the state change by themselves; once connected
  // the page still looks now and then, to see a dropped connection.
  useEffect(() => {
    if (!cur || !cur.enabled || !(MOVING.includes(cur.state) || cur.state === "connected")) {
      return;
    }
    const id = setInterval(
      () => {
        api
          .whatsapp()
          .then((s) => take(s, true))
          .catch(() => {});
      },
      cur.state === "connected" ? 15000 : 2000,
    );
    return () => clearInterval(id);
  }, [cur?.state, cur?.enabled, cur?.qr]);
  if (!cur || !want) {
    return null;
  }
  const changed = !same(want, pick(cur));
  const turningOn = want.enabled && !cur.enabled;
  // The risk is asked once: the first time the link is turned on.
  const needsRisk = turningOn && !cur.risk_accepted && !risk;
  const disabled = !cur.can_change || busy;
  const run = async (fn: () => Promise<WhatsAppStatus>, done?: string) => {
    setBusy(true);
    setErr("");
    setNote("");
    try {
      take(await fn());
      setRisk(false);
      if (done) {
        setNote(done);
      }
    } catch (e) {
      setErr(errText(e));
    } finally {
      setBusy(false);
    }
  };
  const add = () => {
    const d = digits(number);
    if (d.length < 7 || d.length > 15) {
      setErr(t("Write the number with its country code, like +56 9 1234 5678."));
      return;
    }
    setErr("");
    if (!want.allow.includes(d)) {
      setWant({ ...want, allow: [...want.allow, d] });
    }
    setNumber("");
  };
  const unlink = async () => {
    const yes = await ask(
      t(
        "The phone forgets this device and the kitties stop reading and writing on WhatsApp until you scan a new code. Unlink?",
      ),
      t("Unlink"),
    );
    if (yes) {
      await run(() => api.unlinkWhatsApp(), t("Unlinked."));
    }
  };
  const test = async () => {
    setErr("");
    setNote(t("Sending…"));
    try {
      const r = await api.testWhatsApp(testTo || cur.allow[0] || "");
      setNote(r.ok ? t("Sent: look at that phone.") : t("Not sent: {why}", { why: r.error || "" }));
    } catch (e) {
      setNote("");
      setErr(errText(e));
    }
  };
  const states: Record<WhatsAppStatus["state"], string> = {
    off: t("Off"),
    starting: t("Connecting…"),
    qr: t("Waiting for the QR code to be scanned"),
    qr_expired: t("The code expired"),
    connected: t("Connected"),
    disconnected: t("Reconnecting…"),
    logged_out: t("The phone removed this device"),
    error: t("It could not connect"),
  };
  return (
    <div class="mt-6 pt-4 border-t border-white/5" data-testid="whatsapp">
      <div class="section-title !mx-0 flex items-center gap-2">
        <ConnectorLogo kind="whatsapp" size={18} />
        {t("WhatsApp (linked device)")}
      </div>
      <p class="text-xs text-[var(--t3)] mb-2">
        {t(
          "A number of their own: link a phone as on WhatsApp Web, and the people on the list talk to a kitty from WhatsApp. Off until you turn it on.",
        )}
      </p>
      <div class="callout mb-2" role="note" data-testid="whatsapp-tos">
        <div class="font-semibold text-sm text-[var(--c-amber)]">
          {t("Not an official WhatsApp API")}
        </div>
        <p class="text-xs text-[var(--t3)] mt-1">
          {t(
            "WhatsApp's terms do not allow unofficial clients, and it may ban the number. Use a spare number, not your own. The official way is the WhatsApp Cloud API connection (Meta Business).",
          )}
        </p>
      </div>
      <label class="callout flex items-start gap-2 cursor-pointer" data-testid="whatsapp-enabled">
        <input
          type="checkbox"
          class="mt-1"
          checked={want.enabled}
          disabled={disabled}
          onChange={() => {
            if (!want.enabled && !cur.risk_accepted && !risk) {
              setWelcome(true);
              return;
            }
            setWant({ ...want, enabled: !want.enabled });
          }}
        />
        <span>
          <span class="font-semibold text-sm">{t("Turn on WhatsApp")}</span>
          <span class="block text-xs text-[var(--t3)]">
            {t(
              "Messages from numbers not on the list are ignored, and the kitties write only to them.",
            )}
          </span>
        </span>
      </label>
      <div class="grid gap-3 mt-3 sm:grid-cols-2">
        <label class="block text-sm">
          <span class="block text-xs text-[var(--t3)] mb-1">{t("The kitty that answers")}</span>
          <select
            class={INPUT}
            value={want.kitty}
            disabled={disabled}
            onChange={(e) => setWant({ ...want, kitty: (e.target as HTMLSelectElement).value })}
            data-testid="whatsapp-kitty"
          >
            <option value="">
              {lead ? t("The lead kitty ({name})", { name: lead.name }) : t("Choose a kitty")}
            </option>
            {kitties.map((k) => (
              <option key={k.id} value={k.id}>
                {k.name}
              </option>
            ))}
          </select>
        </label>
        <div class="text-sm">
          <span class="block text-xs text-[var(--t3)] mb-1">{t("Numbers that may write")}</span>
          <div class="flex gap-2">
            <input
              class="grow min-w-0 px-3 py-2 text-sm"
              placeholder="+56 9 1234 5678"
              inputMode="tel"
              value={number}
              disabled={disabled}
              onInput={(e) => setNumber((e.target as HTMLInputElement).value)}
              onKeyDown={(e) => e.key === "Enter" && add()}
              data-testid="whatsapp-number"
            />
            <button
              class="btn-outline px-3 py-1.5 text-sm"
              disabled={disabled || !number.trim()}
              onClick={add}
              data-testid="whatsapp-add"
            >
              {t("Add")}
            </button>
          </div>
        </div>
      </div>
      <div class="flex flex-wrap gap-2 mt-2" data-testid="whatsapp-allow">
        {want.allow.length === 0 && (
          <p class="text-xs text-[var(--t4)]">
            {t("Nobody yet: every message is ignored until you add a number.")}
          </p>
        )}
        {want.allow.map((n) => (
          <span key={n} class="chip on" data-testid="whatsapp-allowed">
            +{n}
            <button
              class="opacity-70 hover:opacity-100"
              aria-label={t("Remove {number}", { number: "+" + n })}
              disabled={disabled}
              onClick={() => setWant({ ...want, allow: want.allow.filter((x) => x !== n) })}
            >
              <IconX size={12} />
            </button>
          </span>
        ))}
      </div>
      <label
        class="flex items-center gap-2 mt-3 text-sm cursor-pointer"
        data-testid="whatsapp-groups"
      >
        <input
          type="checkbox"
          checked={want.groups}
          disabled={disabled}
          onChange={() => setWant({ ...want, groups: !want.groups })}
        />
        {t("Answer in groups when someone mentions the kitty")}
      </label>
      {want.groups && (
        <GroupPicker
          want={want.allow_groups}
          seen={cur.seen_groups || []}
          disabled={disabled}
          onChange={(allow_groups) => setWant({ ...want, allow_groups })}
        />
      )}
      {want.groups && (
        <div
          class="flex flex-wrap items-center gap-2 mt-3 text-sm"
          data-testid="whatsapp-mention-all"
        >
          {t("Mention everyone (@all)")}
          <select
            class="px-2 py-1 text-sm"
            value={want.mention_all || "needed"}
            disabled={disabled}
            onChange={(e) =>
              setWant({
                ...want,
                mention_all: (e.target as HTMLSelectElement)
                  .value as WhatsAppSettings["mention_all"],
              })
            }
          >
            <option value="needed">{t("Only when all must see it")}</option>
            <option value="off">{t("Never")}</option>
          </select>
          {want.mention_all !== "off" && (
            <label class="flex items-center gap-2">
              {t("at most once every")}
              <input
                type="number"
                min={0}
                max={168}
                class="w-16 px-2 py-1 text-sm"
                value={want.mention_all_hours || ""}
                placeholder="4"
                disabled={disabled}
                aria-label={t("Hours between two mentions of everyone")}
                onInput={(e) =>
                  setWant({
                    ...want,
                    mention_all_hours: Math.max(
                      0,
                      Math.min(168, parseInt((e.target as HTMLInputElement).value, 10) || 0),
                    ),
                  })
                }
              />
              {t("hours per group")}
            </label>
          )}
          <span class="block w-full text-xs text-[var(--t3)]">
            {t(
              "The kitty answers by quoting the message it answers, which notifies its author, and mentions others by name. It mentions everyone only for a change of plans, an urgent notice or an announcement someone asked for; past the limit, @all goes as plain text.",
            )}
          </span>
        </div>
      )}
      <WhoAnswers
        want={want}
        seen={cur.seen_groups || []}
        kitties={kitties}
        disabled={disabled}
        onChange={(m) => setWant({ ...want, kitties: m })}
      />
      <label class="flex items-center gap-2 mt-3 text-sm" data-testid="whatsapp-limit">
        {t("Messages sent per hour, at most")}
        <input
          type="number"
          min={0}
          max={1000}
          class="w-20 px-2 py-1 text-sm"
          value={want.max_per_hour || ""}
          placeholder="60"
          disabled={disabled}
          onInput={(e) =>
            setWant({
              ...want,
              max_per_hour: Math.max(0, parseInt((e.target as HTMLInputElement).value, 10) || 0),
            })
          }
        />
      </label>
      <div class="flex flex-wrap items-center gap-2 mt-3 text-sm" data-testid="whatsapp-quiet">
        {t("Quiet hours")}
        <input
          type="time"
          class="px-2 py-1 text-sm"
          value={want.quiet_from || ""}
          disabled={disabled}
          onInput={(e) => setWant({ ...want, quiet_from: (e.target as HTMLInputElement).value })}
          aria-label={t("Quiet hours start")}
        />
        –
        <input
          type="time"
          class="px-2 py-1 text-sm"
          value={want.quiet_to || ""}
          disabled={disabled}
          onInput={(e) => setWant({ ...want, quiet_to: (e.target as HTMLInputElement).value })}
          aria-label={t("Quiet hours end")}
        />
        <span class="text-xs text-[var(--t3)]">
          {t("Messages then are answered when they end.")}
        </span>
      </div>
      <label class="flex flex-wrap items-center gap-2 mt-3 text-sm" data-testid="whatsapp-voice">
        {t("Voice notes")}
        <select
          class="px-2 py-1 text-sm"
          value={want.voice || ""}
          disabled={disabled || (!cur.can_speak && !want.voice)}
          onChange={(e) =>
            setWant({
              ...want,
              voice: (e.target as HTMLSelectElement).value as WhatsAppSettings["voice"],
            })
          }
        >
          <option value="">{t("Never: the kitties write")}</option>
          <option value="mirror">{t("Answer a voice note with one")}</option>
          <option value="always">{t("Every answer")}</option>
        </select>
        <span class="text-xs text-[var(--t3)]">
          {cur.can_speak
            ? t("Short answers are read aloud; long ones and code still go as text.")
            : t("Needs an OpenAI profile with a key, for its speech.")}
        </span>
      </label>
      <label class="flex items-start gap-2 mt-3 text-sm" data-testid="whatsapp-independent">
        <input
          type="checkbox"
          class="mt-1"
          checked={!want.share_with_team}
          disabled={disabled}
          onChange={(e) =>
            setWant({ ...want, share_with_team: !(e.target as HTMLInputElement).checked })
          }
        />
        <span>
          <span class="font-semibold">{t("Works on its own")}</span>
          <span class="block text-xs text-[var(--t3)]">
            {t(
              "Recommended. The WhatsApp kitty keeps its own notes and files, does not see the other kitties' or the shared notes, and talks only with the lead, which hands it nothing secret. So it can take part in groups without mixing what each side knows.",
            )}
          </span>
          {!want.share_with_team && cur.lead_answers && (
            <span
              class="block text-xs text-[var(--warn,#b45309)] mt-1"
              data-testid="whatsapp-lead-answers"
            >
              {t(
                "The lead answers now, and the lead cannot work on its own: choose a kitty of its own for WhatsApp.",
              )}
            </span>
          )}
        </span>
      </label>
      {!cur.can_change && cur.why && (
        <p class="text-xs text-[var(--t3)] mt-2" data-testid="whatsapp-locked">
          {cur.why}
        </p>
      )}
      {cur.can_change && (
        <button
          class="btn-accent mt-3 px-3 py-1.5 text-sm"
          disabled={!changed || disabled || needsRisk}
          onClick={() => run(() => api.setWhatsApp(want, risk), t("Saved."))}
          data-testid="whatsapp-save"
        >
          {t("Save")}
        </button>
      )}
      {cur.enabled && (
        <div class="mt-4" data-testid="whatsapp-link">
          <div class="text-sm font-semibold flex items-center gap-2">
            <span
              class={`dot ${cur.state === "connected" ? "running" : cur.state === "error" || cur.state === "logged_out" ? "error" : "starting"}`}
            />
            <span data-testid="whatsapp-state">
              {states[cur.state] || cur.state}
              {cur.state === "connected" && cur.phone && ` · +${cur.phone}`}
            </span>
          </div>
          {cur.state === "qr" && cur.qr_png && (
            <div class="flex flex-wrap items-center gap-4 mt-2">
              <img
                src={cur.qr_png}
                alt={t("QR code to link WhatsApp")}
                class="rounded-lg bg-white p-2"
                width={232}
                height={232}
                data-testid="whatsapp-qr"
              />
              <ol class="text-xs text-[var(--t3)] list-decimal ml-4 max-w-xs flex flex-col gap-1">
                <li>{t("Open WhatsApp on the phone of the kitties' number.")}</li>
                <li>{t("Go to Settings, Linked devices, Link a device.")}</li>
                <li>{t("Point the phone at this code. It changes every few seconds.")}</li>
              </ol>
            </div>
          )}
          {cur.error && (
            <p class="text-xs text-[var(--c-red)] mt-1" data-testid="whatsapp-error">
              {cur.error}
            </p>
          )}
          <div class="flex flex-wrap gap-2 mt-2">
            {cur.linked && ["disconnected", "error"].includes(cur.state) && (
              <button
                class="btn-outline px-3 py-1.5 text-sm"
                disabled={busy}
                onClick={() => run(() => api.reconnectWhatsApp())}
                data-testid="whatsapp-reconnect"
              >
                {t("Reconnect")}
              </button>
            )}
            {(["qr_expired", "logged_out"].includes(cur.state) ||
              (cur.state === "error" && !cur.linked)) && (
              <button
                class="btn-outline px-3 py-1.5 text-sm"
                disabled={busy}
                onClick={() => run(() => api.linkWhatsApp())}
                data-testid="whatsapp-relink"
              >
                {t("Show a new code")}
              </button>
            )}
            {cur.state === "connected" && cur.allow.length > 0 && (
              <>
                {cur.allow.length > 1 && (
                  <select
                    class="px-3 py-1.5 text-sm"
                    value={testTo}
                    onChange={(e) => setTestTo((e.target as HTMLSelectElement).value)}
                    data-testid="whatsapp-test-to"
                  >
                    {cur.allow.map((n) => (
                      <option key={n} value={n}>
                        +{n}
                      </option>
                    ))}
                  </select>
                )}
                <button
                  class="btn-outline px-3 py-1.5 text-sm"
                  onClick={test}
                  data-testid="whatsapp-test"
                >
                  {t("Send a test")}
                </button>
              </>
            )}
            {cur.linked && (
              <button
                class="btn-outline px-3 py-1.5 text-sm"
                disabled={busy}
                onClick={unlink}
                data-testid="whatsapp-unlink"
              >
                {t("Unlink")}
              </button>
            )}
          </div>
          {cur.state === "connected" && (
            <p class="text-xs text-[var(--t3)] mt-2">
              {t("{kitty} answers on WhatsApp; each number has its own chat here.", {
                kitty: cur.kitty_name || t("The kitty"),
              })}
              {cur.ignored > 0 &&
                " " + t("{n} messages from other numbers were ignored.", { n: cur.ignored })}
              {" " +
                t("{n} messages sent in the last hour.", {
                  n: cur.sent_last_hour || 0,
                })}
              {cur.quiet &&
                " " +
                  t("Quiet hours until {until}: {n} messages wait.", {
                    until: cur.quiet_to || "",
                    n: cur.waiting || 0,
                  })}
              {(cur.held || 0) > 0 &&
                " " + t("{n} were held by the hourly limit.", { n: cur.held || 0 })}
            </p>
          )}
          {(cur.held_log || []).length > 0 && (
            <HeldLog
              held={cur.held_log || []}
              limit={cur.max_per_hour || 60}
              busy={busy}
              onClear={() => run(() => api.clearWhatsAppHeld())}
              onResend={(id) => run(() => api.resendWhatsAppHeld(id), t("Sent again."))}
            />
          )}
        </div>
      )}
      {cur.linked && <Contacts since={cur.last_message} />}
      {welcome && (
        <Welcome
          onCancel={() => setWelcome(false)}
          onAccept={() => {
            setWelcome(false);
            setRisk(true);
            setWant({ ...want, enabled: true });
          }}
        />
      )}
      {note && (
        <p class="text-xs text-[var(--t3)] mt-2" role="status" data-testid="whatsapp-note">
          {note}
        </p>
      )}
      {err && (
        <p class="text-sm text-[var(--c-red)] mt-2" role="alert">
          {err}
        </p>
      )}
    </div>
  );
}

// GroupPicker chooses the groups the kitty answers in, from the ones that wrote lately (WhatsApp
// gives a linked device no list of its groups until they write).
function GroupPicker(props: {
  want: string[];
  seen: WhatsAppGroup[];
  disabled: boolean;
  onChange: (groups: string[]) => void;
}) {
  const all = [...props.seen];
  for (const id of props.want) {
    if (!all.some((g) => g.id === id)) all.push({ id });
  }
  const toggle = (id: string) =>
    props.onChange(
      props.want.includes(id) ? props.want.filter((x) => x !== id) : [...props.want, id],
    );
  return (
    <div class="mt-2 ml-6" data-testid="whatsapp-group-list">
      <p class="text-xs text-[var(--t3)] mb-2">
        {all.length === 0
          ? t(
              "No group has written yet: write in a group with the kitties' number and it shows up here.",
            )
          : t("Groups that wrote lately. The kitty answers only in the ones you pick.")}
      </p>
      <div class="flex flex-wrap gap-2">
        {all.map((g) => {
          const on = props.want.includes(g.id);
          return (
            <button
              key={g.id}
              class={"chip" + (on ? " on" : "")}
              aria-pressed={on}
              disabled={props.disabled}
              title={g.id}
              onClick={() => toggle(g.id)}
              data-testid="whatsapp-group"
            >
              {g.name || g.id.replace(/@g\.us$/, "")}
            </button>
          );
        })}
      </div>
    </div>
  );
}

// WhoAnswers gives a number or a group a kitty of its own, when there is more than one kitty.
function WhoAnswers(props: {
  want: WhatsAppSettings;
  seen: WhatsAppGroup[];
  kitties: Kitty[];
  disabled: boolean;
  onChange: (m: Record<string, string>) => void;
}) {
  const rows = [
    ...props.want.allow.map((n) => ({ id: n, label: "+" + n })),
    ...(props.want.groups ? props.want.allow_groups : []).map((g) => ({
      id: g,
      label: props.seen.find((s) => s.id === g)?.name || g.replace(/@g\.us$/, ""),
    })),
  ];
  if (props.kitties.length < 2 || rows.length === 0) return null;
  const m = props.want.kitties || {};
  const set = (id: string, k: string) => {
    const next = { ...m };
    if (k) next[id] = k;
    else delete next[id];
    props.onChange(next);
  };
  return (
    <div class="mt-3 text-sm" data-testid="whatsapp-who">
      <span class="block text-xs text-[var(--t3)] mb-1">{t("Who answers each one")}</span>
      <div class="grid gap-1" style="grid-template-columns: max-content minmax(0, 14rem)">
        {rows.map((r) => (
          <div key={r.id} class="contents">
            <span class="self-center pr-3">{r.label}</span>
            <select
              class="px-2 py-1 text-sm"
              value={m[r.id] || ""}
              disabled={props.disabled}
              onChange={(e) => set(r.id, (e.target as HTMLSelectElement).value)}
              data-testid="whatsapp-who-kitty"
            >
              <option value="">{t("The kitty that answers")}</option>
              {props.kitties.map((k) => (
                <option key={k.id} value={k.id}>
                  {k.name}
                </option>
              ))}
            </select>
          </div>
        ))}
      </div>
    </div>
  );
}

// Welcome is shown the first time the link is turned on: what it does, what it risks, and the
// official way, with the risk to accept before going on.
function Welcome(props: { onCancel: () => void; onAccept: () => void }) {
  const [ok, setOk] = useState(false);
  // Escape cancels the welcome only, not Settings behind it: taken before Settings sees it, and
  // from the first frame.
  useLayoutEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopImmediatePropagation();
        props.onCancel();
      }
    };
    addEventListener("keydown", onKey, true);
    return () => removeEventListener("keydown", onKey, true);
  }, []);
  const points = [
    {
      icon: <IconHand size={18} />,
      title: t("Not an official API"),
      text: t(
        "Kitties links as WhatsApp Web does. WhatsApp's terms do not allow it, and it may ban the number without warning.",
      ),
    },
    {
      icon: <IconUsers size={18} />,
      title: t("A spare number, and a short list"),
      text: t(
        "Use a number that is not your own. Only the numbers and groups you add can talk to the kitties, and the kitties write only to them, a few messages an hour.",
      ),
    },
    {
      icon: <IconShield size={18} />,
      title: t("The session stays here"),
      text: t(
        "The linked device is sealed in this gateway's vault. Unlink it here or from the phone at any time.",
      ),
    },
  ];
  return (
    <div
      class="fixed inset-0 z-[60] bg-black/60 flex items-center justify-center p-4 backdrop"
      onClick={props.onCancel}
    >
      <div
        class="chat-modal w-full max-w-md p-5"
        role="dialog"
        aria-modal="true"
        aria-labelledby="whatsapp-welcome-title"
        onClick={(e) => e.stopPropagation()}
        data-testid="whatsapp-welcome"
      >
        <div class="flex items-center gap-2">
          <ConnectorLogo kind="whatsapp" size={22} />
          <h2 id="whatsapp-welcome-title" class="text-base font-semibold">
            {t("Before you turn on WhatsApp")}
          </h2>
        </div>
        <p class="text-sm text-[var(--t3)] mt-2">
          {t(
            "The kitties get a WhatsApp number of their own: the people on your list write to them, and they answer from here.",
          )}
        </p>
        <ul class="mt-4 flex flex-col gap-3">
          {points.map((p) => (
            <li key={p.title} class="flex gap-3">
              <span class="shrink-0 mt-0.5 text-[var(--c-amber)]">{p.icon}</span>
              <span>
                <span class="block text-sm font-semibold">{p.title}</span>
                <span class="block text-xs text-[var(--t3)] mt-0.5">{p.text}</span>
              </span>
            </li>
          ))}
        </ul>
        <p class="text-xs text-[var(--t3)] mt-4">
          {t(
            "For a business, the official way is the WhatsApp Cloud API connection (Meta Business), in Connections.",
          )}
        </p>
        <label class="callout flex items-start gap-2 mt-4 cursor-pointer text-sm">
          <input
            type="checkbox"
            class="mt-1"
            checked={ok}
            onChange={() => setOk(!ok)}
            data-testid="whatsapp-accept"
          />
          {t("I understand that WhatsApp may ban this number")}
        </label>
        <div class="flex justify-end gap-2 mt-4">
          <button class="btn-ghost px-4 py-2 text-sm" onClick={props.onCancel}>
            {t("Cancel")}
          </button>
          <button
            class="btn-accent px-4 py-2 text-sm"
            disabled={!ok}
            onClick={props.onAccept}
            data-testid="whatsapp-welcome-accept"
          >
            {t("Accept and continue")}
          </button>
        </div>
      </div>
    </div>
  );
}

// HeldLog lists the last messages the hourly limit did not send, newest first.
function HeldLog(props: {
  held: WhatsAppHeld[];
  limit: number;
  busy: boolean;
  onClear: () => void;
  onResend: (id: string) => void;
}) {
  const kinds: Record<WhatsAppHeld["kind"], string> = {
    text: t("Message"),
    voice: t("Voice note"),
    file: t("File"),
    reaction: t("Reaction"),
  };
  const when = (at: string) =>
    new Date(at).toLocaleString(undefined, {
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    });
  return (
    <details class="callout mt-3" data-testid="whatsapp-held">
      <summary class="cursor-pointer text-sm font-semibold">
        {t("Held by the hourly limit ({n})", { n: props.held.length })}
      </summary>
      <p class="text-xs text-[var(--t3)] mt-1">
        {t(
          "These were not sent: more than {limit} messages went in an hour. Resend one when there is room, or raise the limit above. The list is kept sealed, so a restart does not lose it.",
          { limit: props.limit },
        )}
      </p>
      <ul class="mt-2 flex flex-col gap-2">
        {props.held.map((h) => (
          <li key={h.id} class="text-xs" data-testid="whatsapp-held-item">
            <span class="text-[var(--t3)]">{when(h.at)}</span>
            {" · "}
            <span class="font-semibold">
              {h.name || (h.to.endsWith("@g.us") ? h.to.replace(/@g\.us$/, "") : "+" + h.to)}
            </span>
            {" · "}
            <span class="chip !py-0 !px-1.5">{kinds[h.kind] || h.kind}</span>
            <button
              class="btn-outline ml-2 !py-0 px-2 text-xs"
              disabled={props.busy}
              onClick={() => props.onResend(h.id)}
              data-testid="whatsapp-held-resend"
            >
              {t("Resend")}
            </button>
            {h.text && <span class="block text-[var(--t2)] mt-0.5 break-words">{h.text}</span>}
          </li>
        ))}
      </ul>
      <button
        class="btn-outline mt-2 px-3 py-1 text-xs"
        disabled={props.busy}
        onClick={props.onClear}
        data-testid="whatsapp-held-clear"
      >
        {t("Clear the list")}
      </button>
    </details>
  );
}

// Contacts are the people who wrote to the number, with the name the kitty reads each one by:
// the one the person sets here, else the one they gave themselves on WhatsApp, always with the
// number, so the people of a chat or a group are not taken for one. since is the last message:
// a new one may bring a new contact.
function Contacts({ since }: { since?: string }) {
  const [list, setList] = useState<WhatsAppContact[]>([]);
  const [names, setNames] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const load = () =>
    api
      .whatsappContacts()
      .then((l) => {
        setList(l);
        setNames(Object.fromEntries(l.map((c) => [c.number, c.name || ""])));
      })
      .catch(() => {});
  useEffect(() => {
    load();
  }, [since]);
  if (list.length === 0) {
    return null;
  }
  const save = async (c: WhatsAppContact) => {
    const name = (names[c.number] || "").trim();
    if (name === (c.name || "")) {
      return;
    }
    setBusy(true);
    setErr("");
    try {
      await api.nameWhatsAppContact(c.number, name);
      await load();
    } catch (e) {
      setErr(errText(e));
    } finally {
      setBusy(false);
    }
  };
  const forget = async (c: WhatsAppContact) => {
    setBusy(true);
    setErr("");
    try {
      await api.forgetWhatsAppContact(c.number);
      await load();
    } catch (e) {
      setErr(errText(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <details class="callout mt-3" data-testid="whatsapp-contacts">
      <summary class="cursor-pointer text-sm font-semibold">
        {t("Contacts ({n})", { n: list.length })}
      </summary>
      <p class="text-xs text-[var(--t3)] mt-1">
        {t(
          "The kitty reads each message with who wrote it, as a name and the number, and whose message a reply answers. The name is the one they gave themselves on WhatsApp until you write yours. Kept sealed.",
        )}
      </p>
      <ul class="mt-2 flex flex-col gap-2">
        {list.map((c) => (
          <li
            key={c.number}
            class="flex flex-wrap items-center gap-2 text-xs"
            data-testid="whatsapp-contact"
          >
            <input
              class="px-2 py-1 text-sm w-40"
              value={names[c.number] || ""}
              placeholder={c.push_name || t("Name")}
              maxLength={40}
              disabled={busy}
              aria-label={t("Name of +{number}", { number: c.number })}
              onInput={(e) =>
                setNames({ ...names, [c.number]: (e.target as HTMLInputElement).value })
              }
              onBlur={() => save(c)}
              // Enter saves by leaving the field, so the name is not sent twice.
              onKeyDown={(e) => e.key === "Enter" && (e.target as HTMLInputElement).blur()}
              data-testid="whatsapp-contact-name"
            />
            <span class="font-semibold">+{c.number}</span>
            {c.name && c.push_name && c.push_name !== c.name && (
              <span class="text-[var(--t3)]">
                {t("calls themselves {name}", { name: c.push_name })}
              </span>
            )}
            <button
              class="plain ml-auto"
              disabled={busy}
              onClick={() => forget(c)}
              title={t("Forget")}
              aria-label={t("Forget")}
              data-testid="whatsapp-contact-forget"
            >
              <IconX size={12} />
            </button>
          </li>
        ))}
      </ul>
      {err && (
        <p class="text-sm text-[var(--c-red)] mt-2" role="alert">
          {err}
        </p>
      )}
    </details>
  );
}
