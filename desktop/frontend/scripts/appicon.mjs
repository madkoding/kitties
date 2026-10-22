// Draws the application icon: the tabby kitty of the interface, on a rounded dark tile with the
// neon edge of the design system. It reuses the cat's own grid (src/PixelCat.tsx), so the icon
// is the same animal the person sees in the app.
//
//   npm run icon      # writes ../build/appicon.svg and ../build/appicon.png (1024x1024)
//
// Wails turns appicon.png into the macOS .icns and the Windows .ico when it builds.
import { build } from "esbuild";
import { chromium } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const out = path.resolve(here, "../../build");
fs.mkdirSync(out, { recursive: true });

// Bundle the component's module so its pure drawing code can run here.
const built = await build({
  entryPoints: [path.resolve(here, "../src/PixelCat.tsx")],
  bundle: true,
  write: false,
  format: "esm",
  platform: "node",
  jsx: "automatic",
  jsxImportSource: "preact",
  absWorkingDir: path.resolve(here, ".."),
});
const mod = await import(
  "data:text/javascript;base64," + Buffer.from(built.outputFiles[0].text).toString("base64")
);
const { grid, COATS } = mod;

const coat = COATS.orange;
const paint = {
  1: coat.dark, // outline
  2: coat.body,
  3: coat.light,
  4: "#1b2230", // eye
  5: "#f06a8a", // nose, inner ear
  6: "#ffffff",
  7: coat.dark, // stripes
  8: "#ff2bd6",
};
const g = grid("tabby", "idle", 0);
const cell = 40;
const w = 17 * cell;
const h = 16 * cell;
const ox = (1024 - w) / 2;
const oy = (1024 - h) / 2 + 10;
let rects = "";
g.forEach((row, y) =>
  row.forEach((v, x) => {
    if (paint[v]) {
      rects += `<rect x="${ox + x * cell}" y="${oy + y * cell}" width="${cell}" height="${cell}" fill="${paint[v]}"/>`;
    }
  }),
);
const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="1024" height="1024" viewBox="0 0 1024 1024" shape-rendering="crispEdges">
<defs>
<linearGradient id="bg" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#1a1530"/><stop offset="1" stop-color="#0a0a0f"/></linearGradient>
</defs>
<rect x="64" y="64" width="896" height="896" rx="200" fill="url(#bg)"/>
<rect x="64" y="64" width="896" height="896" rx="200" fill="none" stroke="#38bdf8" stroke-width="14" shape-rendering="geometricPrecision"/>
${rects}
</svg>
`;
fs.writeFileSync(path.join(out, "appicon.svg"), svg);

// Rasterise with the browser the tests already use, on a transparent page.
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1024, height: 1024 } });
await page.setContent(`<style>html,body{margin:0;background:transparent}</style>${svg}`);
await page.screenshot({ path: path.join(out, "appicon.png"), omitBackground: true });
await browser.close();
console.log("icon written to", out);

// The background of the macOS disk image (desktop/dmg), at 1x and 2x: CI joins them into one
// retina TIFF. The picture is taller (460) than the visible area (about 436 to 444, since the
// title bar's height varies with the macOS version): Finder anchors it at the top and neither
// scales nor repeats it, so anything lower than 430 is only background, and a window a little
// shorter or taller than expected shows no white strip. The app sits at (170,165), the Applications shortcut at (490,165) and the note at (330,345).
const dmgDir = path.resolve(here, "../../dmg");
fs.mkdirSync(dmgDir, { recursive: true });
const arrow = [
  "..........X.....",
  "...........X....",
  "............X...",
  "XXXXXXXXXXXXXX..",
  "............X...",
  "...........X....",
  "..........X.....",
]
  .map((row, y) =>
    [...row]
      .map((c, x) => (c === "X" ? `<rect x="${x * 8}" y="${y * 8}" width="8" height="8"/>` : ""))
      .join(""),
  )
  .join("");
// Finder paints a disk image's labels black whatever the background, so each label gets a light
// plate behind it (centred under its icon: the label sits 16pt below the icon's bottom edge).
const ICON = 96;
const plate = (cx, cy, w) =>
  `<div class="plate" style="left:${cx - w / 2}px;top:${cy + ICON / 2 + 16 - 12}px;width:${w}px"></div>`;
const backdrop = `<!doctype html><style>
html,body{margin:0;width:660px;height:460px;overflow:hidden}
body{position:relative;background:linear-gradient(#1a1530,#0a0a0f);font-family:"DejaVu Sans Mono","SF Mono",Menlo,monospace;color:#e6e6f0}
.t{position:absolute;top:26px;left:0;right:0;text-align:center;font-size:20px;letter-spacing:.5px}
.t b{color:#38bdf8}
.n{position:absolute;top:258px;left:0;right:0;text-align:center;font-size:12px;color:#9a9ab0}
.n code{color:#f39a3b}
svg{position:absolute;left:268px;top:137px}
.edge{position:absolute;left:6px;right:6px;top:6px;height:420px;border:2px solid #38bdf8;border-radius:22px;opacity:.55}
.plate{position:absolute;height:24px;border-radius:12px;background:#dfe4f2;opacity:.94}
</style>
<div class="edge"></div>
<div class="t">Drag <b>Kitties</b> to Applications</div>
<svg width="128" height="56" shape-rendering="crispEdges" fill="#38bdf8">${arrow}</svg>
${plate(170, 165, 92)}${plate(490, 165, 114)}${plate(330, 345, 152)}
<div class="n">First time? Open <code>READ ME FIRST.txt</code> before you start the app.</div>`;
for (const [scale, file] of [
  [1, "background.png"],
  [2, "background@2x.png"],
]) {
  const b = await chromium.launch();
  const pg = await b.newPage({ viewport: { width: 660, height: 460 }, deviceScaleFactor: scale });
  await pg.setContent(backdrop);
  await pg.screenshot({ path: path.join(dmgDir, file) });
  await b.close();
}
console.log("disk image background written to", dmgDir);
