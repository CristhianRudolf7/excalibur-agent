// Verify the production bundle with real Chrome and the real same-origin daemon.
import { createRequire } from "node:module";
const require = createRequire(
  new URL("../../apps/web/package.json", import.meta.url),
);
const { chromium, expect } = require("@playwright/test");
const base = process.argv[2] || "http://127.0.0.1:7787";
const browser = await chromium.launch({ channel: "chrome", headless: true });
try {
  const context = await browser.newContext({ locale: "en-US" });
  await context.addInitScript(() => localStorage.setItem("i18nextLng", "en"));
  const page = await context.newPage();
  const errors = [];
  let eventFrames = 0;
  page.on("websocket", (socket) => {
    if (new URL(socket.url()).pathname === "/ws")
      socket.on("framereceived", () => eventFrames++);
  });
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto(`${base}/settings?section=connections`);
  await expect(
    page
      .getByRole("region", { name: /ChatGPT \/ Codex/ })
      .getByRole("button", { name: /^(Connect|Conectar)$/ }),
  ).toBeVisible({ timeout: 30_000 });
  const status = await page.evaluate(async () => {
    const response = await fetch("/rpc", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        jsonrpc: "2.0",
        id: "browser",
        method: "codex.status",
        params: {},
      }),
    });
    return { status: response.status, body: await response.json() };
  });
  if (
    status.status !== 200 ||
    status.body.error ||
    status.body.result.connected
  )
    throw new Error("Clean first-run connection status failed");
  if (process.env.EXCALIBUR_SMOKE_NO_CHAT === "1") {
    await page
      .getByRole("region", { name: /ChatGPT \/ Codex/ })
      .getByRole("button", { name: /^(Connect|Conectar)$/ })
      .click();
    const link = page.getByRole("link", {
      name: /Open Sign in with ChatGPT|Abrir inicio de sesión con ChatGPT/,
    });
    await expect(link).toBeVisible();
    const destination = new URL(await link.getAttribute("href"));
    if (
      destination.protocol !== "https:" ||
      !destination.searchParams.get("state")
    )
      throw new Error("Account login did not start");
  }
  if (process.env.EXCALIBUR_SMOKE_NO_CHAT !== "1") {
    await page.goto(`${base}/agents/excalibur?view=chat`);
    const composer = page.locator("textarea").first();
    await expect(composer).toBeVisible({ timeout: 30_000 });
    await composer.fill("Release browser smoke message");
    const receipt = page.waitForResponse(
      (response) =>
        response.url() === `${base}/rpc` &&
        response.request().postDataJSON()?.method === "conversation.send",
    );
    await composer.press("Enter");
    const response = await receipt;
    const reply = await response.json();
    if (!response.ok() || reply.error || !reply.result.msg_id)
      throw new Error("Chat did not reach the daemon");
  }
  await expect.poll(() => eventFrames, { timeout: 15_000 }).toBeGreaterThan(0);
  if (errors.length) throw new Error(`Browser errors: ${errors.join("; ")}`);
  console.log(
    process.env.EXCALIBUR_SMOKE_NO_CHAT === "1"
      ? "Chrome smoke passed: clean first-run account login, same-origin RPC and event WebSocket."
      : "Chrome smoke passed: connection UI, same-origin RPC, event WebSocket and chat receipt.",
  );
  await page.screenshot({ path: "/tmp/excalibur-release-chrome.png" });
  await context.close();
} finally {
  await browser.close();
}
