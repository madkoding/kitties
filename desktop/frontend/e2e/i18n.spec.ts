import { expect, test } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

// Every text the page wraps in t("…") has its Spanish: a missing one would show in English in
// the middle of a Spanish page.
test("every text of the page has its Spanish", async () => {
  const src = path.join(path.dirname(fileURLToPath(import.meta.url)), "..", "src");
  const dir = path.join(src, "i18n", "es");
  const es: Record<string, string> = {};
  for (const file of fs.readdirSync(dir)) {
    if (file.endsWith(".ts") && file !== "index.ts") {
      Object.assign(es, ((await import(path.join(dir, file))) as { default: object }).default);
    }
  }
  const missing: string[] = [];
  const literal = /\bt\(\s*("(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*')/g;
  for (const file of fs.readdirSync(src)) {
    if (!/\.(tsx?|ts)$/.test(file) || file === "i18n.ts") {
      continue;
    }
    const text = fs.readFileSync(path.join(src, file), "utf8");
    for (const m of text.matchAll(literal)) {
      const key = m[1].startsWith('"')
        ? (JSON.parse(m[1]) as string)
        : m[1].slice(1, -1).replace(/\\'/g, "'");
      if (!(key in es)) {
        missing.push(`${file}: ${key}`);
      }
    }
  }
  expect(missing).toEqual([]);
});
