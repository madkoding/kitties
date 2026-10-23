// Photographs the live desktop of a real kitty for the README (docs/screenshots 48 and 49): the
// e2e tests run on the demo's drawn desktop, these pictures are of a real kitty-desktop container.
//
//   make image && kitties gateway          # with a kitty called tom that has a desktop
//   cd desktop/frontend && node scripts/live-desktop-shots.mjs http://127.0.0.1:7488 TOKEN
//
// It opens Chromium and a terminal on tom's display through the API, then, on the gateway's own
// page, opens the live desktop, takes control and types a command in the terminal.
import { chromium } from "@playwright/test";

const [base = "http://127.0.0.1:7488", token = ""] = process.argv.slice(2);
const out = new URL("../../../docs/screenshots/", import.meta.url).pathname;

async function exec(argv) {
  const res = await fetch(`${base}/v1/kitties/tom/exec`, {
    method: "POST",
    headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
    body: JSON.stringify({ argv }),
  });
  if (!res.ok) {
    throw new Error(`exec ${argv.join(" ")}: HTTP ${res.status}`);
  }
}

const wait = (ms) => new Promise((r) => setTimeout(r, ms));
// A page of its own, so the picture needs no network (Chromium ignores chrome:// pages given on
// its command line).
const page0 =
  "data:text/html,<title>tom's notes</title><body style='font:20px sans-serif;padding:40px'>" +
  "<h1>Hello from tom's desktop</h1><p>Chromium, a terminal and IceWM, in tom's own container.</p>";
await exec(["kitty-open", page0]);
await wait(4000);
await exec(["kitty-launch", "xterm", "-geometry", "74x16+620+420"]);
await wait(3000);

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
await page.goto(`${base}/?theme=dark&motion=off`);
await page.locator("aside").getByRole("button", { name: "tom", exact: true }).last().click();
await page.locator("[data-modal]").getByTestId("open-desktop").click();
const live = page.getByTestId("live-desktop");
const frame = live.getByTestId("live-frame");
await frame.waitFor();
await page.waitForTimeout(2500);
await page.screenshot({ path: `${out}48-live-desktop.png` });

await live.getByTestId("take-control").click();
await page.waitForTimeout(500);
const box = await frame.boundingBox();
const at = (x, y) => ({ x: (x * box.width) / 1280, y: (y * box.height) / 800 });
// The terminal's window (in the display's own pixels), then a command typed in it.
await frame.click({ position: at(800, 600) });
await page.waitForTimeout(800);
await page.keyboard.type("echo hola desde el escritorio de tom; uname -sm");
await page.keyboard.press("Enter");
await page.waitForTimeout(2500);
await page.screenshot({ path: `${out}49-live-desktop-control.png` });
await live.getByTestId("give-back").click();
await browser.close();
