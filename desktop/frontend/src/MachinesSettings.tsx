import { useEffect, useState } from "preact/hooks";
import type { Kitty, Machines, Memory, Power } from "./api";
import { api, errText } from "./api";
import { t } from "./i18n";
import MachineMemory, { MemorySelects, mbText } from "./MachineMemory";

// SETTINGS › MACHINES.
//
// The RAM and swap every kitty's machine gets unless it has its own, and each kitty's machine
// with what it uses now. A change reaches a machine that exists at once when the container engine
// can; otherwise the next time it is turned on. The machines that are on share the engine's
// memory, so their sum should fit in it.

function KittyMachine({ kitty }: { kitty: Kitty }) {
  const [info, setInfo] = useState<{ power: Power; memory?: Memory } | null>(null);
  const load = () =>
    api
      .machine(kitty.id)
      .then(setInfo)
      .catch(() => setInfo(null));
  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [kitty.id, kitty.status]);
  if (!info) {
    return null;
  }
  return (
    <div class="mem-row" data-testid={`machine-${kitty.name}`}>
      <div class="flex items-center gap-2 min-w-0">
        <span class="font-medium truncate">{kitty.name}</span>
        <span class="tag">{info.power.state === "on" ? t("On") : t("Off")}</span>
      </div>
      <MachineMemory
        kitty={kitty}
        power={info.power}
        memory={info.memory}
        onChanged={() => void load()}
        compact
      />
    </div>
  );
}

export default function MachinesSettings({ kitties }: { kitties: Kitty[] }) {
  const [view, setView] = useState<Machines | null>(null);
  const [err, setErr] = useState("");
  const [saved, setSaved] = useState(false);
  // n changes after the defaults change, so each kitty shows what it gets now.
  const [n, setN] = useState(0);
  useEffect(() => {
    api
      .machines()
      .then(setView)
      .catch((e) => setErr(errText(e)));
  }, []);
  const desks = kitties.filter((k) => k.desktop);
  const save = async (v: { memory_mb: number; swap_mb: number } | "default") => {
    if (v === "default") {
      return;
    }
    setErr("");
    setSaved(false);
    try {
      setView(await api.setMachines(v));
      setSaved(true);
      setN((x) => x + 1);
    } catch (e) {
      setErr(errText(e));
    }
  };
  const sum = view ? desks.length * view.memory_mb : 0;
  return (
    <div data-testid="machines" class="flex flex-col gap-3">
      <p class="text-sm text-[var(--t3)]">
        {t(
          "Each kitty's desktop runs on a machine of its own with so much RAM and swap. Kitties see how much is free and do not open browser tabs or programs that do not fit.",
        )}
      </p>
      {view && (
        <div class="form-card flex flex-col gap-2">
          <div class="section-title !m-0">{t("For every kitty")}</div>
          <MemorySelects
            ram={view.memory_mb}
            swap={view.swap_mb}
            custom
            onChoose={(v) => void save(v)}
          />
          {view.engine_mb ? (
            <p
              class={`text-xs ${sum > view.engine_mb ? "text-[var(--c-amber)]" : "text-[var(--t4)]"}`}
              data-testid="engine-memory"
            >
              {t(
                "The container engine has {engine} in all; the machines that are on share it. {n} machines of {size} add up to {sum}.",
                {
                  engine: mbText(view.engine_mb),
                  n: desks.length,
                  size: mbText(view.memory_mb),
                  sum: mbText(sum),
                },
              )}
            </p>
          ) : null}
          <p class="text-xs text-[var(--t4)]">
            {t(
              "A change reaches a machine at once when the engine can; otherwise the next time it is turned on. A machine in the cloud has the size of its provider.",
            )}
          </p>
          {saved && <p class="text-xs text-[var(--c-green)]">{t("Saved.")}</p>}
        </div>
      )}
      {err && (
        <p class="text-sm text-[var(--c-red)]" role="alert">
          {err}
        </p>
      )}
      {desks.length > 0 && (
        <div class="flex flex-col gap-2">
          <div class="section-title !m-0">{t("Each kitty")}</div>
          {desks.map((k) => (
            <KittyMachine key={`${k.id}-${n}`} kitty={k} />
          ))}
        </div>
      )}
    </div>
  );
}
