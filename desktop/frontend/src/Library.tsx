import { useEffect, useLayoutEffect, useState } from "preact/hooks";
import type { ArtifactInfo, SharedFile, Skill } from "./api";
import { api, sharedPath, shotURL } from "./api";
import ArtifactReader from "./ArtifactReader";
import { ask } from "./Confirm";
import { when } from "./format";
import { t } from "./i18n";
import { IconPlus, IconTrash, IconX } from "./icons";
import { bytes, facets, filterArtifacts, type Kind, type Sort } from "./artifacts";
import { Missing, Required } from "./Required";

type Tab = "skills" | "artifacts" | "shared";

// LibraryModal is what the kitties share and reuse: the skills (procedures they read and
// propose), the artifacts (files they saved for later) and what each kitty left for its person in
// the Shared folder of its computer.
export default function LibraryModal({ onClose, initial }: { onClose: () => void; initial?: Tab }) {
  const [tab, setTab] = useState<Tab>(initial || "skills");
  useEffect(() => setTab(initial || "skills"), [initial]);
  const [skills, setSkills] = useState<Skill[]>([]);
  const [arts, setArts] = useState<ArtifactInfo[]>([]);
  const [open, setOpen] = useState<Skill | null>(null);
  const [draft, setDraft] = useState<{ name: string; body: string } | null>(null);
  const [reading, setReading] = useState<string | null>(null);
  const [shared, setShared] = useState<{ files: SharedFile[]; quota_mb: number }>({
    files: [],
    quota_mb: 0,
  });
  // The artifacts' filters: a search in the names, a kind, the kitty that saved them, an order.
  const [q, setQ] = useState("");
  const [kind, setKind] = useState<Kind | "">("");
  const [by, setBy] = useState("");
  const [sort, setSort] = useState<Sort>("new");
  const [err, setErr] = useState("");
  const load = () => {
    api
      .skills()
      .then(setSkills)
      .catch((e) => setErr((e as Error).message));
    api
      .artifacts()
      .then(setArts)
      .catch((e) => setErr((e as Error).message));
    // An older gateway has no Shared folders: the tab stays empty.
    api
      .shared()
      .then(setShared)
      .catch(() => undefined);
  };
  useEffect(load, []);
  useLayoutEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && !e.defaultPrevented && onClose();
    addEventListener("keydown", onKey);
    return () => removeEventListener("keydown", onKey);
  }, [onClose]);
  const act = (p: Promise<unknown>) => p.then(load).catch((e) => setErr((e as Error).message));
  const proposals = skills.filter((s) => s.proposed);
  return (
    <div
      class="fixed inset-0 z-40 bg-black/60 flex items-center justify-center p-4 backdrop"
      onClick={onClose}
      data-modal
    >
      <div
        class="chat-modal w-full max-w-3xl p-5 max-h-[90vh] overflow-y-auto scroll-thin"
        onClick={(e) => e.stopPropagation()}
        role="dialog"
      >
        <div class="flex items-center justify-between mb-4">
          <h2 class="font-mono text-base tracking-wide">{t("Library")}</h2>
          <button
            class="plain p-1 text-[var(--t3)] hover:text-[var(--t1)]"
            onClick={onClose}
            aria-label={t("Close")}
          >
            <IconX />
          </button>
        </div>
        <div class="flex gap-1 border-b border-white/5 mb-3">
          {(["skills", "artifacts", "shared"] as Tab[]).map((tb) => (
            <button
              key={tb}
              class={`plain px-4 py-2 text-sm border-b-2 capitalize ${tab === tb ? "border-[var(--color-accent)] text-[var(--t1)]" : "border-transparent text-[var(--t3)]"}`}
              onClick={() => setTab(tb)}
            >
              {tb === "shared" ? t("Shared files") : t(tb)}
              {tb === "shared" && shared.files.length > 0 && (
                <span class="tag ml-2" data-testid="shared-count">
                  {shared.files.length}
                </span>
              )}
              {tb === "skills" && proposals.length > 0 && (
                <span class="tag tag-amber ml-2">{t("{n} proposed", { n: proposals.length })}</span>
              )}
            </button>
          ))}
        </div>
        {err && <p class="text-sm text-[var(--c-red)] mb-2">{err}</p>}

        {tab === "skills" && (
          <div>
            <p class="text-sm text-[var(--t3)]">
              {t(
                "Procedures the kitties read before a task and extend when they learn something. A proposal waits here until you accept it.",
              )}
            </p>
            <div class="flex flex-col gap-1 mt-3">
              {skills.map((s) => (
                <div key={s.name} class="row !py-2.5">
                  <button
                    class="row-hit"
                    onClick={() => api.skill(s.name).then(setOpen)}
                    aria-label={s.title}
                  />
                  <span class={`dot ${s.proposed ? "stopped" : "running"}`} />
                  <div class="flex-1 min-w-0">
                    <div class="flex items-center gap-2 flex-wrap">
                      <span class="text-sm font-semibold">{s.title}</span>
                      <span class="tag">{s.name}</span>
                      {s.builtin && <span class="tag tag-accent">{t("built in")}</span>}
                      {s.proposed && <span class="tag tag-amber">{t("proposed")}</span>}
                    </div>
                    <div class="text-xs text-[var(--t3)] truncate">{s.summary}</div>
                  </div>
                  <div class="row-actions flex gap-1 flex-none">
                    {s.proposed && (
                      <button
                        class="btn-outline px-2 py-1 text-xs"
                        onClick={() => act(api.acceptSkill(s.name))}
                      >
                        {t("accept")}
                      </button>
                    )}
                    {!s.builtin && (
                      <button
                        class="btn-ghost px-2 py-1 text-xs text-[var(--c-red)]"
                        aria-label={t("Delete {name}", { name: s.name })}
                        onClick={async () =>
                          (await ask(
                            t("Delete the skill {name}?", { name: s.name }),
                            t("Delete"),
                          )) && act(api.deleteSkill(s.name))
                        }
                      >
                        <IconTrash size={12} />
                      </button>
                    )}
                  </div>
                </div>
              ))}
            </div>
            {draft ? (
              <div class="mt-3 p-4 border border-[rgb(var(--accent-rgb)/0.3)] bg-[rgb(var(--accent-rgb)/0.05)]">
                <label class="block text-xs text-[var(--t3)] mb-1 uppercase tracking-wider font-mono">
                  {t("Name")}
                  <Required />
                </label>
                <input
                  class="w-full px-3 py-2 text-sm"
                  aria-required="true"
                  aria-label={t("Name")}
                  value={draft.name}
                  onInput={(e) =>
                    setDraft({ ...draft, name: (e.target as HTMLInputElement).value })
                  }
                  placeholder="deploy-to-prod"
                />
                <label class="block text-xs text-[var(--t3)] mb-1 mt-3 uppercase tracking-wider font-mono">
                  {t("Document (markdown)")}
                  <Required />
                </label>
                <textarea
                  class="w-full px-3 py-2 text-sm font-mono min-h-[10rem]"
                  aria-required="true"
                  aria-label={t("Document")}
                  value={draft.body}
                  onInput={(e) =>
                    setDraft({ ...draft, body: (e.target as HTMLTextAreaElement).value })
                  }
                  placeholder={t("# Title\n\nWhat it is for.\n\n1. Step")}
                />
                <div class="flex justify-end gap-2 mt-3">
                  <button class="btn-ghost px-4 py-2 text-sm" onClick={() => setDraft(null)}>
                    {t("Cancel")}
                  </button>
                  <button
                    class="btn-accent px-4 py-2 text-sm"
                    disabled={!draft.name.trim() || !draft.body.trim()}
                    onClick={() =>
                      act(api.saveSkill(draft.name.trim(), draft.body)).then(() => setDraft(null))
                    }
                  >
                    {t("Save")}
                  </button>
                </div>
                <Missing
                  items={[
                    !draft.name.trim() && t("the name"),
                    !draft.body.trim() && t("the document"),
                  ]}
                />
              </div>
            ) : (
              <button
                class="btn-outline mt-3 flex items-center gap-2 px-3 py-2 text-sm"
                onClick={() => setDraft({ name: "", body: "" })}
              >
                <IconPlus size={14} /> {t("Write a skill")}
              </button>
            )}
            {open && (
              <div class="mt-3 p-4 border border-[rgb(var(--neon-rgb)/0.3)] bg-[rgb(var(--bg-rgb)/0.6)]">
                <div class="flex items-center justify-between">
                  <h3 class="font-mono text-sm">{open.title}</h3>
                  <div class="flex gap-1">
                    {!open.builtin && (
                      <button
                        class="btn-ghost px-2 py-1 text-xs"
                        onClick={() => setDraft({ name: open.name, body: open.body || "" })}
                      >
                        {t("edit")}
                      </button>
                    )}
                    <button
                      class="plain p-1 text-[var(--t3)]"
                      onClick={() => setOpen(null)}
                      aria-label={t("Close the skill")}
                    >
                      <IconX size={14} />
                    </button>
                  </div>
                </div>
                <pre class="skill-body mt-2">{open.body}</pre>
              </div>
            )}
          </div>
        )}

        {tab === "artifacts" && (
          <div>
            <p class="text-sm text-[var(--t3)]">
              {t(
                "Files the kitties saved for later: reports, tables, diagrams, drafts. Any kitty can read them in any chat; you can download or delete them.",
              )}
            </p>
            {arts.length > 0 && (
              <ArtifactFilters
                list={arts}
                q={q}
                setQ={setQ}
                kind={kind}
                setKind={setKind}
                by={by}
                setBy={setBy}
                sort={sort}
                setSort={setSort}
              />
            )}
            <div class="flex flex-col gap-1 mt-3">
              {arts.length > 0 && filterArtifacts(arts, { q, kind, by, sort }).length === 0 && (
                <div class="text-sm text-[var(--t4)] px-2" data-testid="artifacts-none">
                  {t("No file matches these filters.")}
                </div>
              )}
              {filterArtifacts(arts, { q, kind, by, sort }).map((a) => (
                <div key={a.name} class="row !py-2.5">
                  <button class="row-hit" onClick={() => setReading(a.name)} aria-label={a.name} />
                  <span class="dot running" />
                  <div class="flex-1 min-w-0">
                    <div class="flex items-center gap-2 flex-wrap">
                      <span class="text-sm font-semibold">{a.name}</span>
                      <span class="tag">{a.type}</span>
                      {a.by && (
                        <span class="tag tag-magenta">{t("by {name}", { name: a.by })}</span>
                      )}
                    </div>
                    <div class="text-xs text-[var(--t3)]">
                      {t("{size} bytes", { size: a.size })} · {when(a.updated_at)}
                    </div>
                  </div>
                  <div class="row-actions flex gap-1 flex-none">
                    <a
                      class="btn-ghost px-2 py-1 text-xs"
                      href={shotURL(`/v1/artifacts/${encodeURIComponent(a.name)}`)}
                      download={a.name}
                      target="_blank"
                    >
                      {t("download")}
                    </a>
                    <button
                      class="btn-ghost px-2 py-1 text-xs text-[var(--c-red)]"
                      aria-label={t("Delete {name}", { name: a.name })}
                      onClick={async () =>
                        (await ask(t("Delete {name}?", { name: a.name }), t("Delete"))) &&
                        act(api.deleteArtifact(a.name))
                      }
                    >
                      <IconTrash size={12} />
                    </button>
                  </div>
                </div>
              ))}
              {arts.length === 0 && (
                <div class="text-sm text-[var(--t4)] px-2">
                  {t(
                    "Nothing saved yet. A kitty saves one with @artifact save, or a connector such as the diagram renderer does.",
                  )}
                </div>
              )}
            </div>
            {reading && <ArtifactReader name={reading} onClose={() => setReading(null)} />}
          </div>
        )}

        {tab === "shared" && (
          <SharedFiles
            files={shared.files}
            quotaMB={shared.quota_mb}
            act={act}
            onKept={() => setTab("artifacts")}
          />
        )}
      </div>
    </div>
  );
}

const KIND_LABEL: Record<Kind, string> = {
  doc: "Documents",
  data: "Data",
  image: "Images",
  code: "Code",
  other: "Other",
};

// ArtifactFilters narrows the artifacts: a search, a chip per kind the library has, the kitty
// that saved them and the order.
function ArtifactFilters(p: {
  list: ArtifactInfo[];
  q: string;
  setQ: (v: string) => void;
  kind: Kind | "";
  setKind: (v: Kind | "") => void;
  by: string;
  setBy: (v: string) => void;
  sort: Sort;
  setSort: (v: Sort) => void;
}) {
  const f = facets(p.list);
  return (
    <div class="flex flex-col gap-2 mt-3" data-testid="artifact-filters">
      <div class="flex gap-2 flex-wrap">
        <input
          class="flex-1 min-w-[10rem] px-3 py-2 text-sm"
          type="search"
          placeholder={t("Search the files")}
          aria-label={t("Search the files")}
          value={p.q}
          onInput={(e) => p.setQ((e.target as HTMLInputElement).value)}
        />
        {f.by.length > 1 && (
          <select
            class="px-3 py-2 text-sm"
            aria-label={t("Saved by")}
            value={p.by}
            onChange={(e) => p.setBy((e.target as HTMLSelectElement).value)}
          >
            <option value="">{t("Every kitty")}</option>
            {f.by.map((b) => (
              <option key={b} value={b}>
                {b}
              </option>
            ))}
          </select>
        )}
        <select
          class="px-3 py-2 text-sm"
          aria-label={t("Order")}
          value={p.sort}
          onChange={(e) => p.setSort((e.target as HTMLSelectElement).value as Sort)}
        >
          <option value="new">{t("Newest first")}</option>
          <option value="name">{t("By name")}</option>
          <option value="size">{t("Biggest first")}</option>
        </select>
      </div>
      {f.kinds.length > 1 && (
        <div class="flex gap-1 flex-wrap">
          <button class={`chip ${p.kind === "" ? "on" : ""}`} onClick={() => p.setKind("")}>
            {t("All")}
          </button>
          {f.kinds.map((k) => (
            <button
              key={k}
              class={`chip ${p.kind === k ? "on" : ""}`}
              data-testid={`kind-${k}`}
              onClick={() => p.setKind(p.kind === k ? "" : k)}
            >
              {t(KIND_LABEL[k])}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

// A shared file as a row of the filters: its path is its name, its kitty who saved it.
type SharedRow = ArtifactInfo & { file: SharedFile };

// The library keeps files up to this size (artifacts.maxFile).
const LIBRARY_MAX = 8 << 20;

// SharedFiles lists what the kitties left in the Shared folders of their computers: a picture or
// a text opens in the reader, anything else downloads; a file can be kept in the library, where
// every kitty reads it, or deleted.
function SharedFiles({
  files,
  quotaMB,
  act,
  onKept,
}: {
  files: SharedFile[];
  quotaMB: number;
  act: (p: Promise<unknown>) => Promise<unknown>;
  onKept: () => void;
}) {
  const [q, setQ] = useState("");
  const [kind, setKind] = useState<Kind | "">("");
  const [by, setBy] = useState("");
  const [sort, setSort] = useState<Sort>("new");
  const [open, setOpen] = useState<SharedFile | null>(null);
  const rows: SharedRow[] = files.map((f) => ({
    name: f.path,
    type: f.type,
    size: f.size,
    updated_at: f.updated_at,
    by: f.kitty,
    file: f,
  }));
  const shown = filterArtifacts(rows, { q, kind, by, sort });
  return (
    <div data-testid="shared-files">
      <p class="text-sm text-[var(--t3)]">
        {t(
          "What the kitties made for you, from the Shared folder of their computers: documents, pictures, files. It stays there through updates.",
        )}
        {quotaMB > 0 &&
          " " +
            t("Each kitty's folder holds up to {quota}.", { quota: bytes(quotaMB * 1024 * 1024) })}
      </p>
      {rows.length > 0 && (
        <ArtifactFilters
          list={rows}
          q={q}
          setQ={setQ}
          kind={kind}
          setKind={setKind}
          by={by}
          setBy={setBy}
          sort={sort}
          setSort={setSort}
        />
      )}
      <div class="flex flex-col gap-1 mt-3">
        {rows.length > 0 && shown.length === 0 && (
          <div class="text-sm text-[var(--t4)] px-2">{t("No file matches these filters.")}</div>
        )}
        {shown.map(({ file: f }) => (
          <div key={f.kitty + "/" + f.path} class="row !py-2.5" data-testid="shared-row">
            <button class="row-hit" onClick={() => setOpen(f)} aria-label={f.path} />
            <span class="dot running" />
            <div class="flex-1 min-w-0">
              <div class="flex items-center gap-2 flex-wrap">
                <span class="text-sm font-semibold break-all">{f.path}</span>
                <span class="tag">{f.type.split(";")[0]}</span>
                <span class="tag tag-magenta">{t("by {name}", { name: f.kitty })}</span>
              </div>
              <div class="text-xs text-[var(--t3)]">
                {bytes(f.size)} · {when(f.updated_at)}
              </div>
            </div>
            <div class="row-actions flex gap-1 flex-none">
              <a
                class="btn-ghost px-2 py-1 text-xs"
                href={shotURL(sharedPath(f.kitty, f.path) + "?download=1")}
                download={f.path.split("/").pop()}
                target="_blank"
              >
                {t("download")}
              </a>
              {f.size <= LIBRARY_MAX && (
                <button
                  class="btn-ghost px-2 py-1 text-xs"
                  title={t("Copy it into the library, where every kitty can read it")}
                  data-testid="keep-shared"
                  onClick={() => act(api.keepShared(f.kitty, f.path)).then(onKept)}
                >
                  {t("keep in library")}
                </button>
              )}
              <button
                class="btn-ghost px-2 py-1 text-xs text-[var(--c-red)]"
                aria-label={t("Delete {name}", { name: f.path })}
                onClick={async () =>
                  (await ask(
                    t("Delete {name} from {kitty}'s Shared folder?", {
                      name: f.path,
                      kitty: f.kitty,
                    }),
                    t("Delete"),
                  )) && act(api.deleteShared(f.kitty, f.path))
                }
              >
                <IconTrash size={12} />
              </button>
            </div>
          </div>
        ))}
        {rows.length === 0 && (
          <div class="text-sm text-[var(--t4)] px-2" data-testid="shared-empty">
            {t(
              "Nothing shared yet. Ask a kitty to share a file with you: it leaves it in ~/Shared on its computer and it shows up here.",
            )}
          </div>
        )}
      </div>
      {open && (
        <ArtifactReader
          name={open.path.split("/").pop() || open.path}
          path={sharedPath(open.kitty, open.path)}
          onClose={() => setOpen(null)}
        />
      )}
    </div>
  );
}
