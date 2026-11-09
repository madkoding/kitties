import assert from "node:assert/strict";
import { test } from "node:test";
import { splitQuote } from "../src/quote.ts";

test("splitQuote takes the line a WhatsApp reply starts with off its text", () => {
  assert.deepEqual(splitQuote('(replying to Ana (+569): "hola")\nsí'), [
    'replying to Ana (+569): "hola"',
    "sí",
  ]);
  assert.deepEqual(splitQuote("(replying to +569)\nok\nmore"), ["replying to +569", "ok\nmore"]);
  assert.deepEqual(splitQuote("(replying to an earlier message)"), [
    "replying to an earlier message",
    "",
  ]);
  assert.deepEqual(splitQuote("hola (replying to x)"), ["", "hola (replying to x)"]);
});
