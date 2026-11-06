// Sorting and filtering the artifacts of the library: no imports, so the tests run it as it is.

export interface ArtifactRow {
  name: string;
  type: string;
  size: number;
  updated_at: string;
  by?: string;
}

export type Kind = "doc" | "data" | "image" | "code" | "other";
export type Sort = "new" | "name" | "size";

const KINDS: [Kind, RegExp][] = [
  ["doc", /\.(md|markdown|txt|pdf|docx?|odt|rtf|html?)$/i],
  ["data", /\.(csv|tsv|json|ya?ml|xlsx?|ods|xml|toml)$/i],
  ["image", /\.(svg|png|jpe?g|gif|webp|bmp)$/i],
  ["code", /\.(go|py|js|ts|tsx|jsx|sh|rb|rs|java|c|cpp|h|sql|css|lua)$/i],
];

// kindOf is what a file is for the filter, from its name and else from its type.
export function kindOf(a: Pick<ArtifactRow, "name" | "type">): Kind {
  for (const [k, re] of KINDS) {
    if (re.test(a.name)) {
      return k;
    }
  }
  if (a.type.startsWith("image/")) {
    return "image";
  }
  if (a.type.startsWith("text/")) {
    return "doc";
  }
  return "other";
}

export interface Filter {
  q?: string;
  kind?: Kind | "";
  by?: string;
  sort?: Sort;
}

// filterArtifacts keeps the files that match a search in the name, a kind and the kitty that
// saved them, in the order asked: the newest first (the default), by name, or the biggest first.
export function filterArtifacts<T extends ArtifactRow>(list: T[], f: Filter): T[] {
  const q = (f.q || "").trim().toLowerCase();
  const out = list.filter(
    (a) =>
      (!q || a.name.toLowerCase().includes(q)) &&
      (!f.kind || kindOf(a) === f.kind) &&
      (!f.by || (a.by || "") === f.by),
  );
  const time = (a: T) => Date.parse(a.updated_at) || 0;
  switch (f.sort || "new") {
    case "name":
      return out.sort((a, b) => a.name.localeCompare(b.name));
    case "size":
      return out.sort((a, b) => b.size - a.size || a.name.localeCompare(b.name));
    default:
      return out.sort((a, b) => time(b) - time(a) || a.name.localeCompare(b.name));
  }
}

// facets are the kinds and the kitties the list has, for the filters to offer only those.
export function facets(list: ArtifactRow[]): { kinds: Kind[]; by: string[] } {
  const kinds = new Set<Kind>();
  const by = new Set<string>();
  for (const a of list) {
    kinds.add(kindOf(a));
    if (a.by) {
      by.add(a.by);
    }
  }
  const order: Kind[] = ["doc", "data", "image", "code", "other"];
  return { kinds: order.filter((k) => kinds.has(k)), by: [...by].sort() };
}

// previewOf is how the reader shows a file of a Shared folder: as a picture, as text, or not at
// all (a binary document it cannot draw).
export function previewOf(name: string): "image" | "text" | "none" {
  const lower = name.toLowerCase();
  if (/\.(png|jpe?g|gif|webp|bmp)$/.test(lower)) {
    return "image";
  }
  if (
    /\.(md|markdown|txt|csv|tsv|json|ya?ml|xml|toml|svg|log|ini|conf|html?|css|go|py|js|ts|tsx|jsx|sh|rb|rs|java|c|cpp|h|sql|lua)$/.test(
      lower,
    ) ||
    !/\.[a-z0-9]+$/.test(lower)
  ) {
    return "text";
  }
  return "none";
}

// bytes says a size the way a person reads it: 0 B, 812 B, 4.2 KB, 13 MB, 1.5 GB.
export function bytes(n: number): string {
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  let v = Math.max(0, n);
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  const shown = i === 0 || v >= 10 ? Math.round(v).toString() : v.toFixed(1).replace(/\.0$/, "");
  return `${shown} ${units[i]}`;
}
