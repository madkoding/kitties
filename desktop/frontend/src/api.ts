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
  soul: {
    name?: string;
    persona?: string;
    tone?: string;
    traits?: string[];
    skills?: string[];
    // voice reads its WhatsApp voice notes (alloy, nova, …); empty or "default": the gateway's.
    voice?: string;
  };
  avatar: { breed: string; color: string };
  // Limits bound one task; zero fields take the defaults. Backup takes over a task this kitty gives up on.
  limits?: Limits;
  backup?: string;
  // lead marks the principal kitty, the orchestrator of the fleet (one at most): it hands the
  // work to the others and their reports reach the person through it.
  lead?: boolean;
  // approvals is "ask" (the default: an action that reaches other people waits for a yes until
  // the kitty is trusted with it) or "auto".
  approvals?: "ask" | "auto" | "";
  // theme is the look of its desktop (a Lumen (Openbox) theme); empty is the image's own, Numix (dark).
  theme?: string;
  // auto_off_minutes is how long its machine stays on without use (0 is never); absent is the
  // gateway's default. off_reason is why the machine is off (idle, manual or kitty).
  auto_off_minutes?: number;
  off_reason?: "idle" | "manual" | "kitty" | "";
}

// Power is the state of a kitty's machine (its desktop): on, starting, off or error, what keeps it
// on and when it turns itself off.
export interface Power {
  state: "on" | "starting" | "off" | "error";
  // step: what a starting machine is doing ("desktop": its container is up, its desktop coming up).
  step?: "desktop";
  error?: string;
  off_reason?: "idle" | "manual" | "kitty" | "";
  auto_off_minutes: number;
  auto_off_custom: boolean;
  // memory_mb and swap_mb are the RAM and swap the machine is given (0 RAM: the engine's own);
  // memory_custom is whether they are the kitty's own, memory_pending whether they wait for the
  // next start; memory_local is false in the cloud, where the machine's size is the provider's.
  memory_mb?: number;
  swap_mb?: number;
  memory_custom?: boolean;
  memory_pending?: boolean;
  memory_local?: boolean;
  in_use: boolean;
  in_use_by?: "task" | "control" | "watch" | "command";
  idle_seconds: number;
  off_in_seconds?: number;
}

// Memory is what a kitty's machine has and uses now, in MB, and the programs that hold the most.
export interface Memory {
  total_mb: number;
  used_mb: number;
  available_mb: number;
  swap_total_mb: number;
  swap_used_mb: number;
  top?: { name: string; mb: number; count: number }[];
}

// Machines are the RAM and swap of every machine without values of its own, and what the
// container engine has (engine_mb, absent when it cannot tell).
export interface Machines {
  memory_mb: number;
  swap_mb: number;
  engine_mb?: number;
}

// THEMES are the looks a kitty's desktop can wear, as the gateway knows them.
export const THEMES: [string, string][] = [
  ["", "Numix"],
  ["Greybird-dark", "Greybird"],
];

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
  mode: "auto" | "ask" | "never";
  created: string;
}

export interface MemoryNote {
  id: string;
  scope: string;
  text: string;
  strength: number;
  source: string;
  pinned?: boolean;
  proposed?: boolean;
  at?: string;
  uses?: number;
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
  // true: stops at the end of its rounds and waits; false (default): works on its own to the end.
  attended?: boolean;
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
  // A run that did not finish is picked up again by itself: how many in a row, and the next one.
  follow_ups?: number;
  follow_up_at?: string;
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
    | "off"
    | "limit"
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
  // answer is the whole answer when result had to be cut.
  answer?: string;
  error?: string;
  // limits says what the lead gave the kitty for this task alone, when it did.
  limits?: string;
}

export interface Step {
  at: string;
  action: string;
  args?: Record<string, unknown>;
  result?: string;
  error?: string;
  shot?: string;
}

// Attachment is a file the person attached to a message; the gateway keeps its bytes.
export interface Attachment {
  id: string;
  name: string;
  mime: string;
  // kind is what the file really is: image, pdf, text, doc or file.
  kind: "image" | "pdf" | "text" | "doc" | "file";
  size: number;
}

// The most a message may carry; the gateway checks the same limits by the file's real type.
export const ATTACH_MAX_FILES = 20;
export const ATTACH_MAX_TOTAL = 100 << 20;
export const ATTACH_MAX_IMAGE = 5 << 20;
export const ATTACH_MAX_FILE = 50 << 20;

export interface Turn {
  at: string;
  from: string;
  kitty?: string;
  text: string;
  calls?: { name: string; kind?: string; output?: string; error?: string; artifact?: string }[];
  steps?: Step[];
  artifacts?: string[];
  skills?: string[];
  attachments?: Attachment[];
  // resumable: the task stopped on its budget and can go on from where it was.
  resumable?: boolean;
  working?: boolean;
  needs_help?: boolean;
  delegations?: Delegation[];
  approvals?: { id: string; connector: string; action: string; summary: string }[];
  feedback?: "up" | "down" | "";
  // detail is the technical form of what the text says in plain words (an error as written).
  detail?: string;
  // limits are the limits the kitty set on itself in this turn (an empty mode lifted one).
  limits?: { connector: string; action: string; mode: string; previous?: string }[];
  usage?: { input: number; output: number; cache_read?: number; cache_write?: number };
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
  // reports marks the lead kitty's Reports thread, where the other kitties' news land.
  reports?: boolean;
  // gist is the chat's last words (the row shows up to two lines), secrets masked; gist_by who said them ("user" or
  // a kitty's name).
  gist?: string;
  gist_by?: string;
  // exposed marks a chat where whoever writes is not the person (a WhatsApp contact, a group, a
  // public chat): the guardrails apply; audience says who writes there.
  exposed?: boolean;
  audience?: string;
  // delegates are the kitties that took part through a hand-over without being members (they do
  // not answer what the person writes); kitties is every kitty of the chat: members, the kitty
  // that opened it to delegate, and delegates. More than one makes it a group of kitties.
  delegates?: string[];
  kitties?: string[];
  // helping are the kitties at work right now on a subtask handed over from this chat.
  helping?: Helper[];
  // dismissed are delegates the person took off the chat.
  dismissed?: string[];
}

// HandOverStat is how one kitty has handed work out (asked) and taken it on (took), and how much
// of each failed, over every chat kept.
export interface HandOverStat {
  kitty_id: string;
  kitty: string;
  asked: number;
  asked_failed: number;
  took: number;
  took_failed: number;
  // threads are the hand-over threads it took part in, the latest first.
  threads?: { id: string; title: string; at: string }[];
}

// Helper is a kitty at work on a subtask handed over from a chat.
export interface Helper {
  kitty: string;
  kitty_id: string;
  // from is the kitty that asked; chat the thread between the two.
  from: string;
  task: string;
  chat: string;
  since: string;
}

// chatKitties is every kitty taking part in a chat, members first (a gateway older than the
// field gives only the members).
export function chatKitties(c: Session): string[] {
  return c.kitties && c.kitties.length ? c.kitties : c.members;
}

export interface ProviderKind {
  Name: string;
  Family: string;
  BaseURL: string;
  KeyEnv: string;
  NoKey: boolean;
  // PublicList: the model list needs no key (Ollama Cloud), so it can be shown right away.
  PublicList?: boolean;
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
  // missing is true when the program the sign-in runs is not installed (claude).
  missing?: boolean;
  // browser is true when it signs in in the browser instead of with a program (Google).
  browser?: boolean;
}

// LoginFlow is a sign-in in progress: open url (and type code), or paste back where the browser
// landed.
export interface LoginFlow {
  id: string;
  kind: string;
  method: "device" | "browser" | "instant" | "install";
  url?: string;
  code?: string;
  paste?: boolean;
  status: "pending" | "connected" | "failed";
  error?: string;
  who?: string;
  // log is the last lines the Claude Code installer printed, while it runs.
  log?: string;
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
  // fallback is the model thought with while this one is down.
  fallback?: string;
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
  // helping: a kitty started or ended a subtask for the chat (no turn: the chat is read again).
  type: "hello" | "turn" | "update" | "done" | "deleted" | "helping";
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
  // key groups the fields that, all filled in, stand for the sign-in ("token", "oauth1",
  // "webhook").
  key?: string;
  // lifetime is how long, in seconds, a token typed in the field lasts.
  lifetime?: number;
  // options are the values the field picks several of, saved joined by commas.
  options?: string[];
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
  notify: { connector: string; on: string[]; digest?: string; day?: boolean; replies?: boolean };
  notify_events: string[];
  // autonomy is how long the kitties keep at a task on their own: empty takes the defaults.
  autonomy?: { follow_ups: string; brain_wait: string; stand_ins?: string; lead_limits?: string };
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
  SignInQR(base: string): Promise<string>;
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

// A link can carry the token (?token=...): the gateway's own page and a shared link use it. It is
// saved once and dropped from the address bar. The gateway's address never comes from a link: a
// link could point the page, and the token it holds, at another server.
(function connectionFromLink() {
  try {
    const q = new URLSearchParams(location.search);
    if (q.has("token")) {
      const saved = savedConnection();
      saveConnection({ url: saved.url, token: q.get("token") ?? saved.token });
      q.delete("token");
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
    return { url: safeGatewayBase(raw.url || ""), token: raw.token || "" };
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
  // A file (a Blob) goes as it is, never as JSON; the app's bridge only carries text.
  const raw = body instanceof Blob;
  if (app && raw) {
    throw new ApiError(0, "Restoring a file works from the browser: open Kitties at its address.");
  }
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
  const headers: Record<string, string> = raw ? {} : { "Content-Type": "application/json" };
  if (conn.token) {
    headers.Authorization = "Bearer " + conn.token;
  }
  const res = await fetch(safeGatewayBase(conn.url) + path, {
    method,
    headers,
    body: body === undefined ? undefined : raw ? (body as Blob) : JSON.stringify(body),
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
    const res = await fetch(safeGatewayBase(conn.url) + "/v1/local-token");
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

// gatewayAddress turns what a person typed into the gateway's address: a bare "host:port" is
// taken as http, and anything that is not an http(s) address is refused (null). Empty means this
// page's own gateway.
export function gatewayAddress(typed: string): string | null {
  const text = typed.trim();
  if (!text) {
    return "";
  }
  const full = /^[a-z][a-z0-9+.-]*:\/\//i.test(text) ? text : "http://" + text;
  return safeGatewayBase(full) || null;
}

// sendsInTheClear says whether an address would carry the token unencrypted over a network that
// is not the person's own: plain http to a host that is not this computer or a home/office address.
export function sendsInTheClear(address: string): boolean {
  try {
    const u = new URL(address);
    if (u.protocol !== "http:") {
      return false;
    }
    const h = u.hostname;
    return !(
      h === "localhost" ||
      h === "[::1]" ||
      h.endsWith(".local") ||
      /^127\./.test(h) ||
      /^10\./.test(h) ||
      /^192\.168\./.test(h) ||
      /^172\.(1[6-9]|2\d|3[01])\./.test(h)
    );
  } catch {
    return false;
  }
}

// safeGatewayBase is the gateway address to build requests on: an http(s) address without a
// trailing slash, or "" (this page's own origin) for anything else.
function safeGatewayBase(url: string): string {
  try {
    const parsed = new URL(url);
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
      return "";
    }
    return parsed.toString().replace(/\/$/, "");
  } catch {
    return "";
  }
}

// shotURL is where a screenshot can be loaded from as an image.
export function shotURL(path: string): string {
  const conn = savedConnection();
  const base = safeGatewayBase(conn.url);
  const sep = path.includes("?") ? "&" : "?";
  return conn.token ? `${base}${path}${sep}token=${encodeURIComponent(conn.token)}` : base + path;
}

// shotData loads a screenshot as a data: URL through the API (the only way inside Wails).
export async function shotData(path: string, mime = "image/png"): Promise<string> {
  const app = window.go?.main?.App;
  if (!app) {
    return shotURL(path);
  }
  const raw = await app.Request("GET", path, "");
  const parsed = JSON.parse(raw) as { status: number; body: string; base64?: string };
  if (parsed.status >= 400 || !parsed.base64) {
    throw new ApiError(parsed.status, "no screenshot");
  }
  return `data:${mime};base64,` + parsed.base64;
}

// fileBase64 reads a file as base64, the way the gateway takes it inside a message.
export function fileBase64(file: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result).split(",", 2)[1] || "");
    reader.onerror = () => reject(reader.error || new Error("could not read the file"));
    reader.readAsDataURL(file);
  });
}

export const attachmentPath = (chat: string, id: string) => `/v1/chats/${chat}/files/${id}`;

export const api = {
  health: () =>
    request<{
      ok: boolean;
      version: string;
      containers: boolean;
      containers_state?: "not_installed" | "not_running";
    }>("GET", "/v1/health"),
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
      lead?: boolean;
      approvals?: string;
      theme?: string;
      auto_off_minutes?: number;
      auto_off_default?: boolean;
      memory_mb?: number;
      swap_mb?: number;
      memory_default?: boolean;
    },
  ) => request<{ kitty: Kitty }>("PATCH", `/v1/kitties/${ref}`, u).then((r) => r.kitty),
  // lead is the fleet's lead kitty and its Reports thread; setLead makes another the lead
  // (an empty kitty leaves the fleet without one).
  lead: () =>
    request<{ lead: Kitty | null; reports?: string; handovers?: HandOverStat[] }>(
      "GET",
      "/v1/lead",
    ),
  setLead: (kitty: string) =>
    request<{ lead: Kitty | null; reports?: string }>("PUT", "/v1/lead", { kitty }),
  // power is the state of a kitty's machine; setPower turns it on (it comes up in the background)
  // or off (a 409 says what is using it; force turns it off anyway).
  power: (ref: string) =>
    request<{ power: Power }>("GET", `/v1/kitties/${ref}/power`).then((r) => r.power),
  // machine is the power and, when it is on, the memory of a kitty's machine.
  machine: (ref: string) =>
    request<{ power: Power; memory?: Memory }>("GET", `/v1/kitties/${ref}/power`),
  // machines are the RAM and swap of the machines without values of their own.
  machines: () => request<Machines>("GET", "/v1/settings/machines"),
  setMachines: (m: { memory_mb: number; swap_mb: number }) =>
    request<Machines>("PUT", "/v1/settings/machines", m),
  setPower: (ref: string, on: boolean, force = false) =>
    request<{ power: Power }>("POST", `/v1/kitties/${ref}/power`, { on, force }).then(
      (r) => r.power,
    ),
  startKitty: (ref: string) => request("POST", `/v1/kitties/${ref}/start`),
  // controlKitty takes a kitty's display over (hold) or gives it back; inputKitty is a click,
  // keys or text of the person on it.
  controlKitty: (ref: string, hold: boolean) =>
    request<{ held: boolean }>("POST", `/v1/kitties/${ref}/control`, { hold }).then((r) => r.held),
  inputKitty: (ref: string, action: string, args: Record<string, unknown>) =>
    request("POST", `/v1/kitties/${ref}/input`, { action, args }),
  // answerHelp is what the person answers a kitty that asked for help: told to it, or (secret)
  // typed on its desktop without showing it.
  answerHelp: (
    chat: string,
    turn: number,
    a: { text: string; secret: boolean; enter: boolean; save?: string },
  ) => request("POST", `/v1/chats/${chat}/turns/${turn}/help`, a),
  // updateStatus is the version of Kitties and the news of a newer one (refresh looks now), with
  // how far an update in the making has gone; applyUpdate starts installing the newer one (an
  // older gateway installs it before it answers, with updated_to), and the gateway restarts into it.
  updateStatus: (refresh = false) =>
    request<UpdateStatus>("GET", "/v1/update" + (refresh ? "?refresh=1" : "")),
  applyUpdate: () =>
    request<{ updating_to?: string; updated_to?: string }>("POST", "/v1/update/apply"),
  // exposure is where the gateway can be reached from (local, lan, wan); setExposure keeps the
  // choice and the gateway restarts to listen there (wan needs acceptRisk).
  exposure: () => request<Exposure>("GET", "/v1/exposure"),
  setExposure: (mode: ExposureMode, acceptRisk = false) =>
    request<{ restarting: boolean }>("PUT", "/v1/exposure", { mode, accept_risk: acceptRisk }),
  // rotateToken replaces the gateway's token and restarts it: everyone signed in with the old
  // one is signed out.
  rotateToken: () => request<{ restarting: boolean }>("POST", "/v1/token/rotate"),
  // mcp is the MCP connector for Claude; setMcp keeps the switches and the gateway restarts
  // (exec and approve need acceptRisk); mcpToken is the explicit way to read its token;
  // rotateMcpToken replaces it (and restarts); the clients are the OAuth sign-ins, revocable.
  mcp: () => request<McpInfo>("GET", "/v1/mcp"),
  setMcp: (s: McpSettings, acceptRisk = false) =>
    request<{ restarting: boolean }>("PUT", "/v1/mcp", { ...s, accept_risk: acceptRisk }),
  mcpToken: () => request<{ token: string }>("GET", "/v1/mcp/token"),
  rotateMcpToken: () => request<{ restarting: boolean }>("POST", "/v1/mcp/token"),
  mcpTest: () => request<{ ok: boolean; tools?: number; error?: string }>("POST", "/v1/mcp/test"),
  mcpClients: () => request<{ clients: McpClient[] }>("GET", "/v1/mcp/clients"),
  // whatsapp is the WhatsApp number linked as a device (docs/WHATSAPP.md): turning it on needs
  // acceptRisk (an unofficial API); link asks for new QR codes, unlink forgets the session.
  whatsapp: () => request<WhatsAppStatus>("GET", "/v1/whatsapp"),
  setWhatsApp: (s: WhatsAppSettings, acceptRisk = false) =>
    request<WhatsAppStatus>("PUT", "/v1/whatsapp", { ...s, accept_risk: acceptRisk }),
  linkWhatsApp: () => request<WhatsAppStatus>("POST", "/v1/whatsapp/link"),
  reconnectWhatsApp: () => request<WhatsAppStatus>("POST", "/v1/whatsapp/reconnect"),
  unlinkWhatsApp: () => request<WhatsAppStatus>("POST", "/v1/whatsapp/unlink"),
  testWhatsApp: (to: string) =>
    request<{ ok: boolean; error?: string }>("POST", "/v1/whatsapp/test", { to }),
  clearWhatsAppHeld: () => request<WhatsAppStatus>("DELETE", "/v1/whatsapp/held"),
  // guard is the guardrails of the kitties that answer other people (docs/GUARDRAILS.md).
  guard: () => request<GuardStatus>("GET", "/v1/guard"),
  setGuard: (s: GuardSettings) => request<GuardStatus>("PUT", "/v1/guard", s),
  clearGuardLog: () => request<GuardStatus>("DELETE", "/v1/guard/log"),
  exposeChat: (id: string, exposed: boolean, audience = "") =>
    request<Session>("PUT", `/v1/chats/${encodeURIComponent(id)}/exposed`, { exposed, audience }),
  resendWhatsAppHeld: (id: string) =>
    request<WhatsAppStatus>("POST", `/v1/whatsapp/held/${encodeURIComponent(id)}/resend`),
  revokeMcpClient: (id: string) =>
    request<{ revoked: string }>("DELETE", `/v1/mcp/clients/${encodeURIComponent(id)}`),
  // desktopInfo says whether a kitty's desktop has a newer image and when its sessions were backed
  // up; backupKitty backs them up now; updateKitty moves the desktop to the newest image.
  desktopInfo: (ref: string) => request<DesktopInfo>("GET", `/v1/kitties/${ref}/desktop`),
  backupKitty: (ref: string) => request("POST", `/v1/kitties/${ref}/backup`),
  updateKitty: (ref: string) => request("POST", `/v1/kitties/${ref}/update`),
  // shared lists what every kitty left in its Shared folder; keepShared copies one into the
  // library; deleteShared deletes it.
  shared: () =>
    request<{ files: SharedFile[]; quota_mb: number }>("GET", "/v1/shared").then((r) => ({
      files: r.files || [],
      quota_mb: r.quota_mb || 0,
    })),
  keepShared: (kitty: string, path: string) => request("POST", sharedPath(kitty, path, "keep")),
  deleteShared: (kitty: string, path: string) => request("DELETE", sharedPath(kitty, path)),
  // vault lists the secrets a kitty may type (names and kitties, never values); saveSecret saves
  // one (with a value) or changes who may use it (without).
  vault: () => request<{ secrets: Secret[] }>("GET", "/v1/vault").then((r) => r.secrets || []),
  saveSecret: (name: string, body: { value?: string; kitties: string[] }) =>
    request<{ secrets: Secret[] }>("PUT", `/v1/vault/${encodeURIComponent(name)}`, body).then(
      (r) => r.secrets || [],
    ),
  deleteSecret: (name: string) =>
    request<{ secrets: Secret[] }>("DELETE", `/v1/vault/${encodeURIComponent(name)}`).then(
      (r) => r.secrets || [],
    ),
  // clipboard is the text on a kitty's clipboard; setClipboard gives it the person's.
  clipboard: (ref: string) =>
    request<{ text: string }>("GET", `/v1/kitties/${ref}/clipboard`).then((r) => r.text || ""),
  setClipboard: (ref: string, text: string) =>
    request("PUT", `/v1/kitties/${ref}/clipboard`, { text }),
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
  // removeMember takes a member out of a chat, or a delegate off it (until it is handed work there
  // again).
  removeMember: (id: string, ref: string) =>
    request<{ chat: Session }>("DELETE", `/v1/chats/${id}/members/${encodeURIComponent(ref)}`).then(
      (r) => r.chat,
    ),
  // restoreDelegate brings back a delegate the person took off a chat.
  restoreDelegate: (id: string, ref: string) =>
    request<{ chat: Session }>("POST", `/v1/chats/${id}/delegates/${encodeURIComponent(ref)}`).then(
      (r) => r.chat,
    ),
  addMembers: (id: string, members: string[]) =>
    request<{ chat: Session }>("POST", `/v1/chats/${id}/members`, { members }).then((r) => r.chat),
  send: async (id: string, text: string, files: File[] = []) => {
    const encoded = await Promise.all(
      files.map(async (f) => ({ name: f.name, mime: f.type, data: await fileBase64(f) })),
    );
    return request<{ turns: Turn[] }>(
      "POST",
      `/v1/chats/${id}/messages`,
      encoded.length ? { text, files: encoded } : { text },
    ).then((r) => r.turns || []);
  },
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
  // providerModels checks an API key that is not saved yet and lists the models it can use.
  providerModels: (name: string, p: Record<string, unknown>) =>
    request<{ ok: boolean; models?: string[]; invalid_key?: boolean; error?: string }>(
      "POST",
      "/v1/settings/provider-models",
      { name, ...p },
    ),
  testConnector: (name: string) =>
    request<ConnectorTest>("POST", `/v1/settings/connectors/${name}/test`),
  startLogin: (kind: string, project?: string, via?: "gcloud") =>
    request<LoginFlow>("POST", `/v1/settings/accounts/${kind}/login`, {
      ...(project ? { project } : {}),
      ...(via ? { via } : {}),
    }),
  // installAccount installs the program an account signs in with (Claude Code) on the gateway.
  installAccount: (kind: string) =>
    request<LoginFlow>("POST", `/v1/settings/accounts/${kind}/install`),
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
  // setRule limits an action of a kitty (all of the connection's with an empty action).
  setRule: (
    kitty: string,
    connector: string,
    action: string,
    mode: "ask" | "never" | "auto" | "",
  ) =>
    request<{ rule: ApprovalRule }>("POST", "/v1/approvals/rules", {
      kitty,
      connector,
      action,
      mode,
    }),
  memory: (kitty: string) => request<MemoryView>("GET", `/v1/kitties/${kitty}/memory`),
  remember: (kitty: string, text: string, scope = "user") =>
    request<{ note: MemoryNote }>("POST", `/v1/kitties/${kitty}/memory`, { text, scope }).then(
      (r) => r.note,
    ),
  forgetNote: (kitty: string, id: string) => request("DELETE", `/v1/kitties/${kitty}/memory/${id}`),
  editNote: (kitty: string, id: string, text: string) =>
    request<{ note: MemoryNote }>("PUT", `/v1/kitties/${kitty}/memory/${id}`, { text }).then(
      (r) => r.note,
    ),
  sharedNotes: () =>
    request<{ notes: MemoryNote[] }>("GET", "/v1/memory/shared").then((r) => r.notes),
  rememberShared: (text: string) =>
    request<{ note: MemoryNote }>("POST", "/v1/memory/shared", { text }).then((r) => r.note),
  editShared: (id: string, text: string) =>
    request<{ note: MemoryNote }>("PUT", `/v1/memory/shared/${id}`, { text }).then((r) => r.note),
  forgetShared: (id: string) => request("DELETE", `/v1/memory/shared/${id}`),
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
  setNotify: (c: {
    connector: string;
    on: string[];
    digest?: string;
    day?: boolean;
    replies?: boolean;
  }) => request<SettingsView>("PUT", "/v1/settings/notify", c),
  setAutonomy: (a: {
    follow_ups: string;
    brain_wait: string;
    stand_ins: string;
    lead_limits?: string;
  }) => request<SettingsView>("PUT", "/v1/settings/autonomy", a),
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

// oauthRedirect asks the gateway for the exact redirect address a sign-in sends (what to register
// in the service's dashboard); provider answers for a connection not saved yet.
export const oauthRedirect = (name: string, provider?: string) =>
  request<{ redirect: string }>(
    "GET",
    `/v1/oauth/${encodeURIComponent(name)}/redirect${provider ? `?provider=${encodeURIComponent(provider)}` : ""}`,
  ).then((r) => r.redirect);

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

// watchDesktop follows a kitty's display live inside Wails, where the page cannot load the
// gateway's stream itself: each picture comes as a data: URL. It returns the function that stops
// watching, or null outside Wails (there an <img> loads streamURL).
export function watchDesktop(
  ref: string,
  onFrame: (src: string) => void,
  onError: (why: string) => void,
): (() => void) | null {
  const app = window.go?.main?.App as unknown as
    | {
        WatchDesktop?: (ref: string) => Promise<void>;
        UnwatchDesktop?: (ref: string) => Promise<void>;
      }
    | undefined;
  const rt = (
    window as unknown as {
      runtime?: { EventsOn?: (name: string, cb: (data: unknown) => void) => () => void };
    }
  ).runtime;
  if (!app?.WatchDesktop || !rt?.EventsOn) {
    return null;
  }
  const off = rt.EventsOn("kitties:desktop:" + ref, (data) => {
    const d = data as { frame?: string; error?: string };
    if (d.frame) {
      onFrame(d.frame);
    } else if (d.error) {
      onError(d.error);
    }
  });
  void app.WatchDesktop(ref).catch((e: unknown) => onError(String(e)));
  return () => {
    off();
    void app.UnwatchDesktop?.(ref);
  };
}

// streamDesktop follows a kitty's display live and hands each picture to onFrame as an image
// address (a blob: URL in a browser, a data: URL inside Wails), until the returned function is
// called. The gateway's motion JPEG is read with fetch and cut into pictures here: an <img> of
// it shows nothing in some browsers (headless Chromium, Safari's webviews). onError gets why a
// stream ended or could not start; the caller falls back to screenshots.
export function streamDesktop(
  ref: string,
  onFrame: (src: string) => void,
  onError: (why: string) => void,
): () => void {
  const wails = watchDesktop(ref, onFrame, onError);
  if (wails) {
    return wails;
  }
  const stop = new AbortController();
  let last = "";
  void (async () => {
    try {
      let conn = savedConnection();
      if (!conn.token) {
        conn = await fetchLocalToken(conn);
      }
      const res = await fetch(safeGatewayBase(conn.url) + `/v1/kitties/${ref}/stream`, {
        headers: conn.token ? { Authorization: "Bearer " + conn.token } : {},
        signal: stop.signal,
      });
      if (!res.ok || !res.body) {
        throw new Error(`HTTP ${res.status}`);
      }
      const reader = res.body.getReader();
      let buf = new Uint8Array(0);
      const text = new TextDecoder();
      for (;;) {
        const { value, done } = await reader.read();
        if (done) {
          throw new Error("the stream ended");
        }
        const joined = new Uint8Array(buf.length + value.length);
        joined.set(buf);
        joined.set(value, buf.length);
        buf = joined;
        // Parts: "--boundary\r\n" or headers, a blank line, Content-Length bytes, "\r\n".
        for (;;) {
          const end = indexOf(buf, [13, 10, 13, 10]);
          if (end < 0) {
            break;
          }
          const head = text.decode(buf.subarray(0, end));
          const len = Number(/content-length:\s*(\d+)/i.exec(head)?.[1] ?? NaN);
          const type = /content-type:\s*([^\r\n]+)/i.exec(head)?.[1] ?? "image/jpeg";
          if (!Number.isFinite(len)) {
            throw new Error("not a picture stream");
          }
          if (buf.length < end + 4 + len) {
            break;
          }
          const picture = buf.slice(end + 4, end + 4 + len);
          buf = buf.slice(end + 4 + len);
          const src = URL.createObjectURL(new Blob([picture], { type }));
          onFrame(src);
          if (last) {
            URL.revokeObjectURL(last);
          }
          last = src;
        }
      }
    } catch (e) {
      if (!stop.signal.aborted) {
        onError(e instanceof Error ? e.message : String(e));
      }
    }
  })();
  return () => {
    stop.abort();
    if (last) {
      setTimeout(() => URL.revokeObjectURL(last), 1000);
    }
  };
}

// watchRects is rectsDesktop inside Wails: the app's WatchRects emits "kitties:rects:<ref>" events
// with the display's size and the rectangles as data: URLs, drawn here in the order they came.
function watchRects(
  ref: string,
  on: { size: (w: number, h: number) => void; rects: (list: DesktopRect[]) => void },
  onError: (why: string) => void,
): (() => void) | null {
  const app = window.go?.main?.App as unknown as
    | {
        WatchRects?: (ref: string) => Promise<void>;
        UnwatchRects?: (ref: string) => Promise<void>;
      }
    | undefined;
  const rt = (
    window as unknown as {
      runtime?: { EventsOn?: (name: string, cb: (data: unknown) => void) => () => void };
    }
  ).runtime;
  if (!app?.WatchRects || !rt?.EventsOn) {
    return null;
  }
  let ordered: Promise<unknown> = Promise.resolve();
  const off = rt.EventsOn("kitties:rects:" + ref, (data) => {
    const d = data as {
      error?: string;
      size?: [number, number];
      rects?: { x: number; y: number; w: number; h: number; data: string }[];
    };
    if (d.error) {
      onError(d.error);
    } else if (d.size) {
      const size = d.size;
      ordered = ordered.then(() => on.size(size[0], size[1]));
    } else if (d.rects) {
      const rects = d.rects;
      ordered = ordered
        .then(async () => {
          const list = await Promise.all(
            rects.map(async (r) => {
              if (!r.data.startsWith("data:image/")) {
                throw new Error("unexpected picture source");
              }
              return { ...r, blob: await (await fetch(r.data)).blob() };
            }),
          );
          on.rects(list);
        })
        .catch((e) => onError(String(e)));
    }
  });
  void app.WatchRects(ref).catch((e: unknown) => onError(String(e)));
  return () => {
    off();
    void app.UnwatchRects?.(ref);
  };
}

// A DesktopRect is a part of the display that changed, and its picture.
// DesktopInfo is what is known about a kitty's desktop: its image, whether a newer one waits, and
// when its sessions were last backed up.
export type DesktopInfo = {
  image: string;
  update_available: boolean;
  auto_update?: boolean;
  backup?: { saved: string; bytes: number };
  // storage is what the kitty keeps on this computer, which no update deletes.
  storage?: Storage;
};

// Storage is what a kitty keeps on the gateway's computer, by folder: its home (settings and the
// programs it installed for itself, when its desktop keeps it there), its workspace and its
// Shared folder.
export type Storage = {
  home: boolean;
  home_bytes: number;
  work_bytes: number;
  shared_bytes: number;
  // shared_quota_mb is the most Shared holds; 0 is no limit.
  shared_quota_mb: number;
  // moving is true while an older desktop's home waits to go into the new one.
  moving?: boolean;
};

// SharedFile is a file a kitty left in its Shared folder for its person.
export interface SharedFile {
  kitty: string;
  path: string;
  type: string;
  size: number;
  updated_at: string;
}

// sharedPath is where the gateway serves a kitty's shared file.
export const sharedPath = (kitty: string, path: string, verb = "shared") =>
  `/v1/kitties/${encodeURIComponent(kitty)}/${verb}/${path.split("/").map(encodeURIComponent).join("/")}`;

// importKitty puts a kitty's files back from an export (.tar.gz), straight to the gateway: the
// file can be big, and binary.
export async function importKitty(ref: string, file: Blob): Promise<void> {
  // A kitty's id is letters, digits, dashes and underscores: nothing else goes into an address.
  if (!/^[\w-]+$/.test(ref)) {
    throw new ApiError(400, "not a kitty");
  }
  await request("POST", `/v1/kitties/${ref}/import`, file);
}

export type ExposureMode = "local" | "lan" | "wan";

// Exposure is where the gateway listens: local (127.0.0.1), lan (this machine's network address)
// or wan (0.0.0.0).
export type Exposure = {
  mode: ExposureMode;
  listen: string;
  lan_ip?: string;
  can_change: boolean;
  why?: string;
  // auto: the gateway opened itself to the local network on its first start, nobody chose it.
  auto?: boolean;
  token_rotate?: boolean;
  token_why?: string;
};

// McpInfo is the MCP connector (docs/MCP.md) as Settings shows it. It never carries the token:
// the command reads it from token_file, and mcpToken is the explicit way to get it.
export type McpInfo = {
  enabled: boolean;
  allow_exec: boolean;
  allow_approve: boolean;
  oauth: boolean;
  url: string;
  token_file: string;
  command: string;
  // connect are the addresses /mcp answers at (they follow the gateway's exposure), best first.
  connect?: McpConnect[];
  in_container?: boolean;
  token_managed: boolean;
  can_change: boolean;
  why?: string;
};

// McpConnect is one address Claude Code reaches the connector at. With with_token the command
// carries MCP_TOKEN_PLACEHOLDER, replaced with the token only when the person copies it.
export type McpConnect = {
  where: "public" | "lan" | "local";
  url: string;
  command: string;
  with_token?: boolean;
};

export const MCP_TOKEN_PLACEHOLDER = "<MCP_TOKEN>";

export type McpSettings = Pick<McpInfo, "enabled" | "allow_exec" | "allow_approve" | "oauth">;

// WhatsAppSettings are what the person chooses: on or off, who may write (numbers with their
// country code, digits only once kept), the kitty that answers, and whether it answers in groups:
// only in the groups of allow_groups (their ids, "…@g.us"), when someone there mentions it.
export type WhatsAppSettings = {
  enabled: boolean;
  allow: string[];
  kitty: string;
  groups: boolean;
  allow_groups: string[];
  // kitties gives a number (digits) or a group (its id) a kitty of its own.
  kitties?: Record<string, string>;
  // max_per_hour is the most messages sent in an hour (0: 60).
  max_per_hour?: number;
  // quiet_from and quiet_to ("22:00", "08:00") are the quiet hours; empty: none.
  quiet_from?: string;
  quiet_to?: string;
  // voice is when the kitties answer with a voice note: "" never, "mirror" a voice note gets
  // one, "always" every answer.
  voice?: "" | "mirror" | "always";
};

// WhatsAppHeld is a message the hourly limit did not send: to is a number (digits) or a group
// (its id, with its name).
export type WhatsAppHeld = {
  id: string;
  at: string;
  to: string;
  name?: string;
  kind: "text" | "voice" | "file" | "reaction";
  text?: string;
};

// WhatsAppGroup is a group that wrote lately, to put on the list.
export type WhatsAppGroup = { id: string; name?: string };

// WhatsAppStatus is the link: qr_png is the code to scan while state is "qr".
export type WhatsAppStatus = WhatsAppSettings & {
  kitty_name?: string;
  state:
    | "off"
    | "starting"
    | "qr"
    | "qr_expired"
    | "connected"
    | "disconnected"
    | "logged_out"
    | "error";
  qr?: string;
  qr_png?: string;
  phone?: string;
  linked: boolean;
  error?: string;
  ignored: number;
  last_message?: string;
  seen_groups?: WhatsAppGroup[];
  sent_last_hour?: number;
  held?: number;
  held_log?: WhatsAppHeld[];
  // can_speak is true when there is a speech service for voice notes; risk_accepted once the
  // person accepted, the first time, that WhatsApp may ban the number.
  can_speak?: boolean;
  risk_accepted?: boolean;
  quiet?: boolean;
  waiting?: number;
  can_change: boolean;
  why?: string;
};

// McpClient is a remote client (claude.ai) that signed in to the connector with OAuth.
export type McpClient = {
  id: string;
  name: string;
  host: string;
  since: string;
  last_token?: string;
  revoked?: boolean;
};

// UpdateStatus says which version of Kitties runs, whether a newer one is out, and whether this
// gateway may install it by itself (and why not when it may not).
export type UpdateStatus = {
  current: string;
  latest?: string;
  available: boolean;
  mode: "off" | "notify" | "auto";
  can_apply: boolean;
  // signed is true when releases are checked against a signing key (updates.public_key).
  signed?: boolean;
  // skipped is a newer version that was tried here and did not hold: it is not offered again.
  skipped?: string;
  why?: string;
  checked_at?: string;
  error?: string;
  // url is the newest release's page; changes are the lines of its "What's Changed".
  url?: string;
  changes?: string[];
  // progress is the update being made (or the last one, when it failed).
  progress?: UpdateProgress;
};

// UpdateProgress is how far an update has gone: its step, the bytes of the download, the whole.
export type UpdateProgress = {
  to: string;
  phase: "check" | "download" | "verify" | "install" | "restart" | "failed";
  percent: number;
  done?: number;
  total?: number;
  error?: string;
};

// A Secret is one of the vault: its name and the ids of the kitties that may type it.
export type Secret = { name: string; kitties: string[]; saved: string };

export type DesktopRect = { x: number; y: number; w: number; h: number; blob: Blob };

// rectsDesktop follows a kitty's display as what changed on it, like VNC: the whole picture
// first, then only the rectangles that differ from the one before (the gateway's /rects). size
// says the display's size, and rects the pictures to draw over what is there. It returns the
// function that stops it, or null where it cannot be had. Inside Wails the page cannot reach the
// gateway itself: the app reads the stream and hands each update over as runtime events. onError
// says why it ended (a gateway without /rects answers 404); the caller falls back to streamDesktop.
export function rectsDesktop(
  ref: string,
  on: { size: (w: number, h: number) => void; rects: (list: DesktopRect[]) => void },
  onError: (why: string) => void,
): (() => void) | null {
  if (window.go?.main?.App) {
    return watchRects(ref, on, onError);
  }
  const stop = new AbortController();
  void (async () => {
    try {
      let conn = savedConnection();
      if (!conn.token) {
        conn = await fetchLocalToken(conn);
      }
      const res = await fetch(safeGatewayBase(conn.url) + `/v1/kitties/${ref}/rects?fps=20`, {
        headers: conn.token ? { Authorization: "Bearer " + conn.token } : {},
        signal: stop.signal,
      });
      if (!res.ok || !res.body) {
        throw new Error(`HTTP ${res.status}`);
      }
      const reader = res.body.getReader();
      const text = new TextDecoder();
      let buf = new Uint8Array(0);
      // want is how many rectangles of the update are still to come; head is the one whose
      // picture is being waited for; batch is what the update has so far.
      let want = 0;
      let head: number[] | null = null;
      let batch: DesktopRect[] = [];
      for (;;) {
        const { value, done } = await reader.read();
        if (done) {
          throw new Error("the stream ended");
        }
        const joined = new Uint8Array(buf.length + value.length);
        joined.set(buf);
        joined.set(value, buf.length);
        buf = joined;
        for (;;) {
          if (head) {
            if (buf.length < head[4]) {
              break;
            }
            batch.push({
              x: head[0],
              y: head[1],
              w: head[2],
              h: head[3],
              blob: new Blob([buf.slice(0, head[4])]),
            });
            buf = buf.slice(head[4]);
            head = null;
            if (--want === 0) {
              on.rects(batch);
              batch = [];
            }
            continue;
          }
          const nl = buf.indexOf(10);
          if (nl < 0) {
            break;
          }
          const line = text.decode(buf.subarray(0, nl)).trim().split(" ");
          buf = buf.slice(nl + 1);
          if (line[0] === "SCREEN" && line.length === 3) {
            on.size(Number(line[1]), Number(line[2]));
          } else if (line[0] === "UPDATE" && line.length === 2) {
            want = Number(line[1]);
            batch = [];
          } else if (line[0] === "RECT" && line.length === 6 && want > 0) {
            head = line.slice(1).map(Number);
          } else {
            throw new Error("not a stream of rectangles");
          }
        }
      }
    } catch (e) {
      if (!stop.signal.aborted) {
        onError(e instanceof Error ? e.message : String(e));
      }
    }
  })();
  return () => stop.abort();
}

// indexOf finds a sequence of bytes, or -1.
function indexOf(buf: Uint8Array, seq: number[]): number {
  outer: for (let i = 0; i + seq.length <= buf.length; i++) {
    for (let j = 0; j < seq.length; j++) {
      if (buf[i + j] !== seq[j]) {
        continue outer;
      }
    }
    return i;
  }
  return -1;
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
  // The stream carries the token in its address: on a first visit the token is fetched first,
  // or the stream is refused and nothing arrives live until a reload.
  let es: EventSource | null = null;
  let closed = false;
  const conn = savedConnection();
  void (conn.token ? Promise.resolve(conn) : fetchLocalToken(conn)).then(() => {
    if (closed) {
      return;
    }
    es = new EventSource(shotURL(`/v1/chats/${id}/events`));
    for (const type of ["turn", "update", "done", "deleted", "helping"]) {
      es.addEventListener(type, (ev) => {
        try {
          onEvent(JSON.parse((ev as MessageEvent).data) as ChatEvent);
        } catch {
          /* a malformed event is dropped */
        }
      });
    }
  });
  return () => {
    closed = true;
    es?.close();
  };
}

// artifactText loads a text artifact for reading in the page.
export function artifactText(name: string): Promise<string> {
  return fileText(`/v1/artifacts/${encodeURIComponent(name)}`);
}

// fileText loads a text file the gateway serves (an artifact, a shared file) for reading in the page.
export async function fileText(path: string): Promise<string> {
  const app = window.go?.main?.App;
  if (app) {
    const raw = await app.Request("GET", path, "");
    const parsed = JSON.parse(raw) as { status: number; body: string; base64?: string };
    if (parsed.status >= 400) {
      throw new ApiError(parsed.status, "no artifact");
    }
    return parsed.base64 ? "(binary file)" : parsed.body;
  }
  // A link opened on a first visit (#artifact/<name>) can come before the token: it is fetched
  // first, or the gateway refuses the file.
  const conn = savedConnection();
  if (!conn.token) {
    await fetchLocalToken(conn);
  }
  const res = await fetch(shotURL(path));
  if (!res.ok) {
    throw new ApiError(res.status, "no artifact");
  }
  return res.text();
}

// voxlSource is the address of a kitty's H.264 live view (voxl, which the gateway proxies at
// /v1/kitties/{ref}/voxl/) for the page to frame, or null where it cannot be had: Android with a
// gateway that is not HTTPS (the app's page is HTTPS and the browser would not show a frame of
// plain HTTP), a gateway on another origin (it only lets its own page and the apps' frame it), a
// page where the browser has no WebCodecs (it needs HTTPS or localhost), and a desktop whose image
// has no voxl. The caller then follows the display as rectangles, without sound. The desktop app
// (Wails) frames the gateway it started on this machine; its ticket comes through the app's bridge.
export async function voxlSource(ref: string): Promise<string | null> {
  // A kitty's id is letters, digits, dashes and underscores: nothing else goes into an address.
  if (!/^[\w-]+$/.test(ref) || !window.isSecureContext || typeof VideoDecoder === "undefined") {
    return null;
  }
  if (inWails()) {
    try {
      const { path } = await request<{ path?: string }>("POST", `/v1/kitties/${ref}/voxl/ticket`);
      return path && /^\/v1\/kitties\/[\w-]+\/voxl\/t\/[0-9a-f]+\/$/.test(path)
        ? (safeGatewayBase(savedConnection().url) || "http://127.0.0.1:7488") + path
        : null;
    } catch {
      return null;
    }
  }
  let conn = savedConnection();
  if (!conn.token) {
    conn = await fetchLocalToken(conn);
  }
  const base = safeGatewayBase(conn.url);
  if (
    base &&
    new URL(base).origin !== location.origin &&
    !(inAndroid() && base.startsWith("https:"))
  ) {
    return null;
  }
  // A ticket opens the live view for a while: the frame's own requests cannot carry the token.
  try {
    const res = await fetch(`${base}/v1/kitties/${ref}/voxl/ticket`, {
      method: "POST",
      headers: conn.token ? { Authorization: "Bearer " + conn.token } : {},
    });
    if (!res.ok) {
      return null;
    }
    const { path } = (await res.json()) as { path?: string };
    return path && /^\/v1\/kitties\/[\w-]+\/voxl\/t\/[0-9a-f]+\/$/.test(path) ? base + path : null;
  } catch {
    return null;
  }
}

// GuardSettings are the guardrails of the chats with people who are not the person: on by default,
// no connection allowed, no number trusted.
export type GuardSettings = {
  enabled: boolean;
  // connectors an exposed kitty may still use.
  connectors: string[];
  // trusted are the person's own WhatsApp numbers (digits): their chats are not exposed.
  trusted: string[];
  max_per_hour: number;
  block_injection: boolean;
  redact_personal: boolean;
};

// GuardEvent is one thing the guardrails did: kind is redacted, blocked_tool, rate_limited or
// injection; it never holds a secret.
export type GuardEvent = {
  id: string;
  at: string;
  kind: "redacted" | "blocked_tool" | "rate_limited" | "injection";
  chat?: string;
  who?: string;
  detail: string;
  text?: string;
};

export type GuardStatus = GuardSettings & {
  log: GuardEvent[];
  counts: Record<string, number>;
};
