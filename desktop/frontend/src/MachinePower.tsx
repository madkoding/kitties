import { useEffect, useState } from "preact/hooks";
import type { Kitty, Memory, Power } from "./api";
import { api, errText } from "./api";
import { t } from "./i18n";
import MachineMemory from "./MachineMemory";

// THE KITTY'S MACHINE.
//
// A kitty's desktop runs on a machine that can be on or off. It is turned on and off with the
// button of the kitty's header; it turns itself off after a time without use (15 minutes unless
// the gateway says otherwise), and never while the kitty is working, or you are watching its
// desktop or have taken control. Here you see its state and choose that time for this kitty,
// down to never.

// IDLE_CHOICES are the idle times offered, in minutes; 0 is never.
const IDLE_CHOICES = [5, 15, 30, 60, 120, 240, 480, 0];

function minutesText(n: number): string {
  if (n === 0) {
    return t("Never");
  }
  if (n % 60 === 0) {
    return n === 60 ? t("1 hour") : t("{n} hours", { n: n / 60 });
  }
  return t("{n} minutes", { n });
}

function inUseText(by?: string): string {
  switch (by) {
    case "task":
      return t("working on a task");
    case "control":
      return t("you have taken control");
    case "watch":
      return t("you are watching its desktop");
    case "command":
      return t("running a command");
    default:
      return t("in use");
  }
}

function stateText(p: Power): string {
  switch (p.state) {
    case "starting":
      return p.step === "desktop" ? t("Starting the desktop…") : t("Starting…");
    case "error":
      return p.error ? t("Failed to start: {error}", { error: p.error }) : t("Failed to start");
    case "off":
      return p.off_reason === "idle"
        ? t("Off: it turned itself off after a time without use")
        : p.off_reason === "kitty"
          ? t("Off: the kitty turned it off")
          : t("Off");
  }
  if (p.in_use) {
    return t("On, in use: {what}", { what: inUseText(p.in_use_by) });
  }
  if (p.auto_off_minutes === 0) {
    return t("On. It never turns itself off.");
  }
  if (p.off_in_seconds !== undefined) {
    return t("On. It turns itself off in about {n} min if it is not used.", {
      n: Math.max(1, Math.ceil(p.off_in_seconds / 60)),
    });
  }
  return t("On");
}

export default function MachinePower({
  kitty,
  onChanged,
}: {
  kitty: Kitty;
  onChanged: () => void;
}) {
  const [power, setPower] = useState<Power | null>(null);
  const [memory, setMemory] = useState<Memory | undefined>(undefined);
  const [err, setErr] = useState("");
  const load = () =>
    api
      .machine(kitty.id)
      .then((r) => {
        setPower(r.power);
        setMemory(r.memory);
      })
      // A gateway without power control (an older one) shows nothing.
      .catch(() => setPower(null));
  useEffect(() => {
    void load();
    // A machine coming up is looked at more often, so the step it is on shows as it changes.
    const timer = setInterval(() => void load(), power?.state === "starting" ? 2000 : 15000);
    return () => clearInterval(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [kitty.id, kitty.status, power?.state]);
  if (!power) {
    return null;
  }
  const choose = async (value: string) => {
    setErr("");
    try {
      await api.editKitty(
        kitty.id,
        value === "default" ? { auto_off_default: true } : { auto_off_minutes: Number(value) },
      );
      await load();
      onChanged();
    } catch (e) {
      setErr(errText(e));
    }
  };
  const choices = IDLE_CHOICES.includes(power.auto_off_minutes)
    ? IDLE_CHOICES
    : [...IDLE_CHOICES, power.auto_off_minutes].sort((a, b) => (a || 1e9) - (b || 1e9));
  return (
    <div class="mb-3 flex flex-col gap-1.5 text-sm" data-testid="machine-power">
      <div class="flex items-center gap-2 flex-wrap">
        <span class="flex-1 min-w-0" data-testid="machine-state">
          {stateText(power)}
        </span>
        <label class="flex items-center gap-2 text-xs text-[var(--t3)]">
          {t("Turn off after")}
          <select
            class="field !py-1 !text-xs"
            value={power.auto_off_custom ? String(power.auto_off_minutes) : "default"}
            onChange={(e) => void choose((e.target as HTMLSelectElement).value)}
            data-testid="auto-off"
          >
            <option value="default">
              {power.auto_off_custom
                ? t("Default")
                : t("Default ({time})", { time: minutesText(power.auto_off_minutes) })}
            </option>
            {choices.map((n) => (
              <option key={n} value={String(n)}>
                {minutesText(n)}
              </option>
            ))}
          </select>
        </label>
      </div>
      <MachineMemory
        kitty={kitty}
        power={power}
        memory={memory}
        onChanged={() => {
          void load();
          onChanged();
        }}
      />
      <p class="text-xs text-[var(--t4)]">
        {t(
          "{name}'s machine turns itself off after this time without use, never in the middle of a task or while you watch its desktop. {name} knows whether it is on, and turns it on by itself when it needs it.",
          { name: kitty.name },
        )}
      </p>
      {err && (
        <p class="text-sm text-[var(--c-red)]" role="alert">
          {err}
        </p>
      )}
    </div>
  );
}
