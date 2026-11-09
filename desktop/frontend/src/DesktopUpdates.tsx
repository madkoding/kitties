import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import type { DeskJobItem, DesktopFleet, FleetDesktop } from "./api";
import { api, errText } from "./api";
import { when } from "./format";
import { t } from "./i18n";
import { Modal } from "./Modal";
import { ProgressBar } from "./Updates";

// THE DESKTOPS OF ALL THE KITTIES, ON A NEWER IMAGE.
//
// A new desktop image is news for every kitty whose desktop is a container on this computer. The
// gateway looks for it now and then (and when asked here), and the sidebar says how many desktops
// are of an older one. The window (DesktopUpdatesModal) lists them, updates all or the ones picked,
// one after the other, and shows how each one goes: the busy and the held are left for later, a
// stopped one gets the new desktop when it starts. What each kitty keeps (its home and programs,
// its browser's profile, its Shared folder) stays on this computer; its cookies, sessions and
// passwords are backed up and put back.

// NEWS_EVERY is how often the page reads what the gateway found; FOLLOW_EVERY how often while a
// look or an update runs.
const NEWS_EVERY = 30 * 60 * 1000;
const FOLLOW_EVERY = 800;

// working is whether the gateway is looking for a new image or updating desktops.
function working(f: DesktopFleet | null): boolean {
  return !!f && (f.checking || (!!f.job && !f.job.finished));
}

// useDesktopNews is the news of the desktops' image, read when the page opens, now and then, and
// when the window comes back after a while; null when the gateway cannot update desktops.
export function useDesktopNews(enabled: boolean): [DesktopFleet | null, () => void] {
  const [news, setNews] = useState<DesktopFleet | null>(null);
  const last = useRef(0);
  const read = useCallback(() => {
    last.current = Date.now();
    api
      .desktopUpdates()
      .then(setNews)
      .catch(() => setNews(null));
  }, []);
  useEffect(() => {
    if (!enabled) {
      return;
    }
    read();
    const id = setInterval(read, NEWS_EVERY);
    const back = () => Date.now() - last.current > 5 * 60 * 1000 && read();
    addEventListener("focus", back);
    return () => {
      clearInterval(id);
      removeEventListener("focus", back);
    };
  }, [enabled, read]);
  return [news, read];
}

// stepText is what the update of the desktops is doing, for people.
function stepText(f: DesktopFleet): string {
  const j = f.job;
  if (f.checking) {
    return t("Looking for a new desktop (what changed is downloaded)…");
  }
  if (!j) {
    return "";
  }
  if (j.phase === "download") {
    return t("Downloading the new desktop…");
  }
  if (j.phase === "update") {
    return t("Updating desktop {done} of {total}…", {
      done: Math.min(j.done + 1, j.total),
      total: j.total,
    });
  }
  const failed = j.kitties.filter((k) => k.state === "failed").length;
  const later = j.kitties.filter((k) => k.state === "skipped").length;
  let out = failed
    ? t("Done, with {count} that could not be updated.", { count: failed })
    : t("Done.");
  if (later) {
    out += " " + t("{count} left for later.", { count: later });
  }
  if (j.freed) {
    out += " " + t("The old desktop was deleted ({freed} freed).", { freed: j.freed });
  }
  return out;
}

// stateTag is how one desktop fares, as a tag.
function stateTag(s: DeskJobItem["state"]): { text: string; tone: string } {
  switch (s) {
    case "updating":
      return { text: t("updating"), tone: "tag-accent" };
    case "done":
      return { text: t("updated"), tone: "tag-green" };
    case "on_start":
      return { text: t("at its next start"), tone: "tag-green" };
    case "skipped":
      return { text: t("later"), tone: "tag-amber" };
    case "failed":
      return { text: t("failed"), tone: "tag-red" };
    default:
      return { text: t("waiting"), tone: "" };
  }
}

function busyText(d: FleetDesktop): string {
  return d.busy === "held"
    ? t("you have its desktop")
    : d.busy === "working"
      ? t("working on a task")
      : "";
}

// DesktopUpdatesModal lists the desktops on this computer, looks for a new image, and updates all
// of those of an older one, or the ones picked, with the progress of each.
export function DesktopUpdatesModal({
  initial,
  onClose,
  onChanged,
}: {
  initial?: DesktopFleet | null;
  onClose: () => void;
  onChanged?: () => void;
}) {
  const [f, setF] = useState<DesktopFleet | null>(initial || null);
  const [picked, setPicked] = useState<Set<string> | null>(null);
  const [err, setErr] = useState("");
  const [sending, setSending] = useState(false);
  const was = useRef(working(initial || null));
  const read = useCallback(async () => {
    try {
      const next = await api.desktopUpdates();
      setF(next);
      // A look or an update that ended changes the kitties (their status, the sidebar's news).
      if (was.current && !working(next)) {
        onChanged?.();
      }
      was.current = working(next);
    } catch (e) {
      setErr(errText(e));
    }
  }, [onChanged]);
  useEffect(() => {
    void read();
  }, [read]);
  const busy = working(f) || sending;
  useEffect(() => {
    if (!working(f)) {
      return;
    }
    const id = setInterval(read, FOLLOW_EVERY);
    return () => clearInterval(id);
  }, [f, read]);
  // Escape is for this window only, and does nothing while desktops are being updated.
  useLayoutEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopImmediatePropagation();
        if (!busy) {
          onClose();
        }
      }
    };
    addEventListener("keydown", onKey, true);
    return () => removeEventListener("keydown", onKey, true);
  }, [busy, onClose]);
  const send = async (fn: () => Promise<DesktopFleet>) => {
    setErr("");
    setSending(true);
    try {
      const next = await fn();
      was.current = true;
      setF(next);
      setPicked(null);
    } catch (e) {
      setErr(errText(e));
    } finally {
      setSending(false);
    }
  };
  const stale = (f?.kitties || []).filter((k) => k.update_available);
  // What is picked: every desktop of an older image until the person picks.
  const chosen = picked || new Set(stale.map((k) => k.id));
  const toggle = (id: string) => {
    const next = new Set(chosen);
    if (next.has(id)) {
      next.delete(id);
    } else {
      next.add(id);
    }
    setPicked(next);
  };
  const job = f?.job;
  const items = new Map((job?.kitties || []).map((k) => [k.id, k]));
  const all = chosen.size === stale.length;
  return (
    <Modal title={t("Update the desktops")} onClose={onClose} closable={!busy}>
      <div data-testid="desktops-modal">
        {!f ? (
          <p class="text-sm flex items-center gap-2">
            <span class="spinner" /> {t("Loading…")}
          </p>
        ) : (
          <>
            <p class="text-sm" data-testid="desktops-summary">
              {f.available > 0
                ? t("A newer desktop is ready for {count} of your kitties.", {
                    count: f.available,
                  })
                : f.kitties.length
                  ? t("Every desktop on this computer is on the newest image.")
                  : t("No kitty has a desktop on this computer.")}
            </p>
            <p class="text-xs text-[var(--t4)] mt-1 font-mono truncate" title={f.image}>
              {f.image}
              {f.checked_at && <> · {t("looked {when}", { when: when(f.checked_at) })}</>}
            </p>
            {f.kitties.length > 0 && (
              <ul class="mt-3 flex flex-col gap-1" data-testid="desktops-list">
                {f.kitties.map((d) => {
                  const item = items.get(d.id);
                  const tag = item && stateTag(item.state);
                  const note = item?.error || (!item && busyText(d));
                  return (
                    <li
                      key={d.id}
                      class="flex items-center gap-2 text-sm px-2 py-1.5 rounded-lg bg-[rgb(var(--ov-rgb)/0.04)]"
                      data-testid={`desktops-row-${d.name}`}
                    >
                      <input
                        type="checkbox"
                        aria-label={t("Update {name}", { name: d.name })}
                        checked={d.update_available && chosen.has(d.id)}
                        disabled={!d.update_available || busy}
                        onChange={() => toggle(d.id)}
                      />
                      <span class="flex-1 min-w-0">
                        <span class="truncate block">{d.name}</span>
                        {note && (
                          <span
                            class={`block text-xs ${item?.state === "failed" ? "text-[var(--c-red)]" : "text-[var(--t3)]"}`}
                          >
                            {note}
                          </span>
                        )}
                      </span>
                      {item?.state === "updating" && <span class="spinner" />}
                      {tag ? (
                        <span class={`tag ${tag.tone}`}>{tag.text}</span>
                      ) : d.update_available ? (
                        <span class="tag tag-accent">{t("new version")}</span>
                      ) : (
                        <span class="tag">{t("up to date")}</span>
                      )}
                    </li>
                  );
                })}
              </ul>
            )}
            {(working(f) || job) && (
              <div class="mt-4" data-testid="desktops-progress">
                {job && !f.checking && (
                  <ProgressBar
                    percent={job.percent}
                    failed={!!job.finished && job.kitties.some((k) => k.state === "failed")}
                    pulse={job.phase === "download"}
                    label={t("Update the desktops")}
                  />
                )}
                <p
                  class="text-sm mt-2 flex items-center gap-2"
                  role="status"
                  data-testid="desktops-step"
                >
                  {working(f) && <span class="spinner" />}
                  {stepText(f)}
                  {job && !job.finished && !f.checking && (
                    <span class="ml-auto text-xs text-[var(--t3)]">{job.percent}%</span>
                  )}
                </p>
              </div>
            )}
            {f.check_error && !working(f) && (
              <p class="text-xs text-[var(--c-amber)] mt-2" data-testid="desktops-check-error">
                {t("The last look for a new desktop failed: {error}", { error: f.check_error })}
              </p>
            )}
            {stale.length > 0 && !busy && (
              <p class="text-xs text-[var(--t3)] mt-3">
                {t(
                  "Each desktop restarts. Its files, settings, programs, browser profile and Shared folder stay on this computer; its cookies, sessions and passwords are backed up and put back. A stopped kitty gets the new desktop when it starts; a kitty at work, or whose desktop you have, is left for later.",
                )}
              </p>
            )}
          </>
        )}
        {err && (
          <p class="text-sm text-[var(--c-red)] mt-2" role="alert">
            {err}
          </p>
        )}
        <div class="flex justify-end gap-2 mt-5 flex-wrap">
          <button
            class="btn-ghost px-3 py-1.5 text-sm mr-auto"
            disabled={busy || !f}
            onClick={() => send(api.checkDesktops)}
            data-testid="desktops-check"
          >
            {t("Look for a new desktop")}
          </button>
          {!busy && (
            <button class="btn-ghost px-3 py-1.5 text-sm" onClick={onClose}>
              {stale.length ? t("Later") : t("Close")}
            </button>
          )}
          {stale.length > 0 && (
            <button
              class="btn-accent px-3 py-1.5 text-sm"
              disabled={busy || chosen.size === 0}
              onClick={() => send(() => api.updateDesktops(all ? undefined : [...chosen]))}
              data-testid="desktops-update"
            >
              {all
                ? t("Update all ({count})", { count: stale.length })
                : t("Update {count}", { count: chosen.size })}
            </button>
          )}
        </div>
      </div>
    </Modal>
  );
}

// DesktopUpdates is the part of Settings › Updates about the kitties' desktops.
export function DesktopUpdates() {
  const [news, read] = useDesktopNews(true);
  const [open, setOpen] = useState(false);
  if (!news) {
    return null;
  }
  return (
    <div class="mt-4" data-testid="desktop-updates">
      <p class="text-sm">
        {t("Kitties' desktops")}
        <span class="text-[var(--t3)]">
          {" · "}
          {news.available > 0
            ? t("a newer one for {count}", { count: news.available })
            : t("all on the newest image")}
        </span>
      </p>
      <div class="flex items-center gap-2 mt-2 flex-wrap">
        <button
          class={`${news.available > 0 ? "btn-accent" : "btn-ghost"} px-2 py-1 text-xs`}
          onClick={() => setOpen(true)}
          data-testid="desktop-updates-open"
        >
          {news.available > 0 ? t("Update the desktops") : t("See the desktops")}
        </button>
        {news.checked_at && (
          <span class="text-xs text-[var(--t4)]">
            {t("Looked {when}.", { when: when(news.checked_at) })}
          </span>
        )}
      </div>
      <p class="text-xs text-[var(--t4)] mt-2">
        {news.auto_update
          ? t(
              "Kitties looks for a new desktop and puts it on each kitty by itself when it is idle; update them all now from here.",
            )
          : t("Kitties looks for a new desktop and tells you; you decide when to update them.")}
      </p>
      {open && (
        <DesktopUpdatesModal initial={news} onClose={() => setOpen(false)} onChanged={read} />
      )}
    </div>
  );
}
