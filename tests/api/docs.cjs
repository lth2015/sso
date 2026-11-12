const { chromium } = require("playwright");
const path = require("node:path");
const assert = require("node:assert/strict");
(async () => {
  const browser = await chromium.launch({
    executablePath: process.env.CHROMIUM_PATH || "/usr/bin/chromium",
    headless: true,
    args: ["--no-sandbox"],
  });
  try {
    const page = await browser.newPage({
      viewport: { width: 1440, height: 1080 },
    });
    const errors = [];
    page.on("pageerror", () => errors.push("page error"));
    await page.goto("http://api.localhost:8083/docs");
    await page
      .getByRole("heading", { name: "接口参考", exact: true })
      .waitFor();
    assert.equal(await page.locator("tbody tr").count(), 60);
    assert.equal(await page.locator("details").count(), 60);
    const role = page.locator("#createRole");
    await role.locator("summary").click();
    assert((await role.textContent()).includes("policies"));
    assert((await role.textContent()).includes("example"));
    await role.locator("summary").click();
    assert.equal(await page.locator("form").count(), 0);
    await page.screenshot({
      path: path.resolve("docs/ui/api-reference-desktop.png"),
      fullPage: true,
    });
    await page.setViewportSize({ width: 390, height: 844 });
    assert.equal(
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth,
      ),
      false,
    );
    await page.screenshot({
      path: path.resolve("docs/ui/api-reference-mobile.png"),
      fullPage: true,
    });
    assert.equal((await page.context().cookies()).length, 0);
    assert.deepEqual(errors, []);
    console.log(
      "Public API reference passed: 60 live contract operations, desktop/mobile, no credentials",
    );
  } finally {
    await browser.close();
  }
})().catch(() => {
  console.error("Public API reference failed (safe diagnostics only)");
  process.exitCode = 1;
});
