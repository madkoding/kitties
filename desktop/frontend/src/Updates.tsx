import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import type { UpdateProgress, UpdateStatus } from "./api";
import { api, errText, openExternal } from "./api";
import { when } from "./format";
import { t } from "./i18n";
import { Modal } from "./Modal";

// THE VERSION OF KITTIES.
//
// Which version runs, whether a newer one is out, and the way to move to it: a gateway that may
// replace itself does it from here (and restarts: the desktops of the kitties keep running); one
// that may not (in a container, inside the app, a development build) says how it is updated.
// Whether it looks by itself, and installs by itself, is `updates:` in the configuration.
//
// The gateway looks for new versions by itself (updates.check_hours); the page reads what it found
// now and then (useUpdateNews), shows the news in the sidebar, and the update is made in a window
// (UpdateModal): what changed, the update with its progress, the restart, and the page reloaded.

// NEWS_EVERY is how often the page reads what the gateway found.
const NEWS_EVERY = 30 * 60 * 1000;

// useUpdateNews is the version of Kitties as the gateway last found it, read when the page opens,
// now and then, and when the window comes back after a while; null when there is no news to read.
export function useUpdateNews(enabled: boolean): UpdateStatus | null {
  const [st, setSt] = useState<UpdateStatus | null>(null);
  const last = useRef(0);
  const read = useCallback(() => {
    last.current = Date.now();
    api
      .updateStatus()
      .then(setSt)
      // A gateway that does not look for new versions (an older one) has no news.
      .catch(() => setSt(null));
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
  return st;
}

// size is a number of bytes for people.
function size(n: number): string {
  return n >= 1 << 20
    ? `${(n / (1 << 20)).toFixed(1)} MB`
    : `${Math.max(1, Math.round(n / 1024))} KB`;
}

// stepText is what the update is doing, for people.
function stepText(p: UpdateProgress): string {
  switch (p.phase) {
    case "download":
      return p.total
        ? t("Downloading {done} of {total}…", { done: size(p.done || 0), total: size(p.total) })
        : t("Downloading…");
    case "verify":
      return t("Checking the download…");
    case "install":
      return t("Putting the new version in place…");
    case "restart":
      return t("Restarting Kitties…");
    default:
      return t("Getting ready…");
  }
}

type Stage = "offer" | "working" | "restarting" | "back" | "failed";

// UpdateModal offers the newer version and makes the update: its progress, the restart of the
// gateway, and the page reloaded on the new version.
export function UpdateModal({ status, onClose }: { status: UpdateStatus; onClose: () => void }) {
  const [stage, setStage] = useState<Stage>("offer");
  const [progress, setProgress] = useState<UpdateProgress | null>(null);
  const [err, setErr] = useState("");
  const timer = useRef<ReturnType<typeof setInterval> | null>(null);
  const stop = () => {
    if (timer.current) {
      clearInterval(timer.current);
      timer.current = null;
    }
  };
  useEffect(() => stop, []);
  const failed = (text: string) => {
    stop();
    setErr(text);
    setStage("failed");
  };
  // The gateway restarts a moment after it is done: the page waits for the new version to answer
  // and reloads on it.
  const follow = (to: string) => {
    stop();
    setStage("restarting");
    setProgress((p) => ({ ...(p || { to }), to, phase: "restart", percent: 100 }));
    let tries = 0;
    timer.current = setInterval(async () => {
      tries++;
      try {
        const h = await api.health();
        if (h.version && h.version !== status.current) {
          stop();
          setStage("back");
          setTimeout(() => location.reload(), 600);
          return;
        }
      } catch {
        /* it is restarting */
      }
      if (tries > 90) {
        failed(t("Kitties did not come back in a minute: restart it by hand."));
      }
    }, 1000);
  };
  // While it installs, the page reads how far it has gone.
  const watch = (to: string) => {
    let misses = 0;
    timer.current = setInterval(async () => {
      try {
        const st = await api.updateStatus();
        misses = 0;
        if (st.current !== status.current) {
          follow(st.current);
          return;
        }
        const p = st.progress;
        if (!p) {
          return;
        }
        setProgress(p);
        if (p.phase === "failed") {
          failed(p.error || t("The update did not finish."));
        } else if (p.phase === "restart") {
          follow(p.to || to);
        }
      } catch {
        // No answer: the gateway is already restarting into the new version.
        if (++misses > 2) {
          follow(to);
        }
      }
    }, 400);
  };
  const start = async () => {
    setErr("");
    setStage("working");
    setProgress({ to: status.latest || "", phase: "check", percent: 2 });
    try {
      const r = await api.applyUpdate();
      if (r.updated_to) {
        // An older gateway installs before it answers, and restarts now.
        follow(r.updated_to);
      } else {
        watch(r.updating_to || status.latest || "");
      }
    } catch (e) {
      failed(errText(e));
    }
  };
  const busy = stage === "working" || stage === "restarting" || stage === "back";
  // Escape is for this window only, not for Settings behind it (taken before Settings sees it),
  // and does nothing while the update is being made.
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
  const pct = progress?.percent ?? 0;
  return (
    <Modal title={t("Update Kitties")} onClose={onClose} closable={!busy}>
      <div data-testid="update-modal">
        <p class="text-sm">
          {t("Kitties {version} is available.", { version: status.latest || "" })}{" "}
          <span class="text-[var(--t3)]">
            {t("You have {version}.", { version: status.current })}
          </span>
        </p>
        {stage === "offer" && !!status.changes?.length && (
          <div class="mt-3">
            <div class="text-xs text-[var(--t3)] uppercase tracking-wider font-mono mb-1">
              {t("What's new")}
            </div>
            <ul
              class="text-sm list-disc pl-5 space-y-1 max-h-48 overflow-y-auto scroll-thin"
              data-testid="update-changes"
            >
              {status.changes.map((c) => (
                <li key={c}>{c}</li>
              ))}
            </ul>
          </div>
        )}
        {stage === "offer" && status.url && (
          <button
            class="plain text-xs text-[var(--color-accent)] mt-2 underline"
            onClick={() => openExternal(status.url || "")}
          >
            {t("See the release notes")}
          </button>
        )}
        {stage === "offer" && !status.can_apply && status.why && (
          <p class="callout callout-warn text-sm mt-3" data-testid="update-why">
            {status.why}
          </p>
        )}
        {stage === "offer" && status.can_apply && !status.signed && (
          <p class="text-xs text-[var(--c-amber)] mt-3" data-testid="update-unsigned">
            {t(
              "This update is checked against its checksums only, which do not say who made it: set updates.public_key to require the release key's signature.",
            )}
          </p>
        )}
        {stage === "offer" && status.can_apply && (
          <p class="text-xs text-[var(--t3)] mt-3">
            {t(
              "Kitties restarts when it is done; the kitties' desktops keep running and this window refreshes by itself.",
            )}
          </p>
        )}
        {stage !== "offer" && (
          <div class="mt-4" data-testid="update-progress">
            <div
              class="h-2 rounded-full overflow-hidden bg-[rgb(var(--ov-rgb)/0.08)]"
              role="progressbar"
              aria-valuemin={0}
              aria-valuemax={100}
              aria-valuenow={pct}
              aria-label={t("Update Kitties")}
            >
              <div
                class={`h-full rounded-full transition-[width] duration-300 ${stage === "failed" ? "bg-[var(--c-red)]" : "bg-[var(--color-accent)]"} ${stage === "restarting" ? "animate-pulse" : ""}`}
                style={{ width: `${Math.max(2, pct)}%` }}
              />
            </div>
            <p class="text-sm mt-2 flex items-center gap-2" role="status" data-testid="update-step">
              {busy && <span class="spinner" />}
              {stage === "back"
                ? t("Kitties {version} is running: refreshing the window…", {
                    version: progress?.to || "",
                  })
                : stage === "failed"
                  ? t("The update did not finish.")
                  : progress && stepText(progress)}
              {stage === "working" && <span class="ml-auto text-xs text-[var(--t3)]">{pct}%</span>}
            </p>
          </div>
        )}
        {err && (
          <p class="text-sm text-[var(--c-red)] mt-2" role="alert">
            {err}
          </p>
        )}
        <div class="flex justify-end gap-2 mt-5">
          {!busy && (
            <button class="btn-ghost px-3 py-1.5 text-sm" onClick={onClose}>
              {stage === "offer" ? t("Later") : t("Close")}
            </button>
          )}
          {stage === "offer" && !status.can_apply && status.url && (
            <button
              class="btn-accent px-3 py-1.5 text-sm"
              onClick={() => openExternal(status.url || "")}
            >
              {t("Download")}
            </button>
          )}
          {(stage === "offer" || stage === "failed") && status.can_apply && (
            <button class="btn-accent px-3 py-1.5 text-sm" onClick={start} data-testid="update-now">
              {stage === "failed" ? t("Try again") : t("Update now")}
            </button>
          )}
        </div>
      </div>
    </Modal>
  );
}

export default function Updates() {
  const [st, setSt] = useState<UpdateStatus | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const [open, setOpen] = useState(false);
  useEffect(() => {
    api
      .updateStatus()
      .then(setSt)
      // A gateway that does not look for new versions (an older one) shows nothing.
      .catch(() => setSt(null));
  }, []);
  if (!st) {
    return null;
  }
  const check = async () => {
    setBusy(true);
    setErr("");
    try {
      setSt(await api.updateStatus(true));
    } catch (e) {
      setErr(errText(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div class="mt-6 pt-4 border-t border-white/5" data-testid="updates">
      <div class="section-title !mx-0">{t("Updates")}</div>
      <p class="text-sm">
        {t("Kitties {version}", { version: st.current })}
        {st.latest && !st.available && (
          <span class="text-[var(--t3)]"> · {t("it is the newest version")}</span>
        )}
      </p>
      {st.available && (
        <div class="callout mt-2 flex items-center gap-2 flex-wrap" data-testid="update-available">
          <span class="flex-1">
            {t("Kitties {version} is available.", { version: st.latest || "" })}
            {!st.can_apply && st.why && (
              <span class="block text-xs text-[var(--t3)] mt-1">{st.why}</span>
            )}
          </span>
          <button
            class="btn-accent px-3 py-1.5 text-sm"
            disabled={busy}
            onClick={() => setOpen(true)}
            data-testid="update-apply"
          >
            {st.can_apply ? t("Update and restart") : t("See what's new")}
          </button>
        </div>
      )}
      {st.available && st.can_apply && !st.signed && (
        <p class="text-xs text-[var(--c-amber)] mt-1" data-testid="update-unsigned">
          {t(
            "This update is checked against its checksums only, which do not say who made it: set updates.public_key to require the release key's signature.",
          )}
        </p>
      )}
      {st.skipped && (
        <p class="text-xs text-[var(--t3)] mt-2" data-testid="update-skipped">
          {t(
            "{version} did not hold here and Kitties went back to {current}: it is not offered again.",
            {
              version: st.skipped,
              current: st.current,
            },
          )}
        </p>
      )}
      <div class="flex items-center gap-2 mt-2 flex-wrap">
        <button
          class="btn-ghost px-2 py-1 text-xs"
          disabled={busy || st.mode === "off"}
          onClick={check}
          data-testid="update-check"
        >
          {t("Look for a new version")}
        </button>
        {st.checked_at && (
          <span class="text-xs text-[var(--t4)]">
            {t("Looked {when}.", { when: when(st.checked_at) })}
          </span>
        )}
      </div>
      <p class="text-xs text-[var(--t4)] mt-2">
        {st.mode === "off"
          ? t(
              "Kitties does not look for new versions: turn it on with updates.gateway in the configuration.",
            )
          : st.mode === "auto"
            ? t(
                "Kitties looks for new versions and installs them by itself when nothing is in use.",
              )
            : t("Kitties looks for new versions and tells you; you decide when to update.")}
      </p>
      {(st.error || err) && (
        <p class="text-sm text-[var(--c-red)] mt-2" role="alert">
          {err || st.error}
        </p>
      )}
      {open && <UpdateModal status={st} onClose={() => setOpen(false)} />}
    </div>
  );
}
