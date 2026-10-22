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
