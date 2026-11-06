import { useEffect, useState } from "preact/hooks";
import type { Kitty, WhatsAppGroup, WhatsAppSettings, WhatsAppStatus } from "./api";
import { api, errText } from "./api";
import { ask } from "./Confirm";
import ConnectorLogo from "./ConnectorLogo";
import { t } from "./i18n";
import { IconX } from "./icons";

// WHATSAPP AS A LINKED DEVICE, docs/WHATSAPP.md.
//
// The kitties get a number of their own: the gateway links to a phone the way WhatsApp Web does
// (a QR code drawn here), the numbers on the list talk to the chosen kitty and the kitties write
// to them. Off until turned on, with the list empty; turning it on asks the person to accept the
// risk of an unofficial API. The session never reaches this page.

// The states that change by themselves: the page asks again until they settle.
const MOVING = ["starting", "qr", "disconnected"];

function pick(s: WhatsAppStatus): WhatsAppSettings {
  return {
    enabled: s.enabled,
    allow: [...(s.allow || [])],
    kitty: s.kitty || "",
    groups: !!s.groups,
    allow_groups: [...(s.allow_groups || [])],
  };
}

function same(a: WhatsAppSettings, b: WhatsAppSettings): boolean {
  return (
    a.enabled === b.enabled &&
    a.kitty === b.kitty &&
    a.groups === b.groups &&
    a.allow_groups.join(",") === b.allow_groups.join(",") &&
    a.allow.join(",") === b.allow.join(",")
  );
}

// digits is a number as the gateway keeps it, to compare what was typed with the list.
const INPUT = "w-full px-3 py-2 text-sm";

const digits = (n: string) => n.replace(/\D/g, "").replace(/^00/, "");

export default function WhatsAppLink({ kitties }: { kitties: Kitty[] }) {
  const [cur, setCur] = useState<WhatsAppStatus | null>(null);
  const [want, setWant] = useState<WhatsAppSettings | null>(null);
  const [number, setNumber] = useState("");
  const [risk, setRisk] = useState(false);
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
  // While it links or reconnects, the code and the state change by themselves.
  useEffect(() => {
    if (!cur || !cur.enabled || !MOVING.includes(cur.state)) {
      return;
    }
    const id = setInterval(() => {
      api
        .whatsapp()
        .then((s) => take(s, true))
        .catch(() => {});
    }, 2000);
    return () => clearInterval(id);
  }, [cur?.state, cur?.enabled, cur?.qr]);
  if (!cur || !want) {
    return null;
  }
  const changed = !same(want, pick(cur));
  const turningOn = want.enabled && !cur.enabled;
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
            setWant({ ...want, enabled: !want.enabled });
            setRisk(false);
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
            <option value="">{t("Choose a kitty")}</option>
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
      {turningOn && changed && (
        <label class="flex items-center gap-2 mt-3 text-sm cursor-pointer">
          <input
            type="checkbox"
            checked={risk}
            onChange={() => setRisk(!risk)}
            data-testid="whatsapp-accept"
          />
          {t("I understand that WhatsApp may ban this number")}
        </label>
      )}
      {!cur.can_change && cur.why && (
        <p class="text-xs text-[var(--t3)] mt-2" data-testid="whatsapp-locked">
          {cur.why}
        </p>
      )}
      {cur.can_change && (
        <button
          class="btn-accent mt-3 px-3 py-1.5 text-sm"
          disabled={!changed || disabled || (turningOn && !risk)}
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
            {["qr_expired", "logged_out", "error"].includes(cur.state) && (
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
            </p>
          )}
        </div>
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
