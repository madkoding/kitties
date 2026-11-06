import { test } from "node:test";
import assert from "node:assert/strict";
import { facets, filterArtifacts, kindOf } from "../src/library.ts";

const list = [
  {
    name: "plan.md",
    type: "text/markdown",
    size: 30,
    updated_at: "2026-11-01T10:00:00Z",
    by: "mimi",
  },
  {
    name: "prices.csv",
    type: "text/csv",
    size: 900,
    updated_at: "2026-11-03T10:00:00Z",
    by: "luna",
  },
  {
    name: "flow.svg",
    type: "image/svg+xml",
    size: 500,
    updated_at: "2026-11-02T10:00:00Z",
    by: "mimi",
  },
  { name: "Alpha.md", type: "text/markdown", size: 30, updated_at: "bad date" },
];

test("kindOf: by the name, else by the type", () => {
  assert.equal(kindOf({ name: "a.md", type: "" }), "doc");
  assert.equal(kindOf({ name: "a.json", type: "" }), "data");
  assert.equal(kindOf({ name: "a.PNG", type: "" }), "image");
  assert.equal(kindOf({ name: "a.go", type: "" }), "code");
  assert.equal(kindOf({ name: "shot", type: "image/png" }), "image");
  assert.equal(kindOf({ name: "notes", type: "text/plain" }), "doc");
  assert.equal(kindOf({ name: "blob.bin", type: "application/octet-stream" }), "other");
});

test("filterArtifacts: the newest first, by name, the biggest first", () => {
  const names = (f: Parameters<typeof filterArtifacts>[1]) =>
    filterArtifacts(list, f).map((a) => a.name);
  assert.deepEqual(names({}), ["prices.csv", "flow.svg", "plan.md", "Alpha.md"]);
  assert.deepEqual(names({ sort: "name" }), ["Alpha.md", "flow.svg", "plan.md", "prices.csv"]);
  assert.deepEqual(names({ sort: "size" }), ["prices.csv", "flow.svg", "Alpha.md", "plan.md"]);
  assert.deepEqual(names({ kind: "doc", sort: "name" }), ["Alpha.md", "plan.md"]);
  assert.deepEqual(names({ by: "mimi" }), ["flow.svg", "plan.md"]);
  assert.deepEqual(names({ q: " PL " }), ["plan.md"]);
  assert.deepEqual(names({ q: "zzz" }), []);
  // The list it was given stays as it was.
  assert.equal(list[0].name, "plan.md");
});

test("facets: only the kinds and kitties the library has", () => {
  assert.deepEqual(facets(list), { kinds: ["doc", "data", "image"], by: ["luna", "mimi"] });
  assert.deepEqual(facets([]), { kinds: [], by: [] });
});

test("previewOf: pictures, text, and what has no preview", async () => {
  const { previewOf } = await import("../src/library.ts");
  assert.equal(previewOf("cat.PNG"), "image");
  assert.equal(previewOf("photo.jpeg"), "image");
  assert.equal(previewOf("notes.md"), "text");
  assert.equal(previewOf("data.csv"), "text");
  assert.equal(previewOf("README"), "text");
  assert.equal(previewOf("report.pdf"), "none");
  assert.equal(previewOf("deck.pptx"), "none");
});

test("bytes: sizes a person reads", async () => {
  const { bytes } = await import("../src/library.ts");
  assert.equal(bytes(0), "0 B");
  assert.equal(bytes(-5), "0 B");
  assert.equal(bytes(812), "812 B");
  assert.equal(bytes(4300), "4.2 KB");
  assert.equal(bytes(2048), "2 KB");
  assert.equal(bytes(13 * 1024 * 1024 + 5), "13 MB");
  assert.equal(bytes(1.5 * 1024 ** 3), "1.5 GB");
  assert.equal(bytes(3 * 1024 ** 5), "3072 TB");
});
