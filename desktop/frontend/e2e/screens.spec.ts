import { expect, test, type Page } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";
import { GATEWAY, SHOTS, STATE } from "./playwright.config";

// Every screen of the desktop app, checked and photographed. The pictures go to
// docs/screenshots under the names the README uses, so the documentation is never stale.
const ids = JSON.parse(fs.readFileSync(STATE, "utf8")) as {
  single: string;
  group: string;
  desktop: string;
  post: string;
  tom: string;
  mimi: string;
  delegated: string;
};
const Q = "?theme=dark&motion=off";

test.beforeAll(() => fs.mkdirSync(SHOTS, { recursive: true }));

// kittyRow is a kitty's row in the sidebar (its hit button carries the name).
// A chat with one kitty is titled after it, so the kitty row is the last button of that name.
const kittyRow = (page: Page, name: string) =>
  page.locator("aside").getByRole("button", { name, exact: true }).last();

async function open(page: Page, hash: string, query = Q): Promise<void> {
  await page.goto(`/${query}${hash}`);
  await expect(kittyRow(page, "mimi")).toBeAttached();
}

async function shot(page: Page, name: string): Promise<void> {
  // The pixel cats breathe and the panels fade in: motion=off stills them; fonts land first.
  await page.evaluate(() => document.fonts.ready);
  await page.screenshot({ path: path.join(SHOTS, `${name}.png`), animations: "disabled" });
}

test("the empty state lists the kitties", async ({ page }) => {
  await open(page, "");
  for (const name of ["mimi", "tom", "luna"]) {
    await expect(kittyRow(page, name)).toBeAttached();
  }
  await expect(page.getByText("connected")).toBeVisible();
  await shot(page, "01-empty");
});

test("a chat with one kitty: the answer, the skill and the saved artifact", async ({ page }) => {
  await open(page, `#chat/${ids.single}`);
  await expect(page.getByText("Here is how I would split it")).toBeVisible();
  await expect(page.getByText("skill · report-findings")).toBeVisible();
  await expect(page.getByText("launch-plan.md").first()).toBeVisible();
  await shot(page, "02-chat");
});

test("a group chat shows every member", async ({ page }) => {
  await open(page, `#chat/${ids.group}`);
  await expect(page.getByText("Launch team").first()).toBeVisible();
  await expect(page.getByText("I agree with the point above").first()).toBeVisible();
  await shot(page, "03-group");
});

test("a desktop task: the steps and their screenshots", async ({ page }) => {
  await open(page, `#chat/${ids.desktop}`);
  const trail = page.locator(".trail").first();
  await expect(trail.getByText("open", { exact: true })).toBeVisible();
  await expect(trail.getByText("done", { exact: true })).toBeVisible();
  await expect(trail.locator(".shot-thumb img").first()).toBeVisible();
  await shot(page, "04-desktop-task");
  await page.goto(`/?theme=light&motion=off#chat/${ids.desktop}`);
  await expect(trail.getByText("done", { exact: true })).toBeVisible();
  await shot(page, "11-light");
  await page.setViewportSize({ width: 430, height: 900 });
  await page.goto(`/${Q}#chat/${ids.desktop}`);
  await expect(page.locator(".trail").first().getByText("done", { exact: true })).toBeVisible();
  await shot(page, "12-phone");
});

test("a kitty's drawer shows its display", async ({ page }) => {
  await open(page, `#kitty/${ids.tom}`);
  await expect(page.getByRole("dialog").first()).toBeVisible();
  await shot(page, "05-kitty-drawer");
  const res = await page.request.get(`${GATEWAY}/v1/kitties/tom/screenshot`, {
    headers: { Authorization: "Bearer e2e-token" },
  });
  expect(res.ok()).toBeTruthy();
  const png = await res.body();
  expect(png.subarray(1, 4).toString()).toBe("PNG");
  fs.writeFileSync(path.join(SHOTS, "13-tom-display.png"), png);
});

test("new chat and new kitty: pick the members, pick the pixel cat", async ({ page }) => {
  await open(page, `#chat/${ids.group}/new-chat`);
  await expect(page.getByText("New chat").first()).toBeVisible();
  await shot(page, "06-new-chat");
  await open(page, "#new-kitty");
  await expect(page.locator(".pixelcat").first()).toBeVisible();
  await shot(page, "07-new-kitty");
});

test("settings: models, connectors and the gateway", async ({ page }) => {
  await open(page, "#new-settings");
  await expect(page.getByText("Models", { exact: true })).toBeVisible();
  await expect(page.getByText("gpt-5.5").first()).toBeVisible();
  await shot(page, "08-settings-models");
  await open(page, "#new-connectors");
  // Every service in the catalogue is a button with its logo, grouped by category.
  const buttons = page.locator(".service-btn");
  expect(await buttons.count()).toBeGreaterThanOrEqual(30);
  expect(await buttons.locator("svg.logo, .logo-fallback").count()).toBe(await buttons.count());
  for (const cat of [
    "messaging",
    "social media",
    "git",
    "productivity",
    "office",
    "drawing",
    "generic",
  ]) {
    await expect(page.getByText(cat, { exact: true }).first()).toBeVisible();
  }
  await expect(page.getByText("sign in needed")).toBeVisible();
  await shot(page, "09-settings-connectors");
  await open(page, "#new-connect");
  await shot(page, "10-gateway-settings");
});

test("the gateway serves its own page", async ({ page }) => {
  await page.goto(GATEWAY + "/");
  await expect(page.getByText("Kitties").first()).toBeVisible();
  await shot(page, "14-gateway-page");
});

test("the library: skills with a proposal, and the artifacts", async ({ page }) => {
  await open(page, "#new-library");
  await expect(page.getByText("weekly-report").first()).toBeVisible();
  await expect(page.getByText("close-a-cookie-banner").first()).toBeVisible();
  await shot(page, "15-library-skills");
  await open(page, "#new-artifacts");
  await expect(page.getByText("competitor-notes.md").first()).toBeVisible();
  await shot(page, "16-library-artifacts");
});

test("picking a service opens its form; an OAuth kind offers the sign-in", async ({ page }) => {
  await open(page, "#new-connectors-add");
  await expect(page.getByText("Pick the service to connect.")).toBeVisible();
  await shot(page, "17-connector-catalogue");
  await page.locator(".service-btn", { hasText: "Google Workspace" }).click();
  await expect(page.getByText("Gmail, Calendar, Drive, Sheets and Docs").first()).toBeVisible();
  await expect(page.getByText("OAuth client id")).toBeVisible();
  await expect(page.getByText("Save first, then sign in")).toBeVisible();
  // GitHub takes a token or a sign-in.
  await page.getByText("change service").click();
  await page.locator(".service-btn", { hasText: "GitHub" }).click();
  await expect(page.getByText("Paste a token below, or save first")).toBeVisible();
  // The saved Google connector shows the sign-in button, which asks the gateway for a consent URL.
  await open(page, "#connector/workspace");
  const signIn = page.getByRole("button", { name: "Sign in with Google" });
  await expect(signIn).toBeVisible();
  await shot(page, "18-connector-oauth");
  const res = await page.request.post(`${GATEWAY}/v1/oauth/workspace/start`, {
    headers: { Authorization: "Bearer e2e-token" },
  });
  expect(res.ok()).toBeTruthy();
  expect(((await res.json()) as { url: string }).url).toContain("accounts.google.com");
});

test("a kitty posting through a connector shows the service's logo", async ({ page }) => {
  await open(page, `#chat/${ids.post}`);
  await expect(page.getByText("Posted!").first()).toBeVisible();
  const chip = page.locator(".tag-logo", { hasText: "team-slack" }).first();
  await expect(chip).toBeVisible();
  await expect(chip.locator("svg.logo")).toBeVisible();
  await shot(page, "19-connector-call");
});

test("a kitty hands a subtask to another and the chip leads to their thread", async ({ page }) => {
  await open(page, `#chat/${ids.delegated}`);
  await expect(page.getByText("luna took care of it").first()).toBeVisible();
  const chip = page.locator(".tag", { hasText: "→ luna" }).first();
  await expect(chip).toBeVisible();
  await shot(page, "20-delegation");
  await chip.click();
  await expect(page.getByText("mimi → luna").first()).toBeVisible();
  await expect(page.getByText("summarise the launch plan").first()).toBeVisible();
});

test("the kitty's drawer: goals, the journal, limits and the backup", async ({ page }) => {
  await open(page, `#kitty/${ids.mimi}`);
  const goals = page.getByTestId("goals");
  await expect(goals.getByText("Inbox check")).toBeVisible();
  await expect(goals.getByText("Triage new issues")).toBeVisible();
  await expect(goals.getByText("hourly").first()).toBeVisible();
  await expect(goals.getByText("on events from github")).toBeVisible();
  await expect(goals.locator(".tag", { hasText: "done" })).toHaveCount(2);
  const journal = page.getByTestId("journal");
  await expect(journal.locator(".tag", { hasText: "event" }).first()).toBeVisible();
  await expect(journal.locator(".tag", { hasText: "done" }).first()).toBeVisible();
  await expect(journal.locator(".tag", { hasText: "delegated" }).first()).toBeVisible();
  await expect(page.getByLabel("Backup (takes over a task mimi gives up on)")).toHaveValue("luna");
  await expect(page.getByLabel("Retries (another way)")).toHaveValue("2");
  await goals.scrollIntoViewIfNeeded();
  await shot(page, "21-goals-journal");
  // The sidebar marks the goal chats, and a goal's chat shows the scheduler's call.
  await expect(page.locator("aside .tag", { hasText: "goal" }).first()).toBeVisible();
  await goals.getByText("open chat").first().click();
  await expect(page.locator(".msg.scheduler").first()).toBeVisible();
  await expect(page.getByText("Checked Inbox check").first()).toBeVisible();
  await shot(page, "22-goal-chat");
});
