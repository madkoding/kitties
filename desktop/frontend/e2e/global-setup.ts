import fs from "node:fs";
import http from "node:http";
import path from "node:path";
import { GATEWAY, ROOT, STATE, WEBHOOK_PORT } from "./playwright.config";

// The seed: what the screens show. Connectors and models through the settings API, a skill, an
// artifact, a proposed skill as a file, and four chats whose turns the scripted brain answers.
const H = { Authorization: "Bearer e2e-token", "Content-Type": "application/json" };

async function api<T = unknown>(
  method: string,
  p: string,
  body?: unknown,
  ctype?: string,
): Promise<T> {
  const res = await fetch(GATEWAY + p, {
    method,
    headers: ctype ? { ...H, "Content-Type": ctype } : H,
    body: body === undefined ? undefined : ctype ? (body as string) : JSON.stringify(body),
  });
  if (!res.ok) {
    throw new Error(`${method} ${p}: ${res.status} ${await res.text()}`);
  }
  return (await res.json()) as T;
}

interface Chat {
  id: string;
  turns: { from: string; working?: boolean }[] | null;
}

const turnsOf = async (chat: string) =>
  (await api<{ chat: Chat }>("GET", `/v1/chats/${chat}`)).chat.turns || [];

async function newChat(members: string[], title?: string): Promise<string> {
  const r = await api<{ chat: Chat }>("POST", "/v1/chats", { members, title });
  return r.chat.id;
}

// say sends a message and waits for every kitty to have answered it.
async function say(chat: string, text: string): Promise<void> {
  const before = (await turnsOf(chat)).length;
  await api("POST", `/v1/chats/${chat}/messages`, { text });
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    const turns = await turnsOf(chat);
    const last = turns[turns.length - 1];
    if (turns.length > before + 1 && last && last.from !== "user" && !last.working) {
      return;
    }
    await new Promise((r) => setTimeout(r, 200));
  }
  throw new Error(`chat ${chat}: no answer to ${JSON.stringify(text)}`);
}

// A Slack webhook of our own, so the post the brain makes lands somewhere and says ok.
function webhookStub(): Promise<http.Server> {
  return new Promise((resolve) => {
    const srv = http.createServer((req, res) => {
      req.resume();
      req.on("end", () => {
        res.writeHead(200, { "Content-Type": "text/plain" });
        res.end("ok");
      });
    });
    srv.listen(WEBHOOK_PORT, "127.0.0.1", () => resolve(srv));
  });
}

export default async function globalSetup(): Promise<() => Promise<void>> {
  const stub = await webhookStub();
  const dataDir = path.join(ROOT, "desktop/frontend/e2e/.data/demo");

  await api("PUT", "/v1/settings/providers/fast", { kind: "openai", model: "gpt-5.5" });
  await api("PUT", "/v1/settings/connectors/team-slack", {
    kind: "slack",
    url: `http://127.0.0.1:${WEBHOOK_PORT}/hook`,
    description: "posts to the #launch channel",
  });
  await api("PUT", "/v1/settings/connectors/coder", {
    kind: "motita",
    description: "runs a coding task in motita and returns the result",
    url: "http://127.0.0.1:7477",
  });
  await api("PUT", "/v1/settings/connectors/github", {
    kind: "github",
    description: "issues and pull requests of the kitties repository",
    settings: { token: "ghp_demo", repo: "madkoding/kitties" },
  });
  await api("PUT", "/v1/settings/connectors/diagrams", {
    kind: "kroki",
    description: "renders diagrams to SVG",
  });
  await api("PUT", "/v1/settings/connectors/wiki", {
    kind: "notion",
    description: "the team wiki",
    settings: { token: "secret_demo", parent: "abc123" },
  });
  await api("PUT", "/v1/settings/connectors/workspace", {
    kind: "google-workspace",
    description: "mail, calendar and drive of the team account",
    settings: { client_id: "1234-demo.apps.googleusercontent.com" },
  });
  await api("PUT", "/v1/skills/weekly-report", {
    body: "# Weekly report\n\nHow the Friday report is written.\n\n1. Collect the numbers from the dashboard.\n2. Three bullets: what shipped, what slipped, what is next.\n3. Post it to #launch with the slack connector.",
  });
  await api(
    "PUT",
    "/v1/artifacts/competitor-notes.md",
    "# Competitor notes\n\n| Name | Price | Notes |\n| --- | --- | --- |\n| Acme | 12 | fast |",
    "text/markdown",
  );
  fs.mkdirSync(path.join(dataDir, "skills/proposed"), { recursive: true });
  fs.writeFileSync(
    path.join(dataDir, "skills/proposed/close-a-cookie-banner.md"),
    "# Close a cookie banner\n\nWhat worked on three sites.\n\n1. Look for a button named Accept, OK or Got it in the lower third of the page.\n2. Click it, wait one second and take a screenshot.\n",
  );

  const single = await newChat(["mimi"]);
  await say(single, "Hello Mimi! What can you do?");
  await say(single, "Could you **plan** a small launch for the kitties project?");
  const group = await newChat(["mimi", "tom", "luna"], "Launch team");
  await say(group, "Team, let us plan the launch. Who takes what?");
  await say(group, "@luna can you summarise the plan and @tom check the numbers?");
  const desktop = await newChat(["tom"], "Tom on the desktop");
  await say(desktop, "Tom, open https://example.com and tell me what the page says.");
  const post = await newChat(["mimi"], "Announcement");
  await say(post, "Mimi, please post the launch announcement to the team.");
  const tom = (await api<{ kitty: { id: string } }>("GET", "/v1/kitties/tom")).kitty.id;

  fs.writeFileSync(STATE, JSON.stringify({ single, group, desktop, post, tom }, null, 2));
  return async () => {
    await new Promise<void>((resolve) => stub.close(() => resolve()));
  };
}
