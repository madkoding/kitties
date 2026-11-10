import { useState } from "preact/hooks";
import type { Kitty, Memory, Power } from "./api";
import { api, errText } from "./api";
import { t } from "./i18n";

// THE MEMORY OF A KITTY'S MACHINE.
//
// A kitty's machine has so much RAM and swap: its own, or the defaults for every kitty
// (Settings › Machines). Here you see what it uses now and choose its own. The kitty sees the
// same numbers: it does not open browser tabs or programs that do not fit, and is refused when it
// tries with the memory nearly full.

// The sizes offered, in MB.
export const RAM_CHOICES = [1024, 1536, 2048, 3072, 4096, 6144, 8192, 12288, 16384];
export const SWAP_CHOICES = [0, 512, 1024, 2048, 4096, 8192];

// mbText is an amount of memory: MB under a gigabyte, GB above.
export function mbText(n: number): string {
  if (n < 1024) {
    return t("{n} MB", { n });
  }
  const gb = n / 1024;
  return t("{n} GB", { n: Number.isInteger(gb) ? String(gb) : gb.toFixed(1) });
}

// How often the browser visits its open tabs again, in minutes, so sites that end idle sessions
// keep the kitty logged in; tabs someone is using are only visited quietly, never reloaded.
export const REFRESH_CHOICES = [0, 10, 20, 30, 60, 120];

// withValue keeps a value that is not among the choices (set in the file, or by the API).
export function withValue(choices: number[], v: number): number[] {
  return choices.includes(v) ? choices : [...choices, v].sort((a, b) => a - b);
}

// Meter is a bar of how much of something is used.
export function Meter({
  label,
  used,
  total,
  testid,
}: {
  label: string;
  used: number;
  total: number;
  testid: string;
}) {
  const share = total > 0 ? Math.min(1, used / total) : 0;
  const tone = share > 0.9 ? "danger" : share > 0.75 ? "warn" : "ok";
  return (
    <div class="mem-meter" data-testid={testid}>
      <div class="flex justify-between text-xs">
        <span class="text-[var(--t3)]">{label}</span>
        <span class="tabular-nums text-[var(--t2)]">
          {total > 0
            ? t("{used} of {total}", { used: mbText(used), total: mbText(total) })
            : t("None")}
        </span>
      </div>
      <div
        class="mem-bar"
        role="meter"
        aria-valuenow={used}
        aria-valuemin={0}
        aria-valuemax={total}
      >
        <span class={`mem-fill mem-${tone}`} style={{ width: `${Math.round(share * 100)}%` }} />
      </div>
    </div>
  );
}

// MemorySelects are the RAM and swap pickers, each with the default first.
export function MemorySelects({
  ram,
  swap,
  custom,
  defaults,
  onChoose,
  disabled,
}: {
  ram: number;
  swap: number;
  custom: boolean;
  defaults?: { memory_mb: number; swap_mb: number };
  onChoose: (v: { memory_mb: number; swap_mb: number } | "default") => void;
  disabled?: boolean;
}) {
  const pick = (which: "ram" | "swap", value: string) => {
    if (value === "default") {
      onChoose("default");
      return;
    }
    const n = Number(value);
    onChoose(which === "ram" ? { memory_mb: n, swap_mb: swap } : { memory_mb: ram, swap_mb: n });
  };
  return (
    <div class="mem-selects">
      <label class="mem-select">
        <span>{t("RAM")}</span>
        <select
          class="field !py-1 !text-xs"
          value={custom ? String(ram) : "default"}
          disabled={disabled}
          onChange={(e) => pick("ram", (e.target as HTMLSelectElement).value)}
          data-testid="ram"
        >
          {defaults !== undefined && (
            <option value="default">
              {custom ? t("Default") : t("Default ({size})", { size: mbText(ram) })}
            </option>
          )}
          {withValue(RAM_CHOICES, ram).map((n) => (
            <option key={n} value={String(n)}>
              {mbText(n)}
            </option>
          ))}
        </select>
      </label>
      <label class="mem-select">
        <span>{t("Swap")}</span>
        <select
          class="field !py-1 !text-xs"
          value={custom ? String(swap) : "default"}
          disabled={disabled}
          onChange={(e) => pick("swap", (e.target as HTMLSelectElement).value)}
          data-testid="swap"
        >
          {defaults !== undefined && (
            <option value="default">
              {custom
                ? t("Default")
                : t("Default ({size})", { size: swap ? mbText(swap) : t("none") })}
            </option>
          )}
          {withValue(SWAP_CHOICES, swap).map((n) => (
            <option key={n} value={String(n)}>
              {n === 0 ? t("No swap") : mbText(n)}
            </option>
          ))}
        </select>
      </label>
    </div>
  );
}

// MachineMemory shows a machine's memory now and lets you give it RAM and swap of its own.
export default function MachineMemory({
  kitty,
  power,
  memory,
  onChanged,
  compact,
}: {
  kitty: Kitty;
  power: Power;
  memory?: Memory;
  onChanged: () => void;
  compact?: boolean;
}) {
  const [err, setErr] = useState("");
  const [saving, setSaving] = useState(false);
  if (power.memory_mb === undefined) {
    // An older gateway does not size machines.
    return null;
  }
  const choose = async (v: { memory_mb: number; swap_mb: number } | "default") => {
    setErr("");
    setSaving(true);
    try {
      await api.editKitty(kitty.id, v === "default" ? { memory_default: true } : v);
      onChanged();
    } catch (e) {
      setErr(errText(e));
    } finally {
      setSaving(false);
    }
  };
  if (power.memory_local === false) {
    return (
      <p class="text-xs text-[var(--t4)]" data-testid="machine-memory">
        {t("{name}'s machine runs in the cloud: its memory is the size of the machine there.", {
          name: kitty.name,
        })}
      </p>
    );
  }
  return (
    <div class={`mem-panel ${compact ? "mem-compact" : ""}`} data-testid="machine-memory">
      {memory ? (
        <div class="mem-meters">
          <Meter
            label={t("Memory in use")}
            used={memory.used_mb}
            total={memory.total_mb}
            testid="ram-meter"
          />
          <Meter
            label={t("Swap in use")}
            used={memory.swap_used_mb}
            total={memory.swap_total_mb}
            testid="swap-meter"
          />
        </div>
      ) : (
        !compact && (
          <p class="text-xs text-[var(--t4)]">
            {t("Turn the machine on to see the memory it uses.")}
          </p>
        )
      )}
      <MemorySelects
        ram={power.memory_mb}
        swap={power.swap_mb ?? 0}
        custom={Boolean(power.memory_custom)}
        defaults={{ memory_mb: power.memory_mb, swap_mb: power.swap_mb ?? 0 }}
        onChoose={(v) => void choose(v)}
        disabled={saving}
      />
      {memory && !compact && memory.top && memory.top.length > 0 && (
        <p class="text-xs text-[var(--t4)]" data-testid="memory-top">
          {t("Holding the most: {list}", {
            list: memory.top
              .slice(0, 3)
              .map((p) => `${p.name} ${mbText(p.mb)}`)
              .join(" · "),
          })}
        </p>
      )}
      {power.memory_pending && (
        <p class="text-xs text-[var(--c-amber,var(--color-warning))]" data-testid="memory-pending">
          {t("The new memory applies the next time the machine is turned on.")}
        </p>
      )}
      {!compact && (
        <p class="text-xs text-[var(--t4)]">
          {t(
            "{name} sees how much memory is free and does not open tabs or programs that do not fit; it closes what it no longer needs.",
            { name: kitty.name },
          )}
        </p>
      )}
      {err && (
        <p class="text-sm text-[var(--c-red)]" role="alert">
          {err}
        </p>
      )}
    </div>
  );
}
