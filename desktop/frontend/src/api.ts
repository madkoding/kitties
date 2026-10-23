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
  // starting: its desktop is coming up (the first time, its image downloads).
  status: "created" | "starting" | "running" | "stopped" | "error";
  error?: string;
  connectors?: string[];
  soul: { name?: string; persona?: string; tone?: string; traits?: string[]; skills?: string[] };
  avatar: { breed: string; color: string };
  // Limits bound one task; zero fields take the defaults. Backup takes over a task this kitty gives up on.
  limits?: Limits;
  backup?: string;
  // approvals is "ask" (the default: an action that reaches other people waits for a yes until
  // the kitty is trusted with it) or "auto".
  approvals?: "ask" | "auto" | "";
}

// Approval is an action a kitty wants to take and is waiting to be allowed.
export interface Approval {
  id: string;
  kitty: string;
  kitty_name: string;
  connector: string;
  kind: string;
  action: string;
  args: Record<string, unknown>;
  summary: string;
  risk: "ask" | "always";
  chat?: string;
  status: "pending" | "approved" | "rejected";
  reason?: string;
  always?: boolean;
  at: string;
  decided_at?: string;
  result?: string;
  error?: string;
}

// ApprovalRule is what the person allowed for good.
export interface ApprovalRule {
  id: string;
  kitty: string;
  kitty_name?: string;
  connector: string;
  action: string;
  mode: "auto" | "ask";
  created: string;
}

export interface MemoryNote {
  id: string;
  scope: string;
  text: string;
  strength: number;
  source: string;
  pinned?: boolean;
  updated: string;
}

export interface MemoryProfile {
  demand: number;
  auto: boolean;
  up: number;
  down: number;
  pending: number;
}

// MemoryView is what a kitty knows about the person and how demanding they are.
export interface MemoryView {
  notes: MemoryNote[];
  profile: MemoryProfile;
  level: string;
  trust?: Record<string, { streak: number; needs: number }>;
  // What it used today against its daily limits (a limit of 0 is none).
  spent?: { tokens: number; actions: number; day_tokens: number; day_actions: number };
}

export interface Limits {
  max_tokens?: number;
  max_seconds?: number;
  max_rounds?: number;
  retries?: number;
  // The day's ceiling over all its work: tokens, and actions that reach other people.
  day_tokens?: number;
  day_actions?: number;
}

// Goal is something a kitty pursues on its own: on a schedule (every, a cron expression),
// when a connector reports an event (on), or when a person runs it.
export interface Goal {
  id: string;
  kitty: string;
  title: string;
  text: string;
  every?: string;
  on?: string[];
  enabled: boolean;
  chat?: string;
  created_at: string;
  last_run?: string;
  next_run?: string;
  runs: number;
  last_result?: string;
  from_file?: boolean;
}

// JournalEntry is one line of a kitty's journal: what it did and where it got stuck.
export interface JournalEntry {
  at: string;
  kitty: string;
  kind:
    | "run"
    | "done"
    | "blocked"
    | "retry"
    | "escalated"
    | "delegated"
    | "recovered"
    | "skipped"
    | "event";
  text: string;
  goal?: string;
  chat?: string;
}

// Delegation is a subtask one kitty handed to another.
export interface Delegation {
  kitty: string;
  kitty_id: string;
  chat: string;
  task: string;
  result?: string;
  error?: string;
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
  calls?: { name: string; kind?: string; output?: string; error?: string; artifact?: string }[];
  steps?: Step[];
  artifacts?: string[];
  skills?: string[];
  working?: boolean;
  needs_help?: boolean;
  delegations?: Delegation[];
  approvals?: { id: string; connector: string; action: string; summary: string }[];
  feedback?: "up" | "down" | "";
  usage?: { input: number; output: number };
}

export interface Session {
  id: string;
  title: string;
  members: string[];
  turns?: Turn[];
  created_at: string;
  updated_at: string;
  goal?: string;
  owner?: string;
  depth?: number;
  needs_help?: boolean;
}

export interface ProviderKind {
  Name: string;
  Family: string;
  BaseURL: string;
  KeyEnv: string;
  NoKey: boolean;
  DefaultModel: string;
  // Login names the account a profile of this kind can sign in with instead of a key.
  Login?: string;
}

// Account is one sign-in a profile can think on instead of a key.
export interface Account {
  kind: string;
  signed_in: boolean;
  who?: string;
  available: boolean;
  note?: string;
}

// LoginFlow is a sign-in in progress: open url (and type code), or paste back where the browser
// landed.
export interface LoginFlow {
  id: string;
  kind: string;
  method: "device" | "browser" | "instant";
  url?: string;
  code?: string;
  paste?: boolean;
  status: "pending" | "connected" | "failed";
  error?: string;
  who?: string;
}

export interface ProviderView {
  // used_by names the kitties that think with it.
  used_by?: string[];
  name: string;
  kind: string;
  model: string;
  base_url?: string;
  api_key_set: boolean;
  api_key_env?: string;
  // auth is "login" when it thinks on a signed-in account instead of a key.
  auth?: string;
  max_tokens?: number;
  temperature?: number;
  default: boolean;
  describe: string;
  from_file: boolean;
  error?: string;
}

export interface ConnectorView {
  // used_by names the kitties allowed to use it.
  used_by?: string[];
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
  needs_sign_in?: boolean;
  account?: string;
  // auth is how one that signs in reaches its account: "sign-in", "keys", "token", "webhook".
  auth?: string;
  // token_expires is when a typed token of a known lifetime stops working (RFC 3339).
  token_expires?: string;
}

// ConnectorTest is what testing a connection answered: tested false when it cannot be tested
// without acting.
export interface ConnectorTest {
  ok: boolean;
  tested: boolean;
  account?: string;
  error?: string;
  message?: string;
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
  // key groups the fields that, all filled in, stand for the sign-in ("token", "oauth1").
  key?: string;
}

export interface ConnectorKind {
  name: string;
  category: string;
  label: string;
  help: string;
  fields: ConnectorField[];
  actions: { name: string; help: string; args?: string[]; risk?: "ask" | "always" }[];
  oauth?: string;
  inbound?: boolean;
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
  // day starts the summary with the person's day: calendar, reminders and weather.
  notify: { connector: string; on: string[]; digest?: string; day?: boolean };
  notify_events: string[];
  accounts: Account[];
  // oauth_ready are the services a connection signs in to without an app of the person's own.
  oauth_ready?: string[];
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
  GatewayNote(): Promise<string>;
  Connect(url: string, token: string): Promise<string>;
}

declare global {
  interface Window {
    go?: { main?: { App?: WailsApp } };
  }
}

export const inWails = (): boolean => Boolean(window.go?.main?.App);

// The Android app is this same page inside a WebView; it adds a bridge to open links outside.
interface AndroidBridge {
  open(url: string): void;
}

export const androidBridge = (): AndroidBridge | undefined =>
  (window as unknown as { KittiesAndroid?: AndroidBridge }).KittiesAndroid;

export const inAndroid = (): boolean => Boolean(androidBridge());

// errText is the words of whatever was thrown: Wails rejects with a plain string.
export function errText(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

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

// cookieLogin says whether this page signs in with a session cookie: served by the gateway
// itself (or a proxy in front of it), not inside Wails and not pointed at another address.
export function cookieLogin(): boolean {
  const c = savedConnection();
  return !inWails() && !inAndroid() && !c.url && !c.token;
}

// fixSettings fills what an older gateway leaves out of the settings (or sends as null), so a
// page newer than its gateway shows empty lists instead of failing to draw.
function fixSettings<T>(path: string, data: T): T {
  if (
    !path.startsWith("/v1/settings") ||
    !data ||
    typeof data !== "object" ||
    !("providers" in data)
  ) {
    return data;
  }
  const v = data as unknown as Record<string, unknown>;
  for (const k of [
    "providers",
    "connectors",
    "kinds",
    "connector_kinds",
    "connector_catalog",
    "connector_categories",
    "notify_events",
    "accounts",
    "oauth_ready",
  ]) {
    if (!Array.isArray(v[k])) {
      v[k] = [];
    }
  }
  const n = (v.notify || {}) as { connector?: string; on?: string[] };
  v.notify = { ...n, connector: n.connector || "", on: Array.isArray(n.on) ? n.on : [] };
  return data;
}

// request performs one API call and returns its JSON.
export async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const app = window.go?.main?.App;
  if (app) {
    // A reading that gets no answer from the app fails with a message instead of leaving the
    // screen waiting for good (a message that never comes back through the webview's bridge).
    // Writes may legitimately take minutes, so only reads have a limit.
    // Wails rejects with a plain string, not an Error: keep its words.
    const call = app
      .Request(method, path, body === undefined ? "" : JSON.stringify(body))
      .catch((e: unknown) => {
        throw new ApiError(0, e instanceof Error ? e.message : String(e));
      });
    const raw = await (method === "GET"
      ? Promise.race([
          call,
          new Promise<never>((_, reject) =>
            setTimeout(
              () => reject(new ApiError(0, `the app got no answer for ${method} ${path} in 20 s`)),
              20_000,
            ),
          ),
        ])
      : call);
    const parsed = JSON.parse(raw) as { status: number; body: string };
    const data = parsed.body ? JSON.parse(parsed.body) : {};
    if (parsed.status >= 400) {
      throw new ApiError(parsed.status, data.error || `HTTP ${parsed.status}`);
    }
    return fixSettings(path, data as T);
  }
  let conn = savedConnection();
  if (inAndroid() && (!conn.url || !conn.token)) {
    // The app has no gateway of its own: it asks for the address and the token first.
    throw new ApiError(401, "Connect the app to your Kitties gateway: its address and token.");
  }
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
  return fixSettings(path, data as T);
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
  kitty: (ref: string) =>
    request<{ kitty: Kitty }>("GET", `/v1/kitties/${ref}`).then((r) => r.kitty),
  createKitty: (spec: {
    name: string;
    provider?: string;
    soul?: Kitty["soul"];
    desktop?: boolean;
    avatar?: Kitty["avatar"];
    connectors?: string[];
  }) => request<{ kitty: Kitty; warning?: string }>("POST", "/v1/kitties", spec),
  editKitty: (
    ref: string,
    u: {
      provider?: string;
      connectors?: string[];
      avatar?: Kitty["avatar"];
      soul?: Kitty["soul"];
      limits?: Limits;
      backup?: string;
      approvals?: string;
    },
  ) => request<{ kitty: Kitty }>("PATCH", `/v1/kitties/${ref}`, u).then((r) => r.kitty),
  startKitty: (ref: string) => request("POST", `/v1/kitties/${ref}/start`),
  stopKitty: (ref: string) => request("POST", `/v1/kitties/${ref}/stop`),
  removeKitty: (ref: string) => request("DELETE", `/v1/kitties/${ref}`),
  // connectorNames are the connections a kitty may be given.
  connectorNames: () =>
    request<{ connectors: Record<string, string> }>("GET", "/v1/connectors").then((r) =>
      Object.keys(r.connectors || {}).sort(),
    ),
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
    request<{ turns: Turn[] }>("POST", `/v1/chats/${id}/messages`, { text }).then(
      (r) => r.turns || [],
    ),
  cancel: (id: string) => request("POST", `/v1/chats/${id}/cancel`),
  settings: () => request<SettingsView>("GET", "/v1/settings"),
  setProvider: (name: string, p: Record<string, unknown>) =>
    request<SettingsView>("PUT", `/v1/settings/providers/${name}`, p),
  removeProvider: (name: string) =>
    request<SettingsView>("DELETE", `/v1/settings/providers/${name}`),
  testProvider: (name: string) =>
    request<{ ok: boolean; reply?: string; error?: string }>(
      "POST",
      `/v1/settings/providers/${name}/test`,
    ),
  testConnector: (name: string) =>
    request<ConnectorTest>("POST", `/v1/settings/connectors/${name}/test`),
  startLogin: (kind: string, project?: string) =>
    request<LoginFlow>("POST", `/v1/settings/accounts/${kind}/login`, project ? { project } : {}),
  loginFlow: (id: string) => request<LoginFlow>("GET", `/v1/settings/logins/${id}`),
  pasteLogin: (id: string, code: string) =>
    request<LoginFlow>("POST", `/v1/settings/logins/${id}/paste`, { code }),
  cancelLogin: (id: string) => request("DELETE", `/v1/settings/logins/${id}`),
  signOut: (kind: string) => request<SettingsView>("DELETE", `/v1/settings/accounts/${kind}`),
  setDefaultProvider: (name: string) =>
    request<SettingsView>("PUT", "/v1/settings/default-provider", { name }),
  // signingIn saves a connection that signs in before its sign-in, so without an account yet.
  setConnector: (name: string, c: Record<string, unknown>, signingIn = false) =>
    request<SettingsView>(
      "PUT",
      `/v1/settings/connectors/${name}${signingIn ? "?sign_in=1" : ""}`,
      c,
    ),
  removeConnector: (name: string) =>
    request<SettingsView>("DELETE", `/v1/settings/connectors/${name}`),
  skills: () => request<{ skills: Skill[] }>("GET", "/v1/skills").then((r) => r.skills || []),
  skill: (name: string) =>
    request<{ skill: Skill }>("GET", `/v1/skills/${name}`).then((r) => r.skill),
  saveSkill: (name: string, body: string) =>
    request<{ skill: Skill }>("PUT", `/v1/skills/${name}`, { body }).then((r) => r.skill),
  deleteSkill: (name: string) => request("DELETE", `/v1/skills/${name}`),
  acceptSkill: (name: string) =>
    request<{ skill: Skill }>("POST", `/v1/skills/${name}/accept`).then((r) => r.skill),
  artifacts: () =>
    request<{ artifacts: ArtifactInfo[] }>("GET", "/v1/artifacts").then((r) => r.artifacts || []),
  deleteArtifact: (name: string) => request("DELETE", `/v1/artifacts/${encodeURIComponent(name)}`),
  goals: (kitty?: string) =>
    request<{ goals: Goal[] }>(
      "GET",
      "/v1/goals" + (kitty ? "?kitty=" + encodeURIComponent(kitty) : ""),
    ).then((r) => r.goals || []),
  addGoal: (g: {
    kitty: string;
    title: string;
    text?: string;
    every?: string;
    on?: string[];
    enabled?: boolean;
  }) => request<{ goal: Goal }>("POST", "/v1/goals", g).then((r) => r.goal),
  updateGoal: (
    id: string,
    g: { title: string; text?: string; every?: string; on?: string[]; enabled?: boolean },
  ) => request<{ goal: Goal }>("PUT", `/v1/goals/${id}`, g).then((r) => r.goal),
  removeGoal: (id: string) => request("DELETE", `/v1/goals/${id}`),
  runGoal: (id: string) =>
    request<{ ok: boolean; chat: string }>("POST", `/v1/goals/${id}/run`).then((r) => r.chat),
  approvals: (status = "") =>
    request<{ approvals: Approval[] }>(
      "GET",
      "/v1/approvals" + (status ? "?status=" + status : ""),
    ).then((r) => r.approvals || []),
  approve: (id: string, always = false) =>
    request<{ approval: Approval }>("POST", `/v1/approvals/${id}/approve`, { always }).then(
      (r) => r.approval,
    ),
  reject: (id: string, reason = "") =>
    request<{ approval: Approval }>("POST", `/v1/approvals/${id}/reject`, { reason }).then(
      (r) => r.approval,
    ),
  approvalRules: () =>
    request<{ rules: ApprovalRule[] }>("GET", "/v1/approvals/rules").then((r) => r.rules || []),
  removeRule: (id: string) => request("DELETE", `/v1/approvals/rules/${id}`),
  memory: (kitty: string) => request<MemoryView>("GET", `/v1/kitties/${kitty}/memory`),
  remember: (kitty: string, text: string, scope = "user") =>
    request<{ note: MemoryNote }>("POST", `/v1/kitties/${kitty}/memory`, { text, scope }).then(
      (r) => r.note,
    ),
  forgetNote: (kitty: string, id: string) => request("DELETE", `/v1/kitties/${kitty}/memory/${id}`),
  setProfile: (kitty: string, demand: number, auto: boolean) =>
    request<{ profile: MemoryProfile }>("PUT", `/v1/kitties/${kitty}/memory/profile`, {
      demand,
      auto,
    }).then((r) => r.profile),
  distill: (kitty: string) =>
    request<{ notes: MemoryNote[] }>("POST", `/v1/kitties/${kitty}/memory/distill`).then(
      (r) => r.notes,
    ),
  feedback: (chat: string, index: number, rating: "up" | "down" | "", note = "") =>
    request("POST", `/v1/chats/${chat}/turns/${index}/feedback`, { rating, note }),
  setNotify: (c: { connector: string; on: string[]; digest?: string }) =>
    request<SettingsView>("PUT", "/v1/settings/notify", c),
  testNotify: (connector = "") =>
    request<{ ok: boolean; error?: string }>("POST", "/v1/settings/notify/test", { connector }),
  // The browser session of a gateway served on a network: who is signed in, and the way in and out.
  session: () => request<{ signed_in: boolean; local_token: boolean }>("GET", "/v1/session"),
  login: (token: string) => request<{ ok: boolean }>("POST", "/v1/login", { token: token.trim() }),
  logout: () => request<{ ok: boolean }>("POST", "/v1/logout"),
  sendDigest: () => request<{ ok: boolean; error?: string }>("POST", "/v1/settings/notify/digest"),
  journal: (kitty: string, n = 50) =>
    request<{ journal: JournalEntry[] }>("GET", `/v1/kitties/${kitty}/journal?n=${n}`).then(
      (r) => r.journal || [],
    ),
};

// oauthStart asks the gateway for the consent URL of a connector's sign-in.
export const oauthStart = (name: string) =>
  request<{ url: string }>("POST", `/v1/oauth/${name}/start`).then((r) => r.url);

// openExternal opens a URL in the person's browser: through Wails when inside it.
export function openExternal(url: string): void {
  const android = androidBridge();
  if (android) {
    android.open(url);
    return;
  }
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
  const rt = (
    window as unknown as {
      runtime?: { EventsOn?: (name: string, cb: (data: unknown) => void) => () => void };
    }
  ).runtime;
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
