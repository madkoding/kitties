import { useEffect, useState } from "preact/hooks";
import type { DiskUsage, Retention } from "./api";
import { api, errText } from "./api";
import { bytes } from "./artifacts";
import { t } from "./i18n";

// SETTINGS › PREFERENCES › STORAGE.
//
// Where the disk goes, by area, and how much of what grows by itself is kept: the screenshots the
// kitties take at every step of a desktop task, swept by age and then oldest first to fit a cap.
// The chats, their files and the kitties' folders are the person's and are only counted. "Free up
// space" sweeps now, and deletes the desktop images that updates left behind.

const DAYS = [7, 14, 30, 90, 365, -1];
const MBS = [512, 1024, 2048, 5120, 10240, -1];

function areaLabel(key: string): string {
  switch (key) {
    case "screenshots":
      return t("Screenshots of the tasks");
    case "attachments":
      return t("Files attached to chats");
    case "chats":
      return t("Chat history");
    case "homes":
      return t("Kitties' homes (settings and programs)");
    case "work":
      return t("Kitties' workspaces (files and browser)");
    case "shared":
      return t("Shared folders");
    case "backups":
      return t("Backups of the sessions");
    case "journal":
      return t("Journals");
    case "whatsapp":
      return t("WhatsApp");
    case "logs":
      return t("Logs");
    default:
      return t("Other");
  }
}

export default function StorageSettings() {
  const [view, setView] = useState<DiskUsage | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [note, setNote] = useState("");
  useEffect(() => {
    api
      .storage()
      .then(setView)
      // A gateway that cannot tell (an older one) shows nothing.
      .catch(() => setView(null));
  }, []);
  if (!view) {
    return null;
  }
  const done = (v: DiskUsage) => {
    setView(v);
    const l = v.last;
    if (!l) {
      return;
    }
    let msg = l.shots
      ? t("Freed {size} ({n} screenshots).", { size: bytes(l.freed), n: l.shots })
      : t("No screenshot was old enough to go.");
    if (l.images && l.images !== "0B") {
      msg += " " + t("Old desktop images: {size}.", { size: l.images });
    }
    setNote(msg);
    if (l.error) {
      setErr(l.error);
    }
  };
  const run = async (fn: () => Promise<DiskUsage>) => {
    setBusy(true);
    setErr("");
    setNote("");
    try {
      done(await fn());
    } catch (e) {
      setErr(errText(e));
    } finally {
      setBusy(false);
    }
  };
  const r = view.retention;
  const days = r.shot_days || view.defaults.shot_days;
  const mb = r.shots_mb || view.defaults.shots_mb;
  const set = (next: Partial<Retention>) =>
    void run(() => api.setRetention({ shot_days: days, shots_mb: mb, ...next }));
  const max = Math.max(1, ...view.areas.map((a) => a.bytes));
  return (
    <div class="mt-6 pt-4 border-t border-white/5" data-testid="storage">
      <div class="section-title !mx-0">{t("Storage")}</div>
      <p class="text-xs text-[var(--t3)] mb-2" data-testid="storage-total">
        {view.free
          ? t("Kitties takes {total} in {dir}; {free} free on that disk.", {
              total: bytes(view.total),
              dir: view.dir,
              free: bytes(view.free),
            })
          : t("Kitties takes {total} in {dir}.", { total: bytes(view.total), dir: view.dir })}
      </p>
      <div class="flex flex-col gap-1">
        {view.areas
          .filter((a) => a.bytes > 0)
          .map((a) => (
            <div key={a.key} class="text-xs" data-testid={`storage-${a.key}`}>
              <div class="flex justify-between gap-2">
                <span class="truncate">
                  {areaLabel(a.key)}
                  {a.count ? <span class="text-[var(--t4)]"> · {a.count}</span> : null}
                </span>
                <span class="font-mono text-[var(--t3)]">{bytes(a.bytes)}</span>
              </div>
              <div class="h-1 rounded bg-white/5 mt-0.5">
                <div
                  class="h-1 rounded bg-[rgb(var(--accent-rgb))]"
                  style={{ width: `${Math.max(1, Math.round((a.bytes / max) * 100))}%` }}
                />
              </div>
            </div>
          ))}
      </div>
      <div class="grid grid-cols-2 gap-2 mt-3">
        <label class="text-xs text-[var(--t3)]">
          {t("Keep screenshots for")}
          <select
            class="w-full px-3 py-2 text-sm mt-1"
            value={String(days)}
            disabled={busy}
            data-testid="retention-days"
            onChange={(e) => set({ shot_days: Number((e.target as HTMLSelectElement).value) })}
          >
            {DAYS.map((d) => (
              <option key={d} value={String(d)}>
                {d < 0 ? t("Forever") : t("{n} days", { n: d })}
              </option>
            ))}
          </select>
        </label>
        <label class="text-xs text-[var(--t3)]">
          {t("At most, all together")}
          <select
            class="w-full px-3 py-2 text-sm mt-1"
            value={String(mb)}
            disabled={busy}
            data-testid="retention-mb"
            onChange={(e) => set({ shots_mb: Number((e.target as HTMLSelectElement).value) })}
          >
            {MBS.map((m) => (
              <option key={m} value={String(m)}>
                {m < 0 ? t("No limit") : bytes(m * 1024 * 1024)}
              </option>
            ))}
          </select>
        </label>
      </div>
      <p class="text-xs text-[var(--t4)] mt-1">
        {t(
          "Older screenshots go by themselves every few hours; their steps stay in the chat. Chats, attached files and the kitties' folders are yours and are never deleted.",
        )}
      </p>
      <button
        class="btn-outline px-3 py-1.5 text-xs mt-2"
        disabled={busy}
        data-testid="storage-clean"
        onClick={() => void run(api.cleanStorage)}
      >
        {busy ? t("Freeing up…") : t("Free up space now")}
      </button>
      {note && <p class="text-xs text-[var(--c-green)] mt-1">{note}</p>}
      {err && (
        <p class="text-xs text-[var(--c-red)] mt-1" role="alert">
          {err}
        </p>
      )}
    </div>
  );
}
