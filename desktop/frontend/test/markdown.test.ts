import { test } from "node:test";
import assert from "node:assert/strict";
import { artifactName, headings, highlight, inline, markdown, slug } from "../src/markdown.ts";

const wails = "http://wails.localhost/v1/artifacts/borrador-CV-continuum-backend-go-gcp.md";

test("artifactName: the address of an artifact on any host, with or without a token", () => {
  assert.equal(artifactName(wails), "borrador-CV-continuum-backend-go-gcp.md");
  assert.equal(artifactName("/v1/artifacts/a%20b.md?token=x"), "a b.md");
  assert.equal(artifactName("https://gw.example:7488/v1/artifacts/n.md#top"), "n.md");
  assert.equal(artifactName("/v1/artifacts/%E0%A4%A.md"), "%E0%A4%A.md");
  assert.equal(artifactName("https://example.com/v1/chats/x"), "");
  assert.equal(artifactName("/v1/artifacts/a/b"), "");
});

test("inline: a bare address and a [label](address) become links to the artifact", () => {
  assert.deepEqual(inline(`Mira ${wails}.`), [
    "Mira ",
    {
      t: "a",
      v: "borrador-CV-continuum-backend-go-gcp.md",
      name: "borrador-CV-continuum-backend-go-gcp.md",
    },
    ".",
  ]);
  assert.deepEqual(inline("[mi CV](/v1/artifacts/cv.md)"), [{ t: "a", v: "mi CV", name: "cv.md" }]);
});

test("inline: bold, code, italic and web links; other schemes are plain text", () => {
  assert.deepEqual(inline("**a** `b` *c* [d](https://x.io/p) e"), [
    { t: "b", v: "a" },
    " ",
    { t: "c", v: "b" },
    " ",
    { t: "i", v: "c" },
    " ",
    { t: "l", v: "d", href: "https://x.io/p" },
    " e",
  ]);
  assert.deepEqual(inline("[x](javascript:alert(1))"), ["x", ")"]);
  assert.deepEqual(inline("[x](javascript:void)"), ["x"]);
  assert.deepEqual(inline("see https://x.io/a, ok"), [
    "see ",
    { t: "l", v: "https://x.io/a", href: "https://x.io/a" },
    ",",
    " ok",
  ]);
});

test("markdown: every block", () => {
  const doc = [
    "# Title",
    "",
    "one",
    "two",
    "",
    "- a",
    "  more",
    "- b",
    "",
    "1. x",
    "2) y",
    "",
    "```go",
    "fmt.Println()",
    "```",
    "> quoted",
    "> text",
    "---",
    "| h1 | h2 |",
    "|----|:--:|",
    "| 1 | 2 |",
    "| 3 | 4 |",
    "tail",
  ].join("\r\n");
  assert.deepEqual(markdown(doc), [
    { t: "h", level: 1, text: "Title" },
    { t: "p", text: "one two" },
    { t: "ul", items: ["a more", "b"] },
    { t: "ol", items: ["x", "y"] },
    { t: "code", text: "fmt.Println()", lang: "go" },
    { t: "quote", text: "quoted text" },
    { t: "hr" },
    {
      t: "table",
      head: ["h1", "h2"],
      rows: [
        ["1", "2"],
        ["3", "4"],
      ],
    },
    { t: "p", text: "tail" },
  ]);
  assert.deepEqual(markdown("```\nunclosed"), [{ t: "code", text: "unclosed", lang: "" }]);
  assert.deepEqual(markdown(""), []);
});

test("markdown: a chat message keeps its line breaks", () => {
  assert.deepEqual(markdown("one\ntwo", true), [{ t: "p", text: "one\ntwo" }]);
  assert.deepEqual(markdown("one\ntwo"), [{ t: "p", text: "one two" }]);
});

test("highlight: comments, strings, numbers and keywords by the fence's language", () => {
  assert.deepEqual(highlight('// hi\nfunc main() { s := "a\\"b"; n := 4.2 }', "go"), [
    { k: "com", v: "// hi" },
    { k: "", v: "\n" },
    { k: "kw", v: "func" },
    { k: "", v: " main() { s := " },
    { k: "str", v: '"a\\"b"' },
    { k: "", v: "; n := " },
    { k: "num", v: "4.2" },
    { k: "", v: " }" },
  ]);
  assert.deepEqual(highlight("x = 1 # c\nprint('s')", "py"), [
    { k: "", v: "x = " },
    { k: "num", v: "1" },
    { k: "", v: " " },
    { k: "com", v: "# c" },
    { k: "", v: "\nprint(" },
    { k: "str", v: "'s'" },
    { k: "", v: ")" },
  ]);
  assert.deepEqual(highlight("SELECT 1 -- c", "sql"), [
    { k: "kw", v: "SELECT" },
    { k: "", v: " " },
    { k: "num", v: "1" },
    { k: "", v: " " },
    { k: "com", v: "-- c" },
  ]);
  assert.deepEqual(highlight("/* a */ `t`", "js"), [
    { k: "com", v: "/* a */" },
    { k: "", v: " " },
    { k: "str", v: "`t`" },
  ]);
  assert.deepEqual(highlight("<b>x</b>", ""), [{ k: "", v: "<b>x</b>" }]);
  assert.deepEqual(highlight("", ""), []);
  assert.deepEqual(highlight("a ", "js"), [{ k: "", v: "a " }]);
});

test("headings: anchors for the table of contents, made unique", () => {
  assert.equal(slug("Next **steps**!"), "next-steps");
  assert.equal(slug("Año 2026 · plan"), "año-2026-plan");
  assert.equal(slug("***"), "section");
  assert.deepEqual(headings(markdown("# A\ntext\n## B\n## B\n### `C`")), [
    { level: 1, text: "A", id: "a" },
    { level: 2, text: "B", id: "b" },
    { level: 2, text: "B", id: "b-2" },
    { level: 3, text: "`C`", id: "c" },
  ]);
});
