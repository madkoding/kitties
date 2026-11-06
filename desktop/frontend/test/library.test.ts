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
