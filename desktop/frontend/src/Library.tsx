import { useEffect, useState } from "preact/hooks";
import type { ArtifactInfo, Skill } from "./api";
import { api, artifactText, shotURL } from "./api";
import { when } from "./format";
import { IconPlus, IconTrash, IconX } from "./icons";
import { Missing, Required } from "./Required";

type Tab = "skills" | "artifacts";

// LibraryModal is what the kitties share and reuse: the skills (procedures they read and
// propose) and the artifacts (files they saved for later).
export default function LibraryModal({ onClose, initial }: { onClose: () => void; initial?: Tab }) {
  const [tab, setTab] = useState<Tab>(initial || "skills");
  useEffect(() => setTab(initial || "skills"), [initial]);
  const [skills, setSkills] = useState<Skill[]>([]);
  const [arts, setArts] = useState<ArtifactInfo[]>([]);
  const [open, setOpen] = useState<Skill | null>(null);
  const [draft, setDraft] = useState<{ name: string; body: string } | null>(null);
  const [reading, setReading] = useState<{ name: string; text: string } | null>(null);
  const [err, setErr] = useState("");
  const load = () => {
    api.skills().then(setSkills).catch((e) => setErr((e as Error).message));
    api.artifacts().then(setArts).catch((e) => setErr((e as Error).message));
  };
  useEffect(load, []);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    addEventListener("keydown", onKey);
    return () => removeEventListener("keydown", onKey);
  }, [onClose]);
  const act = (p: Promise<unknown>) => p.then(load).catch((e) => setErr((e as Error).message));
  const proposals = skills.filter((s) => s.proposed);
  return (
    <div class="fixed inset-0 z-40 bg-black/60 flex items-center justify-center p-4 backdrop" onClick={onClose} data-modal>
      <div class="chat-modal w-full max-w-3xl p-5 max-h-[90vh] overflow-y-auto scroll-thin" onClick={(e) => e.stopPropagation()} role="dialog">
        <div class="flex items-center justify-between mb-4">
          <h2 class="font-mono text-base tracking-wide">Library</h2>
          <button class="plain p-1 text-[var(--t3)] hover:text-[var(--t1)]" onClick={onClose} aria-label="Close">
            <IconX />
          </button>
        </div>
        <div class="flex gap-1 border-b border-white/5 mb-3">
          {(["skills", "artifacts"] as Tab[]).map((t) => (
            <button key={t} class={`plain px-4 py-2 text-sm border-b-2 capitalize ${tab === t ? "border-[var(--color-accent)] text-[var(--t1)]" : "border-transparent text-[var(--t3)]"}`} onClick={() => setTab(t)}>
              {t}
              {t === "skills" && proposals.length > 0 && <span class="tag tag-amber ml-2">{proposals.length} proposed</span>}
            </button>
          ))}
        </div>
        {err && <p class="text-sm text-[var(--c-red)] mb-2">{err}</p>}

        {tab === "skills" && (
          <div>
            <p class="text-sm text-[var(--t3)]">Procedures the kitties read before a task and extend when they learn something. A proposal waits here until you accept it.</p>
            <div class="flex flex-col gap-1 mt-3">
              {skills.map((s) => (
                <div key={s.name} class="row !py-2.5">
                  <button class="row-hit" onClick={() => api.skill(s.name).then(setOpen)} aria-label={s.title} />
                  <span class={`dot ${s.proposed ? "stopped" : "running"}`} />
                  <div class="flex-1 min-w-0">
                    <div class="flex items-center gap-2 flex-wrap">
                      <span class="text-sm font-semibold">{s.title}</span>
                      <span class="tag">{s.name}</span>
                      {s.builtin && <span class="tag tag-accent">built in</span>}
                      {s.proposed && <span class="tag tag-amber">proposed</span>}
                    </div>
                    <div class="text-xs text-[var(--t3)] truncate">{s.summary}</div>
                  </div>
                  <div class="row-actions flex gap-1 flex-none">
                    {s.proposed && (
                      <button class="btn-outline px-2 py-1 text-xs" onClick={() => act(api.acceptSkill(s.name))}>
                        accept
                      </button>
                    )}
                    {!s.builtin && (
                      <button class="btn-ghost px-2 py-1 text-xs text-[var(--c-red)]" aria-label={`Delete ${s.name}`} onClick={() => confirm(`Delete the skill ${s.name}?`) && act(api.deleteSkill(s.name))}>
                        <IconTrash size={12} />
                      </button>
                    )}
                  </div>
                </div>
              ))}
            </div>
            {draft ? (
              <div class="mt-3 p-4 border border-[rgb(var(--accent-rgb)/0.3)] bg-[rgb(var(--accent-rgb)/0.05)]">
                <label class="block text-xs text-[var(--t3)] mb-1 uppercase tracking-wider font-mono">Name<Required /></label>
                <input class="w-full px-3 py-2 text-sm" aria-required="true" aria-label="Name" value={draft.name} onInput={(e) => setDraft({ ...draft, name: (e.target as HTMLInputElement).value })} placeholder="deploy-to-prod" />
                <label class="block text-xs text-[var(--t3)] mb-1 mt-3 uppercase tracking-wider font-mono">Document (markdown)<Required /></label>
                <textarea class="w-full px-3 py-2 text-sm font-mono min-h-[160px]" aria-required="true" aria-label="Document" value={draft.body} onInput={(e) => setDraft({ ...draft, body: (e.target as HTMLTextAreaElement).value })} placeholder={"# Title\n\nWhat it is for.\n\n1. Step"} />
                <div class="flex justify-end gap-2 mt-3">
                  <button class="btn-ghost px-4 py-2 text-sm" onClick={() => setDraft(null)}>
                    Cancel
                  </button>
                  <button class="btn-accent px-4 py-2 text-sm" disabled={!draft.name.trim() || !draft.body.trim()} onClick={() => act(api.saveSkill(draft.name.trim(), draft.body)).then(() => setDraft(null))}>
                    Save
                  </button>
                </div>
                <Missing items={[!draft.name.trim() && "the name", !draft.body.trim() && "the document"]} />
              </div>
            ) : (
              <button class="btn-outline mt-3 flex items-center gap-2 px-3 py-2 text-sm" onClick={() => setDraft({ name: "", body: "" })}>
                <IconPlus size={14} /> Write a skill
              </button>
            )}
            {open && (
              <div class="mt-3 p-4 border border-[rgb(var(--neon-rgb)/0.3)] bg-[rgb(var(--bg-rgb)/0.6)]">
                <div class="flex items-center justify-between">
                  <h3 class="font-mono text-sm">{open.title}</h3>
                  <div class="flex gap-1">
                    {!open.builtin && (
                      <button class="btn-ghost px-2 py-1 text-xs" onClick={() => setDraft({ name: open.name, body: open.body || "" })}>
                        edit
                      </button>
                    )}
                    <button class="plain p-1 text-[var(--t3)]" onClick={() => setOpen(null)} aria-label="Close the skill">
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
            <p class="text-sm text-[var(--t3)]">Files the kitties saved for later: reports, tables, diagrams, drafts. Any kitty can read them in any chat; you can download or delete them.</p>
            <div class="flex flex-col gap-1 mt-3">
              {arts.map((a) => (
                <div key={a.name} class="row !py-2.5">
                  <button class="row-hit" onClick={() => artifactText(a.name).then((text) => setReading({ name: a.name, text })).catch((e) => setErr((e as Error).message))} aria-label={a.name} />
                  <span class="dot running" />
                  <div class="flex-1 min-w-0">
                    <div class="flex items-center gap-2 flex-wrap">
                      <span class="text-sm font-semibold">{a.name}</span>
                      <span class="tag">{a.type}</span>
                      {a.by && <span class="tag tag-magenta">by {a.by}</span>}
                    </div>
                    <div class="text-xs text-[var(--t3)]">
                      {a.size} bytes · {when(a.updated_at)}
                    </div>
                  </div>
                  <div class="row-actions flex gap-1 flex-none">
                    <a class="btn-ghost px-2 py-1 text-xs" href={shotURL(`/v1/artifacts/${encodeURIComponent(a.name)}`)} download={a.name} target="_blank">
                      download
                    </a>
                    <button class="btn-ghost px-2 py-1 text-xs text-[var(--c-red)]" aria-label={`Delete ${a.name}`} onClick={() => confirm(`Delete ${a.name}?`) && act(api.deleteArtifact(a.name))}>
                      <IconTrash size={12} />
                    </button>
                  </div>
                </div>
              ))}
              {arts.length === 0 && <div class="text-sm text-[var(--t4)] px-2">Nothing saved yet. A kitty saves one with @artifact save, or a connector such as the diagram renderer does.</div>}
            </div>
            {reading && (
              <div class="mt-3 p-4 border border-[rgb(var(--neon-rgb)/0.3)] bg-[rgb(var(--bg-rgb)/0.6)]">
                <div class="flex items-center justify-between">
                  <h3 class="font-mono text-sm">{reading.name}</h3>
                  <button class="plain p-1 text-[var(--t3)]" onClick={() => setReading(null)} aria-label="Close the artifact">
                    <IconX size={14} />
                  </button>
                </div>
                {reading.name.endsWith(".svg") ? <img class="mt-2 max-h-80 bg-white" src={"data:image/svg+xml;utf8," + encodeURIComponent(reading.text)} alt={reading.name} /> : <pre class="skill-body mt-2">{reading.text}</pre>}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
