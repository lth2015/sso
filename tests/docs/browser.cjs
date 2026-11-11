const { chromium } = require("playwright"),
  path = require("node:path"),
  { createServer } = require("node:http"),
  { readFileSync } = require("node:fs"),
  assert = require("node:assert/strict");
let phase = "startup";
(async () => {
  const browser = await chromium.launch({
    executablePath: process.env.CHROMIUM_PATH || "/usr/bin/chromium",
    headless: true,
    args: ["--no-sandbox"],
  });
  const html = readFileSync("docs/dist/index.html");
  const server = createServer((request, response) => {
    if (request.url !== "/") {
      response.writeHead(404);
      response.end();
      return;
    }
    response.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
    response.end(html);
  });
  await new Promise((done) => server.listen(0, "127.0.0.1", done));
  const origin = "http://127.0.0.1:" + server.address().port + "/";
  try {
    const page = await browser.newPage({
        viewport: { width: 1440, height: 1000 },
      }),
      network = [],
      errors = [];
    page.on("request", (r) => {
      if (/^https?:/.test(r.url()) && r.url() !== origin)
        network.push("unexpected network");
    });
    page.on("pageerror", () => errors.push("page error"));
    phase = "self-contained-navigation";
    await page.goto(origin);
    await page
      .getByRole("heading", { name: "SSO 完整设计与开发手册", exact: true })
      .waitFor();
    phase = "scripts-and-form-boundary";
    assert.equal(await page.locator("script,form").count(), 0);
    phase = "playground-heading";
    assert(await page.getByRole("heading", { name: /API Playground/ }).count());
    phase = "api-heading";
    assert(
      await page
        .getByRole("heading", { name: "GET /v1/me", exact: true })
        .count(),
    );
    phase = "section-anchors";
    assert.equal(
      await page
        .locator("nav a")
        .evaluateAll((links) =>
          links.every((a) =>
            document.getElementById(decodeURIComponent(a.hash.slice(1))),
          ),
        ),
      true,
    );
    await page.screenshot({
      path: path.resolve("docs/ui/documentation-index.png"),
    });
    phase = "mobile-layout";
    await page.setViewportSize({ width: 390, height: 844 });
    assert.equal(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      ),
      false,
    );
    phase = "network-boundary";
    assert.deepEqual(network, []);
    assert.deepEqual(errors, []);
    console.log(
      "Offline handbook passed: all section anchors, API/Playground content, no scripts/forms/additional network requests, desktop/mobile",
    );
  } finally {
    await browser.close();
    await new Promise((done) => server.close(done));
  }
})().catch((error) => {
  console.error("Offline handbook failed: " + phase + " / " + error.name);
  process.exitCode = 1;
});
