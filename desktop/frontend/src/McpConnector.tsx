import { useEffect, useState } from "preact/hooks";
import type { McpClient, McpConnect, McpInfo, McpSettings } from "./api";
import { api, errText, MCP_TOKEN_PLACEHOLDER } from "./api";
import { ask } from "./Confirm";
import { t } from "./i18n";

// THE CLAUDE CONNECTOR (MCP), docs/MCP.md.
//
// Shows whether Claude can run the fleet through /mcp, the three switches that widen what it may
// do (exec, approve, OAuth), the `claude mcp add` line ready to copy and the remote clients that
// signed in, each with a revoke button. The connector answers wherever the gateway does (this
// machine, the local network, the public address), and each address has its command. The token
// is never drawn: on this machine the command reads it from its file; elsewhere it shows a
// placeholder and *Copy command* puts the token in, like copying or rotating it, on request.

const KEYS: (keyof McpSettings)[] = ["enabled", "allow_exec", "allow_approve", "oauth"];

function settingsOf(i: McpInfo): McpSettings {
  return {
    enabled: i.enabled,
    allow_exec: i.allow_exec,
    allow_approve: i.allow_approve,
    oauth: i.oauth,
  };
}

// connectOf is the connector's addresses; an older gateway gives only url and command.
function connectOf(i: McpInfo): McpConnect[] {
  if (i.connect && i.connect.length > 0) {
    return i.connect;
  }
  return [{ where: "local", url: i.url, command: i.command }];
}

// firstChoice is the address this page was opened at, when it is one of them: the computer that
// has the page open is the likeliest to have Claude Code too. Otherwise the first (best) one.
function firstChoice(all: McpConnect[]): number {
  const i = all.findIndex((c) => {
    try {
      return new URL(c.url).host === location.host;
    } catch {
      return false;
    }
  });
  return i >= 0 ? i : 0;
}

export default function McpConnector() {
  const [cur, setCur] = useState<McpInfo | null>(null);
  const [pick, setPick] = useState(0);
  const [want, setWant] = useState<McpSettings | null>(null);
  const [risk, setRisk] = useState(false);
  const [clients, setClients] = useState<McpClient[]>([]);
  const [busy, setBusy] = useState(false);
  const [restarting, setRestarting] = useState(false);
  const [note, setNote] = useState("");
  const [err, setErr] = useState("");
  useEffect(() => {
    api
      .mcp()
      .then((i) => {
        setCur(i);
        setWant(settingsOf(i));
        setPick(firstChoice(connectOf(i)));
        if (i.enabled && i.oauth) {
          api
            .mcpClients()
            .then((r) => setClients(r.clients || []))
            .catch(() => setClients([]));
        }
      })
      // A gateway that cannot change it (an older one) shows nothing.
      .catch(() => setCur(null));
  }, []);
  if (!cur || !want) {
    return null;
  }
  const changed = KEYS.some((k) => want[k] !== cur[k]);
  const widens = (want.allow_exec && !cur.allow_exec) || (want.allow_approve && !cur.allow_approve);
  const set = (k: keyof McpSettings, v: boolean) => {
    const next = { ...want, [k]: v };
    // The connector off takes its extras with it; an extra needs it on.
    if (k === "enabled" && !v) {
      next.allow_exec = next.allow_approve = next.oauth = false;
    }
    if (k !== "enabled" && v) {
      next.enabled = true;
    }
    setWant(next);
    setRisk(false);
  };
  const waitFor = (done: (i: McpInfo) => boolean) => {
    setRestarting(true);
    let tries = 0;
    const id = setInterval(async () => {
      tries++;
      try {
        const i = await api.mcp();
        if (done(i)) {
          clearInterval(id);
          location.reload();
        }
      } catch {
        /* it is restarting */
      }
      if (tries > 30) {
        clearInterval(id);
        setRestarting(false);
        setErr(t("Kitties did not come back after the change: reload this page."));
      }
    }, 1000);
  };
  const run = async (fn: () => Promise<unknown>, done: (i: McpInfo) => boolean) => {
    setBusy(true);
    setErr("");
    try {
      await fn();
      waitFor(done);
    } catch (e) {
      setErr(errText(e));
    } finally {
      setBusy(false);
    }
  };
  const copy = async (text: string, what: string) => {
    try {
      await navigator.clipboard.writeText(text);
      setNote(t("Copied: {what}.", { what }));
    } catch {
      setNote(t("This page cannot reach the clipboard: select the text and copy it by hand."));
    }
  };
  const connect = connectOf(cur);
  const chosen = connect[Math.min(pick, connect.length - 1)];
  const where = {
    local: t("This computer"),
    lan: t("Local network"),
    public: t("Public address"),
  };
  // copyCommand puts the token into a command that needs it: the person asked for it.
  const copyCommand = async () => {
    setErr("");
    if (!chosen.with_token) {
      await copy(chosen.command, t("the command"));
      return;
    }
    try {
      const { token } = await api.mcpToken();
      await copy(
        chosen.command.split(MCP_TOKEN_PLACEHOLDER).join(token),
        t("the command with the token"),
      );
    } catch (e) {
      setErr(errText(e));
    }
  };
  const copyToken = async () => {
    setErr("");
    try {
      await copy((await api.mcpToken()).token, t("the token"));
    } catch (e) {
      setErr(errText(e));
    }
  };
  const test = async () => {
    setErr("");
    setNote(t("Testing…"));
    try {
      const r = await api.mcpTest();
      setNote(
        r.ok
          ? t("The connector answers: {n} tools for Claude.", { n: r.tools ?? 0 })
          : t("The connector does not answer: {why}", { why: r.error || "" }),
      );
    } catch (e) {
      setNote("");
      setErr(errText(e));
    }
  };
  const rotate = async () => {
    const yes = await ask(
      t(
        "A new token replaces the old one and every connection that used it stops working, claude.ai sign-ins too. Continue?",
      ),
      t("Rotate token"),
    );
    if (yes) {
      // The new token is not known yet; the file changes and Kitties restarts.
      await run(
        () => api.rotateMcpToken(),
        () => true,
      );
    }
  };
  const revoke = async (c: McpClient) => {
    const yes = await ask(
      t("{name} will lose access at once and has to sign in again. Revoke?", { name: c.name }),
      t("Revoke"),
    );
    if (!yes) {
      return;
    }
    setErr("");
    try {
      await api.revokeMcpClient(c.id);
      setClients((all) => all.map((x) => (x.id === c.id ? { ...x, revoked: true } : x)));
    } catch (e) {
      setErr(errText(e));
    }
  };
  const disabled = !cur.can_change || busy || restarting;
  const switches: { key: keyof McpSettings; title: string; hint: string; warn?: boolean }[] = [
    {
      key: "enabled",
      title: t("Claude connector"),
      hint: t("Lets Claude list, create and drive your kitties through {url}.", { url: cur.url }),
    },
    {
      key: "allow_exec",
      title: t("Allow commands in a kitty"),
      hint: t("Adds kitty_exec: Claude may run commands inside a kitty's container."),
      warn: true,
    },
    {
      key: "allow_approve",
      title: t("Allow Claude to approve"),
      hint: t("Adds approval_approve: Claude may say yes, once, to an action held for you."),
      warn: true,
    },
    {
      key: "oauth",
      title: t("Sign-in for claude.ai"),
      hint: t("Remote clients sign in with OAuth. It needs https in front (public address)."),
    },
  ];
  return (
    <div class="mt-6 pt-4 border-t border-white/5" data-testid="mcp">
      <div class="section-title !mx-0">{t("Claude (MCP)")}</div>
      <p class="text-xs text-[var(--t3)] mb-2">
        {t(
          "Let Claude run the fleet as an orchestrator. It answers wherever Kitties does, behind a token of its own.",
        )}
      </p>
      <div class="flex flex-col gap-2">
        {switches.map((s) => (
          <label
            key={s.key}
            class="callout flex items-start gap-2 cursor-pointer"
            data-testid={`mcp-${s.key}`}
          >
            <input
              type="checkbox"
              class="mt-1"
              checked={want[s.key]}
              disabled={disabled}
              onChange={() => set(s.key, !want[s.key])}
            />
            <span>
              <span class="font-semibold text-sm">{s.title}</span>
              <span class="block text-xs text-[var(--t3)]">{s.hint}</span>
            </span>
          </label>
        ))}
      </div>
      {widens && changed && (
        <div class="callout mt-2" role="alert" data-testid="mcp-warning">
          <div class="font-semibold text-sm text-[var(--c-amber)]">
            {t("This gives Claude more power")}
          </div>
          <p class="text-xs text-[var(--t3)] mt-1">
            {t(
              "Commands run inside the kitty and an approval lets a held action go ahead without you. Turn it on only if you trust the client that holds the token.",
            )}
          </p>
          <label class="flex items-center gap-2 mt-2 text-sm cursor-pointer">
            <input
              type="checkbox"
              checked={risk}
              onChange={() => setRisk(!risk)}
              data-testid="mcp-accept"
            />
            {t("I understand the risk")}
          </label>
        </div>
      )}
      {!cur.can_change && cur.why && (
        <p class="text-xs text-[var(--t3)] mt-2" data-testid="mcp-locked">
          {cur.why}
        </p>
      )}
      {restarting && (
        <p class="text-sm mt-2" role="status" data-testid="mcp-restarting">
          <span class="spinner" /> {t("Saved: restarting Kitties…")}
        </p>
      )}
      {cur.can_change && (
        <button
          class="btn-accent mt-3 px-3 py-1.5 text-sm"
          disabled={!changed || disabled || (widens && !risk)}
          onClick={() =>
            run(
              () => api.setMcp(want, risk),
              (i) => KEYS.every((k) => i[k] === want[k]),
            )
          }
          data-testid="mcp-apply"
        >
          {t("Apply and restart")}
        </button>
      )}
      {cur.enabled && (
        <div class="mt-4" data-testid="mcp-connect">
          <div class="text-sm font-semibold">{t("Connect Claude Code")}</div>
          {connect.length > 1 && (
            <div class="flex flex-wrap gap-2 mb-2" role="radiogroup" data-testid="mcp-where">
              {connect.map((c, i) => (
                <button
                  key={c.url}
                  role="radio"
                  aria-checked={c === chosen}
                  class={`${c === chosen ? "btn-accent" : "btn-outline"} px-3 py-1 text-xs`}
                  onClick={() => setPick(i)}
                  data-testid={`mcp-where-${c.where}`}
                  title={c.url}
                >
                  {where[c.where] || c.where}
                </button>
              ))}
            </div>
          )}
          <p class="text-xs text-[var(--t3)] mb-1">
            {chosen.with_token
              ? t(
                  "Run this on the computer where Claude Code is. Copy command puts the token in; it is not shown here.",
                )
              : t(
                  "Run this on this computer, where Claude Code is. It reads the token from its file; the token is not shown here.",
                )}
          </p>
          <pre
            class="callout text-xs overflow-x-auto whitespace-pre-wrap break-all"
            data-testid="mcp-command"
          >
            {chosen.command}
          </pre>
          {chosen.url.startsWith("http://") && chosen.where !== "local" && (
            <p class="text-xs text-[var(--c-amber)] mt-1" data-testid="mcp-plain-http">
              {t(
                "Plain http: the token crosses the network unencrypted. Use it on a network you trust, or put https in front (public address).",
              )}
            </p>
          )}
          <div class="flex flex-wrap gap-2 mt-2">
            <button
              class="btn-outline px-3 py-1.5 text-sm"
              onClick={copyCommand}
              data-testid="mcp-copy"
            >
              {t("Copy command")}
            </button>
            <button
              class="btn-outline px-3 py-1.5 text-sm"
              onClick={copyToken}
              data-testid="mcp-copy-token"
            >
              {t("Copy token")}
            </button>
            <button class="btn-outline px-3 py-1.5 text-sm" onClick={test} data-testid="mcp-test">
              {t("Test connection")}
            </button>
            {!cur.token_managed && (
              <button
                class="btn-outline px-3 py-1.5 text-sm"
                disabled={disabled}
                onClick={rotate}
                data-testid="mcp-rotate"
              >
                {t("Rotate token")}
              </button>
            )}
          </div>
          {note && (
            <p class="text-xs text-[var(--t3)] mt-2" role="status" data-testid="mcp-note">
              {note}
            </p>
          )}
        </div>
      )}
      {cur.enabled && cur.oauth && (
        <div class="mt-4" data-testid="mcp-clients">
          <div class="text-sm font-semibold">{t("Signed-in clients")}</div>
          {clients.length === 0 && (
            <p class="text-xs text-[var(--t3)]">{t("Nobody has signed in yet.")}</p>
          )}
          {clients.map((c) => (
            <div
              key={c.id}
              class="callout flex items-center gap-2 mt-2"
              data-testid={`mcp-client-${c.id}`}
            >
              <span class="grow min-w-0">
                <span class="block text-sm font-semibold truncate">{c.name}</span>
                <span class="block text-xs text-[var(--t3)] truncate">
                  {c.host} · {t("since {when}", { when: new Date(c.since).toLocaleDateString() })}
                  {c.last_token &&
                    ` · ${t("last used {when}", { when: new Date(c.last_token).toLocaleString() })}`}
                </span>
              </span>
              {c.revoked ? (
                <span class="text-xs text-[var(--t3)]">{t("Revoked")}</span>
              ) : (
                <button
                  class="btn-outline px-3 py-1 text-xs"
                  onClick={() => revoke(c)}
                  data-testid="mcp-revoke"
                >
                  {t("Revoke")}
                </button>
              )}
            </div>
          ))}
        </div>
      )}
      {err && (
        <p class="text-sm text-[var(--c-red)] mt-2" role="alert">
          {err}
        </p>
      )}
    </div>
  );
}
