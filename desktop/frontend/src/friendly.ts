// Words for people: what the gateway calls openai, messaging or create_issue, as the screens
// say it. Everything here is display only; the API keeps its own names.

export interface ProviderMeta {
  label: string;
  blurb: string;
  // keyURL is where a person gets a key for it.
  keyURL?: string;
  // address is true when the person must say where the server is (a local or private one).
  address?: boolean;
  models?: string[];
}

export const PROVIDERS: Record<string, ProviderMeta> = {
  anthropic: {
    label: "Anthropic Claude",
    blurb: "Claude models: careful, strong at long tasks and tools.",
    keyURL: "https://console.anthropic.com/settings/keys",
    models: ["claude-sonnet-5-5", "claude-opus-5-5", "claude-haiku-5-5"],
  },
  openai: {
    label: "OpenAI",
    blurb: "GPT models, the most widely used.",
    keyURL: "https://platform.openai.com/api-keys",
  },
  gemini: {
    label: "Google Gemini",
    blurb: "Gemini models, with a free tier to start.",
    keyURL: "https://aistudio.google.com/app/apikey",
  },
  ollama: {
    label: "Ollama on this computer",
    blurb: "Free models that run on your own machine. No key needed.",
    keyURL: "https://ollama.com/download",
    address: true,
  },
  "ollama-cloud": {
    label: "Ollama Cloud",
    blurb: "Open models, hosted by Ollama.",
    keyURL: "https://ollama.com/settings/keys",
  },
  openrouter: {
    label: "OpenRouter",
    blurb: "One key for hundreds of models.",
    keyURL: "https://openrouter.ai/keys",
  },
  groq: { label: "Groq", blurb: "Very fast open models.", keyURL: "https://console.groq.com/keys" },
  deepseek: {
    label: "DeepSeek",
    blurb: "DeepSeek models, low cost.",
    keyURL: "https://platform.deepseek.com/api_keys",
  },
  mistral: {
    label: "Mistral",
    blurb: "Mistral models, from Europe.",
    keyURL: "https://console.mistral.ai/api-keys",
  },
  qwen: { label: "Qwen (Alibaba)", blurb: "Qwen models through DashScope." },
  codex: {
    label: "OpenAI Codex",
    blurb: "OpenAI models tuned for code.",
    keyURL: "https://platform.openai.com/api-keys",
  },
  copilot: { label: "GitHub Copilot", blurb: "The models of your Copilot plan." },
  custom: {
    label: "Another server",
    blurb: "Anything that speaks the OpenAI API: LM Studio, vLLM, a company gateway.",
    address: true,
  },
};

// The order the providers are offered in: the common ones first.
export const PROVIDER_ORDER = [
  "anthropic",
  "openai",
  "gemini",
  "ollama",
  "openrouter",
  "groq",
  "deepseek",
  "mistral",
  "ollama-cloud",
  "qwen",
  "codex",
  "copilot",
  "custom",
];

export const providerLabel = (kind: string): string => PROVIDERS[kind]?.label || kind;

// CATEGORY says what a connector category holds.
export const CATEGORY: Record<string, string> = {
  messaging: "Chat & messages",
  "social media": "Social media",
  git: "Code",
  productivity: "Productivity",
  office: "Office & files",
  finance: "Finance",
  drawing: "Drawing",
  generic: "Other",
};

export const categoryLabel = (c: string): string => CATEGORY[c] || c;

// actionLabel turns create_issue into "Create issue".
export function actionLabel(name: string): string {
  const words = name.replace(/_/g, " ");
  return words.charAt(0).toUpperCase() + words.slice(1);
}

// uniqueName is base, or base-2, base-3... whichever is free.
export function uniqueName(base: string, taken: string[]): string {
  const clean =
    base
      .toLowerCase()
      .replace(/[^a-z0-9-]+/g, "-")
      .replace(/^-+|-+$/g, "") || "item";
  if (!taken.includes(clean)) {
    return clean;
  }
  for (let i = 2; ; i++) {
    if (!taken.includes(`${clean}-${i}`)) {
      return `${clean}-${i}`;
    }
  }
}

// people lists names as "mimi", "mimi and tom" or "mimi, tom and luna".
export function people(names: string[]): string {
  if (names.length <= 1) {
    return names.join("");
  }
  return names.slice(0, -1).join(", ") + " and " + names[names.length - 1];
}

// friendlyError rewrites the gateway's technical reasons a person meets most often.
export function friendlyError(msg: string): string {
  if (/API key is missing|api key is missing/i.test(msg)) {
    return "It needs an API key: paste one in the form.";
  }
  if (/401|unauthori[sz]ed|invalid.*key|incorrect api key/i.test(msg)) {
    return "The key was refused: check that it is complete and still valid.";
  }
  if (/connection refused|no such host|does not answer|dial tcp/i.test(msg)) {
    return "The server did not answer: check the address, and that it is running.";
  }
  if (/model.*not.*found|does not exist|404/i.test(msg)) {
    return "That model name was not found at this provider: check its spelling.";
  }
  return msg;
}

// A kitty's connections: an empty list is all of them (new ones too), NONE alone is none, and
// anything else is just those.
export const NONE = "-";

export function mayUse(list: string[] | undefined, name: string): boolean {
  const l = list || [];
  return l.length === 0 || l.includes(name);
}

export function usesAll(list: string[] | undefined): boolean {
  return (list || []).length === 0;
}

// withConnection is a kitty's list with one connection allowed or not; all is the list of every
// connection there is, for a kitty that had all of them until now.
export function withConnection(
  list: string[] | undefined,
  name: string,
  on: boolean,
  all: string[],
): string[] {
  let l = list || [];
  if (l.length === 0) {
    l = [...all];
  } else if (l.length === 1 && l[0] === NONE) {
    l = [];
  }
  l = on ? [...new Set([...l, name])] : l.filter((c) => c !== name);
  return l.length === 0 ? [NONE] : l;
}
