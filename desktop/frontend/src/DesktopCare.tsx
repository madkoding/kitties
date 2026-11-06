import { useEffect, useState } from "preact/hooks";
import type { DesktopInfo, Kitty, Storage } from "./api";
import { api, errText, importKitty, shotURL } from "./api";
import { ask } from "./Confirm";
import { when } from "./format";
import { t } from "./i18n";
import { bytes } from "./artifacts";

// LOOKING AFTER A KITTY'S DESKTOP.
//
// A desktop is made from an image, and a newer one can come. Updating it makes the container anew,
// so what the kitty's browser knows (cookies, sessions, saved passwords) and the keys of its
// programs are backed up first, sealed and away from the container, and put back into the new one.
// The backup is also made whenever the desktop stops, and here on request.
//
// What the kitty made and installed is not in the container at all: its home (settings, its own
// programs), its workspace and its Shared folder are folders of this computer, which no update
// deletes. They can be exported (without keys and passwords) and put back.

export default function DesktopCare({ kitty, onChanged }: { kitty: Kitty; onChanged: () => void }) {
  const [info, setInfo] = useState<DesktopInfo | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const load = () =>
    api
      .desktopInfo(kitty.id)
      .then(setInfo)
      // A gateway that cannot look after desktops (an older one) shows nothing.
      .catch(() => setInfo(null));
  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [kitty.id, kitty.status]);
  if (!info) {
    return null;
  }
  const act = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    setErr("");
    try {
      await fn();
      await load();
      onChanged();
    } catch (e) {
      setErr(errText(e));
    } finally {
      setBusy(false);
    }
  };
  const update = async () => {
    const yes = await ask(
      t(
        "Update {name}'s desktop? It restarts. Its files, settings and programs stay; its cookies, sessions and saved passwords are backed up first and put back in the new one.",
        { name: kitty.name },
      ),
      t("Update"),
    );
    if (yes) {
      void act(() => api.updateKitty(kitty.id));
    }
  };
  return (
    <div class="mt-3 flex flex-col gap-2 text-sm" data-testid="desktop-care">
      {info.update_available && (
        <div class="callout flex items-center gap-2" data-testid="update-callout">
          <span class="flex-1">
            {t("A newer desktop is available for {name}.", { name: kitty.name })}
          </span>
          <button
            class="btn-accent px-3 py-1.5 text-sm"
            disabled={busy || kitty.status === "starting"}
            onClick={update}
            data-testid="update-desktop"
          >
            {t("Update the desktop")}
          </button>
        </div>
      )}
      <div class="flex items-center gap-2 flex-wrap">
        <span class="text-xs text-[var(--t3)] flex-1" data-testid="backup-state">
          {info.backup
            ? t("Cookies, sessions and passwords backed up {when}.", {
                when: when(info.backup.saved),
              })
            : t("Cookies, sessions and passwords are not backed up yet.")}
        </span>
        <button
          class="btn-ghost px-2 py-1 text-xs"
          disabled={busy || kitty.status !== "running"}
          onClick={() => act(() => api.backupKitty(kitty.id))}
          data-testid="backup-now"
        >
          {t("Back up now")}
        </button>
      </div>
      {info.auto_update && (
        <p class="text-xs text-[var(--t3)]" data-testid="auto-update">
          {t(
            "Newer desktops are downloaded and installed by themselves when {name} is idle; its cookies, sessions and passwords come along.",
            { name: kitty.name },
          )}
        </p>
      )}
      <p class="text-xs text-[var(--t4)]">
        {t(
          "The backup is sealed and kept away from the desktop, and goes back into it when it is made anew. It is also made when the desktop stops.",
        )}
      </p>
      {info.storage && (
        <KeptFiles
          kitty={kitty}
          storage={info.storage}
          busy={busy}
          onImport={(file) => act(() => importKitty(kitty.id, file))}
        />
      )}
      {err && (
        <p class="text-sm text-[var(--c-red)]" role="alert">
          {err}
        </p>
      )}
    </div>
  );
}

// KeptFiles says what the kitty keeps on this computer, that updates never delete it, and offers
// to export it or put an export back.
function KeptFiles({
  kitty,
  storage,
  busy,
  onImport,
}: {
  kitty: Kitty;
  storage: Storage;
  busy: boolean;
  onImport: (file: File) => void;
}) {
  const quota = storage.shared_quota_mb;
  const pick = async (e: Event) => {
    const input = e.target as HTMLInputElement;
    const file = input.files?.[0];
    input.value = "";
    if (
      file &&
      (await ask(
        t("Put {file} back into {name}'s computer? Files it has that are newer stay as they are.", {
          file: file.name,
          name: kitty.name,
        }),
        t("Restore"),
      ))
    ) {
      onImport(file);
    }
  };
  return (
    <div class="flex flex-col gap-1 border-t border-white/5 pt-2" data-testid="kept-files">
      <span class="text-xs text-[var(--t2)]">
        {storage.home
          ? t(
              "Kept on this computer, safe from updates: home and programs {home}, work {work}, Shared {shared}.",
              {
                home: bytes(storage.home_bytes),
                work: bytes(storage.work_bytes),
                shared: bytes(storage.shared_bytes),
              },
            )
          : t(
              "Kept on this computer, safe from updates: work {work}, Shared {shared}. Its settings and programs join them at its next update.",
              { work: bytes(storage.work_bytes), shared: bytes(storage.shared_bytes) },
            )}
        {quota > 0 && " " + t("Shared holds up to {quota}.", { quota: bytes(quota * 1024 * 1024) })}
      </span>
      {storage.moving && (
        <span class="text-xs text-[var(--c-amber)]" data-testid="home-moving">
          {t(
            "Its settings and programs are moving to this computer: they will be there after its next start.",
          )}
        </span>
      )}
      <div class="flex items-center gap-2 flex-wrap">
        <a
          class="btn-ghost px-2 py-1 text-xs"
          href={shotURL(`/v1/kitties/${encodeURIComponent(kitty.id)}/export`)}
          download={`kitty-${kitty.name}.tar.gz`}
          data-testid="export-files"
        >
          {t("Export files")}
        </a>
        <label
          class={`btn-ghost px-2 py-1 text-xs cursor-pointer ${busy || kitty.status !== "running" ? "opacity-50 pointer-events-none" : ""}`}
          data-testid="import-files"
        >
          {t("Restore from an export")}
          <input type="file" accept=".gz,.tgz,application/gzip" class="hidden" onChange={pick} />
        </label>
      </div>
      <p class="text-xs text-[var(--t4)]">
        {t(
          "The export leaves out keys, passwords and the browser's sessions; those stay in the sealed backup.",
        )}
      </p>
    </div>
  );
}
