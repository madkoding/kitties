// The gateway, from the browser or from inside the Wails app.
//
// In Wails the Go side owns the connection (URL and token) and proxies every call through the
// bound App.Request method, so the page never holds the token. In a browser the page talks to
// the gateway itself: same-origin through vite's proxy, or the URL and token the person gave.

export interface Kitty {
  id: string;
  name: string;
  provider: string;
  desktop: boolean;
  container?: string;
  status: "created" | "running" | "stopped" | "error";
  error?: string;
  connectors?: string[];
  soul: { name?: string; persona?: string; tone?: string; traits?: string[]; skills?: string[] };
  avatar: { breed: string; color: string };
}

export interface Step {
  at: string;
  action: string;
  args?: Record<string, unknown>;
  result?: string;
  error?: string;
  shot?: string;
}

export interface Turn {
  at: string;
  from: string;
  kitty?: string;
  text: string;
  calls?: { name: string; output?: string; error?: string; artifact?: string }[];
  steps?: Step[];
  artifacts?: string[];
  skills?: string[];
  working?: boolean;
  usage?: { input: number; output: number };
}

export interface Session {
  id: string;
  title: string;
  members: string[];
  turns?: Turn[];
  created_at: string;
  updated_at: string;
}

export interface ProviderKind {
  Name: string;
  Family: string;
  BaseURL: string;
  KeyEnv: string;
  NoKey: boolean;
  DefaultModel: string;
}

export interface ProviderView {
  name: string;
  kind: string;
  model: string;
  base_url?: string;
  api_key_set: boolean;
  api_key_env?: string;
  max_tokens?: number;
  temperature?: number;
  default: boolean;
  describe: string;
  from_file: boolean;
  error?: string;
}

export interface ConnectorView {
  name: string;
  kind: string;
  category: string;
  label: string;
  description?: string;
  url?: string;
  token_set: boolean;
  chat_id?: string;
  command?: string;
  settings: Record<string, string>;
  secrets_set: string[];
  from_file: boolean;
  oauth?: string;
  signed_in?: boolean;
  account?: string;
}

export interface ChatEvent {
  type: "hello" | "turn" | "update" | "done" | "deleted";
  session?: string;
  index?: number;
  turn?: Turn;
}

export interface ConnectorField {
  name: string;
  label: string;
  help?: string;
  placeholder?: string;
  secret?: boolean;
  required?: boolean;
}

export interface ConnectorKind {
  name: string;
  category: string;
  label: string;
  help: string;
  fields: ConnectorField[];
  actions: { name: string; help: string; args?: string[] }[];
  oauth?: string;
}

export interface SettingsView {
  providers: ProviderView[];
  connectors: ConnectorView[];
  default_provider: string;
  kinds: ProviderKind[];
  connector_kinds: string[];
  connector_catalog: ConnectorKind[];
  connector_categories: string[];
  motita_url?: string;
}

export interface Skill {
  name: string;
  title: string;
  summary: string;
  body?: string;
  builtin: boolean;
  proposed: boolean;
  updated_at: string;
}

export interface ArtifactInfo {
  name: string;
  type: string;
  size: number;
  updated_at: string;
  by?: string;
}

export interface ProviderInfo {
  name: string;
  kind: string;
  model: string;
  describe: string;
}

interface WailsApp {
  Request(method: string, path: string, body: string): Promise<string>;
  Gateway(): Promise<{ url: string; token: string }>;
  Connect(url: string, token: string): Promise<string>;
}

declare global {
  interface Window {
    go?: { main?: { App?: WailsApp } };
  }
}

export const inWails = (): boolean => Boolean(window.go?.main?.App);

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
  }
}

const KEY = "kitties.gateway";

export interface Connection {
  url: string;
  token: string;
}

// A link can carry the connection (?gateway=...&token=...): the gateway's own page and a shared
// link use it. It is saved once and dropped from the address bar.
(function connectionFromLink() {
  try {
    const q = new URLSearchParams(location.search);
    if (q.has("token") || q.has("gateway")) {
      const saved = savedConnection();
      saveConnection({ url: q.get("gateway") ?? saved.url, token: q.get("token") ?? saved.token });
      q.delete("token");
      q.delete("gateway");
      const rest = q.toString();
      history.replaceState(null, "", location.pathname + (rest ? "?" + rest : "") + location.hash);
    }
  } catch {
    /* no history in this context */
  }
})();

export function savedConnection(): Connection {
  try {
    const raw = JSON.parse(localStorage.getItem(KEY) || "{}");
    return { url: raw.url || "", token: raw.token || "" };
  } catch {
    return { url: "", token: "" };
  }
}

export function saveConnection(c: Connection): void {
  try {
    localStorage.setItem(KEY, JSON.stringify(c));
  } catch {
    /* private window */
  }
}

// request performs one API call and returns its JSON.
export async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const app = window.go?.main?.App;
  if (app) {
    const raw = await app.Request(method, path, body === undefined ? "" : JSON.stringify(body));
    const parsed = JSON.parse(raw) as { status: number; body: string };
    const data = parsed.body ? JSON.parse(parsed.body) : {};
    if (parsed.status >= 400) {
      throw new ApiError(parsed.status, data.error || `HTTP ${parsed.status}`);
    }
    return data as T;
  }
  let conn = savedConnection();
  if (!conn.token) {
    conn = await fetchLocalToken(conn);
  }
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  if (conn.token) {
    headers.Authorization = "Bearer " + conn.token;
  }
  const res = await fetch(conn.url.replace(/\/$/, "") + path, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (res.status === 401 && conn.token) {
    // The gateway may have restarted with a new token: the local one is tried once more.
    const fresh = await fetchLocalToken({ url: conn.url, token: "" });
    if (fresh.token && fresh.token !== conn.token) {
      return request(method, path, body);
    }
  }
  const text = await res.text();
  let data: { error?: string } = {};
  try {
    data = text ? JSON.parse(text) : {};
  } catch {
    data = { error: text.slice(0, 200) };
  }
  if (!res.ok) {
    throw new ApiError(res.status, data.error || res.statusText);
  }
  return data as T;
}

// fetchLocalToken asks the gateway for its token, which it hands out only to this machine, and
// saves it. The page served by the gateway itself, or opened on the same computer, needs no typing.
async function fetchLocalToken(conn: Connection): Promise<Connection> {
  try {
    const res = await fetch(conn.url.replace(/\/$/, "") + "/v1/local-token");
    if (!res.ok) {
      return conn;
    }
    const data = (await res.json()) as { token?: string };
    if (data.token) {
      const next = { url: conn.url, token: data.token };
      saveConnection(next);
      return next;
    }
  } catch {
    /* not local, or not running */
  }
  return conn;
}

// shotURL is where a screenshot can be loaded from as an image.
export function shotURL(path: string): string {
  const conn = savedConnection();
  const base = conn.url.replace(/\/$/, "");
  const sep = path.includes("?") ? "&" : "?";
  return conn.token ? `${base}${path}${sep}token=${encodeURIComponent(conn.token)}` : base + path;
}

// shotData loads a screenshot as a data: URL through the API (the only way inside Wails).
export async function shotData(path: string): Promise<string> {
  const app = window.go?.main?.App;
  if (!app) {
    return shotURL(path);
  }
  const raw = await app.Request("GET", path, "");
  const parsed = JSON.parse(raw) as { status: number; body: string; base64?: string };
  if (parsed.status >= 400 || !parsed.base64) {
    throw new ApiError(parsed.status, "no screenshot");
  }
  return "data:image/png;base64," + parsed.base64;
}

export const api = {
  health: () => request<{ ok: boolean; version: string; containers: boolean }>("GET", "/v1/health"),
  kitties: () => request<{ kitties: Kitty[] }>("GET", "/v1/kitties").then((r) => r.kitties || []),
  kitty: (ref: string) => request<{ kitty: Kitty }>("GET", `/v1/kitties/${ref}`).then((r) => r.kitty),
  createKitty: (spec: { name: string; provider?: string; soul?: Kitty["soul"]; desktop?: boolean; avatar?: Kitty["avatar"] }) =>
    request<{ kitty: Kitty; warning?: string }>("POST", "/v1/kitties", spec),
  editKitty: (ref: string, u: { avatar?: Kitty["avatar"]; soul?: Kitty["soul"] }) =>
    request<{ kitty: Kitty }>("PATCH", `/v1/kitties/${ref}`, u).then((r) => r.kitty),
  startKitty: (ref: string) => request("POST", `/v1/kitties/${ref}/start`),
  stopKitty: (ref: string) => request("POST", `/v1/kitties/${ref}/stop`),
  removeKitty: (ref: string) => request("DELETE", `/v1/kitties/${ref}`),
  providers: () =>
    request<{ providers: ProviderInfo[]; kinds: string[] }>("GET", "/v1/providers").then(
      (r) => r.providers || [],
    ),
  chats: () => request<{ chats: Session[] }>("GET", "/v1/chats").then((r) => r.chats || []),
  chat: (id: string) => request<{ chat: Session }>("GET", `/v1/chats/${id}`).then((r) => r.chat),
  createChat: (title: string, members: string[]) =>
    request<{ chat: Session }>("POST", "/v1/chats", { title, members }).then((r) => r.chat),
  deleteChat: (id: string) => request("DELETE", `/v1/chats/${id}`),
  addMembers: (id: string, members: string[]) =>
    request<{ chat: Session }>("POST", `/v1/chats/${id}/members`, { members }).then((r) => r.chat),
  send: (id: string, text: string) =>
    request<{ turns: Turn[] }>("POST", `/v1/chats/${id}/messages`, { text }).then((r) => r.turns || []),
  cancel: (id: string) => request("POST", `/v1/chats/${id}/cancel`),
  settings: () => request<SettingsView>("GET", "/v1/settings"),
  setProvider: (name: string, p: Record<string, unknown>) =>
    request<SettingsView>("PUT", `/v1/settings/providers/${name}`, p),
  removeProvider: (name: string) => request<SettingsView>("DELETE", `/v1/settings/providers/${name}`),
  testProvider: (name: string) =>
    request<{ ok: boolean; reply?: string; error?: string }>("POST", `/v1/settings/providers/${name}/test`),
  setDefaultProvider: (name: string) =>
    request<SettingsView>("PUT", "/v1/settings/default-provider", { name }),
  setConnector: (name: string, c: Record<string, unknown>) =>
    request<SettingsView>("PUT", `/v1/settings/connectors/${name}`, c),
  removeConnector: (name: string) => request<SettingsView>("DELETE", `/v1/settings/connectors/${name}`),
  skills: () => request<{ skills: Skill[] }>("GET", "/v1/skills").then((r) => r.skills || []),
  skill: (name: string) => request<{ skill: Skill }>("GET", `/v1/skills/${name}`).then((r) => r.skill),
  saveSkill: (name: string, body: string) => request<{ skill: Skill }>("PUT", `/v1/skills/${name}`, { body }).then((r) => r.skill),
  deleteSkill: (name: string) => request("DELETE", `/v1/skills/${name}`),
  acceptSkill: (name: string) => request<{ skill: Skill }>("POST", `/v1/skills/${name}/accept`).then((r) => r.skill),
  artifacts: () => request<{ artifacts: ArtifactInfo[] }>("GET", "/v1/artifacts").then((r) => r.artifacts || []),
  deleteArtifact: (name: string) => request("DELETE", `/v1/artifacts/${encodeURIComponent(name)}`),
};

// oauthStart asks the gateway for the consent URL of a connector's sign-in.
export const oauthStart = (name: string) => request<{ url: string }>("POST", `/v1/oauth/${name}/start`).then((r) => r.url);

// openExternal opens a URL in the person's browser: through Wails when inside it.
export function openExternal(url: string): void {
  const rt = (window as unknown as { runtime?: { BrowserOpenURL?: (u: string) => void } }).runtime;
  if (rt?.BrowserOpenURL) {
    rt.BrowserOpenURL(url);
    return;
  }
  window.open(url, "_blank", "noopener");
}

// watchChat follows a chat's events as they happen. In a browser it is an EventSource on the
// gateway; inside Wails the Go side reads the stream and emits runtime events. It returns the
// function that stops watching.
export function watchChat(id: string, onEvent: (e: ChatEvent) => void): () => void {
  const app = window.go?.main?.App;
  const rt = (window as unknown as { runtime?: { EventsOn?: (name: string, cb: (data: unknown) => void) => () => void } }).runtime;
  if (app && (app as unknown as { Watch?: (id: string) => Promise<void> }).Watch && rt?.EventsOn) {
    const off = rt.EventsOn("kitties:chat:" + id, (data) => onEvent(data as ChatEvent));
    void (app as unknown as { Watch: (id: string) => Promise<void> }).Watch(id);
    return () => {
      off();
      void (app as unknown as { Unwatch?: (id: string) => Promise<void> }).Unwatch?.(id);
    };
  }
  if (typeof EventSource === "undefined") {
    return () => {};
  }
  const es = new EventSource(shotURL(`/v1/chats/${id}/events`));
  for (const type of ["turn", "update", "done", "deleted"]) {
    es.addEventListener(type, (ev) => {
      try {
        onEvent(JSON.parse((ev as MessageEvent).data) as ChatEvent);
      } catch {
        /* a malformed event is dropped */
      }
    });
  }
  return () => es.close();
}

// artifactText loads a text artifact for reading in the page.
export async function artifactText(name: string): Promise<string> {
  const app = window.go?.main?.App;
  const path = `/v1/artifacts/${encodeURIComponent(name)}`;
  if (app) {
    const raw = await app.Request("GET", path, "");
    const parsed = JSON.parse(raw) as { status: number; body: string; base64?: string };
    if (parsed.status >= 400) {
      throw new ApiError(parsed.status, "no artifact");
    }
    return parsed.base64 ? "(binary file)" : parsed.body;
  }
  const res = await fetch(shotURL(path));
  if (!res.ok) {
    throw new ApiError(res.status, "no artifact");
  }
  return res.text();
}
