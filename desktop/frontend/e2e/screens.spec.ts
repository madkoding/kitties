import { expect, test, type Page } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";
import { GATEWAY, HOOK_LOG, SHOTS, STATE } from "./playwright.config";

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

test("settings: models, connections and the gateway", async ({ page }) => {
  await open(page, "#new-settings");
  await expect(page.getByRole("tab", { name: "Models" })).toBeVisible();
  // A model reads as its provider and its model, with who uses it.
  const model = page.getByTestId("model-fast");
  await expect(model).toContainText("OpenAI");
  await expect(model).toContainText("gpt-5.5");
  await expect(page.getByRole("button", { name: "Add a model" })).toBeVisible();
  await shot(page, "08-settings-models");
  await open(page, "#new-connectors");
  // The connections come first, each saying which kitties may use it.
  await expect(page.getByTestId("connection-team-slack")).toContainText("Slack");
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
  // Every service in the catalogue is a button with its logo; a search and categories narrow it.
  const buttons = page.locator(".service-btn");
  expect(await buttons.count()).toBeGreaterThanOrEqual(30);
  expect(await buttons.locator("svg.logo, .logo-fallback").count()).toBe(await buttons.count());
  for (const cat of ["Chat & messages", "Social media", "Code", "Office & files", "Finance"]) {
    await expect(page.locator(".chip", { hasText: cat })).toBeVisible();
  }
  await shot(page, "17-connector-catalogue");
  await page.getByLabel("Search services").fill("google");
  await expect(page.locator(".service-btn", { hasText: "Slack" })).toHaveCount(0);
  await page.locator(".service-btn", { hasText: "Google Workspace" }).click();
  await expect(page.getByText("Gmail, Calendar, Drive, Sheets and Docs").first()).toBeVisible();
  await expect(page.getByText("OAuth client id")).toBeVisible();
  await expect(page.getByText("Save, then sign in with the button")).toBeVisible();
  // GitHub takes a token or a sign-in.
  await page.getByRole("button", { name: "Cancel" }).click();
  await page.getByLabel("Search services").fill("github");
  await page.locator(".service-btn", { hasText: "GitHub" }).click();
  await expect(page.getByText("Paste a token below, or save and then sign in")).toBeVisible();
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

// hooked is what the outside world (the Slack webhook, the ntfy topic) has received so far.
const hooked = () => fs.readFileSync(HOOK_LOG, "utf8");

test("an action that reaches other people waits for a yes, and runs when it is approved", async ({
  page,
}) => {
  await open(page, `#chat/${ids.post}`);
  await expect(page.getByText("That needs your yes first").first()).toBeVisible();
  await expect(page.getByText("waiting for your yes · team-slack.send")).toBeVisible();
  await expect(page.getByTestId("approvals-button")).toContainText("1 waiting");
  expect(hooked()).not.toContain("Kitties launches next week");
  await shot(page, "23-waiting-for-a-yes");

  await open(page, "#new-approvals");
  const card = page.getByTestId("approval").first();
  await expect(card).toContainText("mimi wants to team-slack.send");
  await expect(card).toContainText("Kitties launches next week");
  await expect(card.getByText("asks first")).toBeVisible();
  await shot(page, "24-approvals");
  await card.getByRole("button", { name: "Approve", exact: true }).click();
  await expect(page.getByText("Nothing is waiting.")).toBeVisible();
  await expect(page.getByTestId("decided")).toContainText("approved");
  await expect.poll(hooked).toContain("Kitties launches next week");

  // The result is in the thread, with the service's logo.
  await open(page, `#chat/${ids.post}`);
  const turn = page.locator(".msg.approval-turn").first();
  await expect(turn).toContainText("Approved and done: team-slack.send");
  await expect(turn.locator(".tag-logo svg.logo")).toBeVisible();
  await expect(page.getByTestId("approvals-button")).not.toContainText("waiting");
  await shot(page, "19-connector-call");
});

test("a no with a reason goes to the kitty, which keeps it as a correction", async ({ page }) => {
  // Mimi is asked to post again; this time the person says no.
  const headers = { Authorization: "Bearer e2e-token" };
  const created = await page.request.post(`${GATEWAY}/v1/chats`, {
    headers,
    data: { members: ["mimi"], title: "Second announcement" },
  });
  const chat = ((await created.json()) as { chat: { id: string } }).chat.id;
  const sent = await page.request.post(`${GATEWAY}/v1/chats/${chat}/messages`, {
    headers,
    data: { text: "Mimi, post the announcement to the team again." },
  });
  expect(sent.ok()).toBeTruthy();
  await open(page, "#new-approvals");
  const card = page.getByTestId("approval").first();
  await expect(card).toBeVisible();
  await card.getByRole("button", { name: "Reject…" }).click();
  await card.getByLabel(/Why not/).fill("too informal for the team channel");
  await card.getByRole("button", { name: "Reject", exact: true }).click();
  await expect(page.getByText("Nothing is waiting.")).toBeVisible();
  await expect(page.getByTestId("decided").first()).toContainText("rejected");
  await open(page, `#chat/${chat}`);
  await expect(page.locator(".msg.approval-turn").first()).toContainText(
    "Reason: too informal for the team channel",
  );
  expect(hooked().match(/Kitties launches next week/g)?.length).toBe(1);
});

test("the thumbs under an answer teach the kitty", async ({ page }) => {
  await open(page, `#chat/${ids.single}`);
  // What was said about these turns is kept: a thumb down on the first answer.
  const first = page.locator(".msg.agent").first();
  await expect(first.getByLabel("Not good")).toHaveAttribute("aria-pressed", "true");
  // A new verdict on a group chat: up on one answer, down with a word on another.
  await open(page, `#chat/${ids.group}`);
  const answers = page.locator(".msg.agent");
  await answers.nth(0).getByLabel("Good answer").click();
  await expect(answers.nth(0).getByLabel("Good answer")).toHaveAttribute("aria-pressed", "true");
  await answers.nth(1).getByLabel("Not good").click();
  await answers.nth(1).getByLabel("What was wrong").fill("a little wordy");
  await answers.nth(1).getByLabel("What was wrong").press("Enter");
  await expect(answers.nth(1).getByLabel("Not good")).toHaveAttribute("aria-pressed", "true");
  await shot(page, "25-feedback");
});

test("what a kitty knows about you: its level of demand, its notes and how far it is trusted", async ({
  page,
}) => {
  await open(page, `#kitty/${ids.mimi}`);
  const memory = page.getByTestId("memory");
  await expect(memory).toBeVisible();
  // Four thumbs down with a word moved the level from balanced to demanding.
  await expect(memory.getByRole("button", { name: "4 · demanding" })).toBeVisible();
  await expect(page.getByTestId("level")).toContainText("They are demanding");
  const notes = page.getByTestId("notes");
  await expect(notes).toContainText("Answer in Spanish when I write in Spanish");
  await expect(notes.locator(".tag", { hasText: "yours" })).toHaveCount(1);
  await expect(notes).toContainText("Correction: Too long: two lines at most");
  await expect(notes).toContainText("I prefer short answers");
  // The yes it gave to Slack is on its way to being trusted: twelve in a row for a demanding person.
  await expect(page.getByTestId("trust")).toContainText("team-slack.send");
  await expect(page.getByTestId("trust")).toContainText("of 12 yeses in a row");
  await expect(page.getByLabel("When it wants to publish, send or spend")).toHaveValue("ask");
  // Today's spending against the daily limits.
  await expect(page.getByTestId("spent")).toContainText("of 5,000,000");
  await expect(page.getByTestId("spent")).toContainText("of 5");
  await memory.scrollIntoViewIfNeeded();
  await shot(page, "26-memory");
  // The person can write a note, correct the level and forget a note.
  await page.getByLabel("A note for it to keep").fill("Sign off with my first name");
  await memory.getByRole("button", { name: "Add note" }).click();
  await expect(notes).toContainText("Sign off with my first name");
  await memory.getByRole("button", { name: "Forget: Sign off with my first name" }).click();
  await expect(notes).not.toContainText("Sign off with my first name");
  await memory.getByRole("button", { name: "2 · easygoing" }).click();
  await expect(page.getByTestId("level")).toContainText("They are easygoing");
  await memory.getByRole("button", { name: "4 · demanding" }).click();
  await expect(page.getByTestId("level")).toContainText("They are demanding");
});

test("notifications: pick a channel, send a test and be told when something waits", async ({
  page,
}) => {
  await open(page, "#new-notifications");
  const form = page.getByTestId("notify");
  await expect(form).toBeVisible();
  await form.getByLabel("Send through").selectOption("phone");
  await expect(form.getByLabel("A kitty gave up and needs my help")).toBeChecked();
  await expect(form.getByLabel("An action is waiting for my yes")).toBeChecked();
  await expect(form.getByLabel("A goal finished")).not.toBeChecked();
  await shot(page, "27-notifications");
  await form.getByRole("button", { name: "Save" }).click();
  await expect(form.getByText("Saved.")).toBeVisible();
  await form.getByRole("button", { name: "Send a test" }).click();
  await expect(form.getByText("Sent: look at your phone or channel.")).toBeVisible();
  await expect.poll(hooked).toContain('"topic":"kitties-e2e"');
  await expect.poll(hooked).toContain("Notifications work");
  // A daily summary at a time of the person's choice, and one on demand.
  await form.getByLabel("A summary of the last day, every day at").fill("08:30");
  await form.getByRole("button", { name: "Save" }).click();
  await expect(form.getByText("Saved.")).toBeVisible();
  await form.getByTestId("send-digest").click();
  await expect(form.getByText("Summary sent.")).toBeVisible();
  await expect.poll(hooked).toContain("Your kitties, last 24 hours");
  await expect.poll(hooked).toContain("mimi:");
  await shot(page, "29-daily-summary");
  // An action that waits for the person now reaches their phone, with a link to the thread.
  const headers = { Authorization: "Bearer e2e-token" };
  const created = await page.request.post(`${GATEWAY}/v1/chats`, {
    headers,
    data: { members: ["mimi"], title: "Third announcement" },
  });
  const chat = ((await created.json()) as { chat: { id: string } }).chat.id;
  await page.request.post(`${GATEWAY}/v1/chats/${chat}/messages`, {
    headers,
    data: { text: "Mimi, post the announcement to the team once more." },
  });
  await expect.poll(hooked, { timeout: 20_000 }).toContain("mimi: needs your approval");
  await expect.poll(hooked).toContain(`#chat/${chat}`);
});

test("the catalogue marks what asks first, and the new services are there", async ({ page }) => {
  await open(page, "#new-connectors-add");
  for (const name of ["Stripe", "Zendesk", "ntfy (push)"]) {
    await expect(page.locator(".service-btn", { hasText: name })).toBeVisible();
  }
  await page.locator(".service-btn", { hasText: "Stripe" }).click();
  await page.getByText("What the kitties can do with it").click();
  await expect(page.locator("li", { hasText: "Refund" })).toContainText("always asks you");
  await expect(page.locator("li", { hasText: "Send invoice" })).toContainText("asks you first");
  await shot(page, "28-stripe");
});

test("a connection says which kitties may use it", async ({ page }) => {
  await open(page, "#connector/team-slack");
  const form = page.getByTestId("connection-form");
  await expect(form).toBeVisible();
  const who = page.getByTestId("connection-kitties");
  // The kitties of the demo may use every connection.
  for (const name of ["mimi", "tom", "luna"]) {
    await expect(who.locator(".chip", { hasText: name })).toHaveClass(/on/);
  }
  await shot(page, "33-connection-form");
});

test("adding a model: pick the provider, paste the key, keep the suggested model", async ({
  page,
}) => {
  await open(page, "#new-models-add");
  await expect(page.getByTestId("provider-picker")).toBeVisible();
  await shot(page, "32-pick-provider");
  await page.getByRole("button", { name: /^Anthropic Claude/ }).click();
  const form = page.getByTestId("model-form");
  await expect(form.getByText("Get a key from Anthropic Claude")).toBeVisible();
  await expect(page.getByLabel("Model")).toHaveValue("claude-sonnet-5-5");
  // Without a key there is nothing to save yet.
  await expect(page.getByRole("button", { name: "Save and test" })).toBeDisabled();
  await page.getByLabel("API key").fill("sk-ant-demo");
  await expect(page.getByRole("button", { name: "Save and test" })).toBeEnabled();
  await shot(page, "34-add-model");
});

test("a first run is a checklist, and nothing can be made out of order", async ({ page }) => {
  // A gateway with no model and no kitty yet.
  await page.route("**/v1/kitties", (route) =>
    route.request().method() === "GET"
      ? route.fulfill({ json: { kitties: [] } })
      : route.continue(),
  );
  await page.route("**/v1/chats", (route) =>
    route.request().method() === "GET" ? route.fulfill({ json: { chats: [] } }) : route.continue(),
  );
  await page.route("**/v1/providers", (route) =>
    route.fulfill({ json: { providers: [], kinds: [] } }),
  );
  await page.goto(`/${Q}`);
  const welcome = page.getByTestId("welcome");
  await expect(welcome).toContainText("Welcome to Kitties");
  await expect(welcome).toContainText("3 steps");
  await expect(welcome.getByRole("button", { name: "Create a kitty" })).toBeDisabled();
  await shot(page, "31-welcome");
  // A chat needs a kitty, and a kitty needs a model: each says so and leads to the next step.
  await page.locator("aside").getByRole("button", { name: "New chat" }).click();
  await expect(page.getByTestId("chat-needs-kitty")).toBeVisible();
  await page.locator("[data-modal]").getByRole("button", { name: "Create a kitty" }).click();
  await expect(page.getByTestId("kitty-needs-model")).toBeVisible();
  await page.locator("[data-modal]").getByRole("button", { name: "Add a model" }).click();
  await expect(page.getByTestId("provider-picker")).toBeVisible();
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
  // Its brain, who it is and what it may use are edited right here.
  const basics = page.getByTestId("kitty-basics");
  await expect(basics.getByLabel("Brain")).toHaveValue("strong");
  await expect(basics.getByLabel("Who is it?")).toHaveValue(/patient researcher/);
  await expect(page.getByTestId("kitty-connections").locator(".chip.on").first()).toContainText(
    "All, new ones too",
  );
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
  await expect(page.getByLabel("Tokens per day")).toHaveValue("5000000");
  await expect(page.getByLabel("Actions per day")).toHaveValue("5");
  await goals.scrollIntoViewIfNeeded();
  await shot(page, "21-goals-journal");
  // The sidebar marks the goal chats, and a goal's chat shows the scheduler's call.
  await expect(page.locator("aside .tag", { hasText: "goal" }).first()).toBeVisible();
  await goals.getByText("open chat").first().click();
  await expect(page.locator(".msg.scheduler").first()).toBeVisible();
  await expect(page.getByText("Checked Inbox check").first()).toBeVisible();
  await shot(page, "22-goal-chat");
});

test("in the macOS app the top bars leave room for the window buttons and drag the window", async ({
  page,
}) => {
  await open(page, "");
  // The Wails app adds this class on macOS, where the title bar is hidden and the red, yellow
  // and green buttons float over the page; a browser never has it.
  await page.evaluate(() => document.documentElement.classList.add("mac-inset"));
  const bar = page.locator("aside .titlebar");
  await expect(bar).toHaveCSS("padding-left", "88px");
  await expect(bar).toHaveCSS("--wails-draggable", "drag");
  await expect(page.locator("header.titlebar")).toHaveCSS("--wails-draggable", "drag");
  // What is on a bar stays clickable.
  await expect(page.getByRole("button", { name: "Theme" })).not.toHaveCSS(
    "--wails-draggable",
    "drag",
  );
  // The logo and the name start to the right of the buttons (about 76px wide).
  const logo = await page.locator("aside .titlebar span").first().boundingBox();
  expect(logo!.x).toBeGreaterThan(80);
  await page.screenshot({
    path: "e2e/test-results/mac-inset.png",
    clip: { x: 0, y: 0, width: 600, height: 120 },
  });
});

test("settings still draw against an older gateway that leaves fields out", async ({ page }) => {
  // A gateway from before the catalogue and the notifications: the lists are missing or null.
  await page.route("**/v1/settings", async (route) => {
    await route.fulfill({
      json: { providers: null, connectors: null, default_provider: "", kinds: null },
    });
  });
  await open(page, "#new-settings");
  await expect(page.getByText("No model yet")).toBeVisible();
  await page.getByRole("tab", { name: "Connections" }).click();
  await expect(page.getByText("None yet: pick a service below.")).toBeVisible();
  await page.getByRole("tab", { name: "Notifications" }).click();
  await expect(page.getByTestId("notify")).toBeVisible();
  await expect(page.getByText("Settings could not be drawn")).toHaveCount(0);
});

test("a model can think on a subscription: Claude through Claude Code, Copilot with a code", async ({
  page,
}) => {
  // Nothing is signed in yet (whatever this computer has), until Copilot is.
  let polls = 0;
  await page.route("**/v1/settings", async (route) => {
    const res = await route.fetch();
    const json = await res.json();
    json.accounts = [
      { kind: "claude-code", signed_in: false, available: true },
      { kind: "copilot", signed_in: polls > 1, who: "octocat", available: true },
    ];
    return route.fulfill({ json });
  });
  await open(page, "#new-models-add");
  await page.getByRole("button", { name: /^Anthropic Claude/ }).click();
  const form = page.getByTestId("model-form");
  // A key is the default; the subscription is one click away and walks through Claude Code.
  await expect(form.getByRole("radio", { name: /API key/ })).toHaveAttribute(
    "aria-checked",
    "true",
  );
  await form.getByRole("radio", { name: /Claude subscription/ }).click();
  const signIn = form.getByTestId("sign-in");
  await expect(signIn).toContainText("claude auth login");
  await expect(page.getByLabel("Model")).toHaveValue("sonnet");
  await expect(form.getByLabel("API key")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Save and test" })).toBeDisabled();
  await shot(page, "35-claude-subscription");

  // Copilot signs in with a code typed on GitHub; the panel follows it until it is done.
  await page.route("**/v1/settings/accounts/copilot/login", (route) =>
    route.fulfill({
      json: {
        id: "f1",
        kind: "copilot",
        method: "device",
        url: "about:blank",
        code: "ABCD-1234",
        status: "pending",
      },
    }),
  );
  await page.route("**/v1/settings/logins/f1", (route) => {
    polls++;
    return route.fulfill({
      json: {
        id: "f1",
        kind: "copilot",
        method: "device",
        status: polls > 1 ? "connected" : "pending",
        who: "octocat",
      },
    });
  });
  await form.getByRole("button", { name: "change" }).click();
  await page.getByRole("button", { name: /^GitHub Copilot/ }).click();
  await expect(form.getByRole("radio", { name: /GitHub Copilot/ })).toHaveAttribute(
    "aria-checked",
    "true",
  );
  await form.getByTestId("sign-in-start").click();
  await expect(form.getByTestId("sign-in-code")).toContainText("ABCD-1234");
  await shot(page, "36-copilot-code");
  await expect(form.getByTestId("sign-in")).toContainText("Signed in as octocat", {
    timeout: 10_000,
  });
  await expect(page.getByRole("button", { name: "Save and test" })).toBeEnabled();

  // Qwen's sign-in ended: the page says so and points at a key.
  await form.getByRole("button", { name: "change" }).click();
  await page.getByRole("button", { name: /^Qwen/ }).click();
  await form.getByRole("radio", { name: /qwen.ai account/ }).click();
  await expect(form.getByTestId("sign-in")).toContainText("DashScope");
});

test("the Android app asks for the gateway first, then works as a remote", async ({
  page,
  request,
}) => {
  // The app is this page in a WebView with a bridge; it has no gateway of its own.
  await page.addInitScript(() => {
    (window as unknown as { KittiesAndroid: { open(u: string): void } }).KittiesAndroid = {
      open: (u: string) => ((window as unknown as { opened: string }).opened = u),
    };
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(`/${Q}`);
  const connect = page.getByTestId("android-connect");
  await expect(connect).toContainText("KITTIES_LISTEN=0.0.0.0:7488");
  await expect(page.getByLabel("Address")).toHaveAttribute(
    "placeholder",
    "http://192.168.1.20:7488",
  );
  await shot(page, "37-android-connect");
  const { token } = (await (await request.get(`${GATEWAY}/v1/local-token`)).json()) as {
    token: string;
  };
  await page.getByLabel("Address").fill(new URL(page.url()).origin);
  await page.getByLabel("Token").fill(token);
  await page.getByRole("button", { name: "Connect", exact: true }).click();
  await expect(page.getByRole("banner").getByText("connected")).toBeVisible();
  // The kitties are a tap away, in the menu.
  await page.getByRole("button", { name: "Menu" }).click();
  await expect(kittyRow(page, "mimi")).toBeVisible();
  await shot(page, "38-android-app");
});

test("Facebook and Instagram: a Page or a professional account, and what waits for a yes", async ({
  page,
}) => {
  await open(page, "#new-connectors-add");
  await page.getByRole("button", { name: "Social media" }).click();
  for (const name of ["Facebook Page", "Instagram"]) {
    await expect(page.locator(".service-btn", { hasText: name })).toBeVisible();
  }
  await page.locator(".service-btn", { hasText: "Instagram" }).click();
  await expect(page.getByText("Instagram account id")).toBeVisible();
  await page.getByText("What the kitties can do with it").click();
  await expect(page.locator("li", { hasText: /^Post/ }).first()).toContainText("asks you first");
  await shot(page, "39-instagram");
});
