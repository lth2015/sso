const s = require("../support/console-fixtures.cjs"),
  assert = require("node:assert/strict"),
  { execFileSync } = require("node:child_process"),
  path = require("node:path");
let phase = "startup",
  checks = 0;
const progress = setInterval(
  () => console.log("Security events acceptance phase: " + phase),
  20000,
);
function validate(result) {
  execFileSync("python3", ["tests/api/validate_contract.py"], {
    cwd: s.root,
    input: JSON.stringify({
      contract: "console",
      path: "/bff/v1/me/security-events",
      method: "GET",
      status: result.status,
      body: result.body,
      content_type: result.contentType,
      request_id: result.requestID,
    }),
    stdio: ["pipe", "pipe", "pipe"],
  });
  checks++;
  return result;
}
async function events(p, query = "", expected = 200) {
  const result = await s.bff(p, "/v1/me/security-events" + query);
  assert.equal(result.status, expected);
  return validate(result).body;
}
function normalizeTime(value) {
  const [second, fraction = ""] = value.replace(/Z$/, "").split(".");
  return second + "." + fraction.padEnd(9, "0") + "Z";
}
async function loaded(p, count) {
  await p
    .getByRole("region", { name: "个人安全记录", exact: true })
    .getByRole("button", { name: "刷新最新记录", exact: true })
    .waitFor();
  await p.waitForFunction(
    (n) =>
      document.querySelector('section[aria-label="个人安全记录"] tbody')?.rows
        .length === n,
    count,
  );
}
(async () => {
  try {
    await s.start();
    const owner = await s.register(true),
      guest = await s.register();
    const me = await s.login(owner);
    await s.login(guest);
    phase = "self-only-events-and-query-bounds";
    const initial = await events(owner.page),
      other = await events(guest.page);
    for (const action of [
      "account.registered",
      "mfa.enrolled",
      "session.authenticated",
    ])
      assert(initial.items.some((x) => x.action === action));
    assert(
      initial.items.every(
        (x) => Object.keys(x).sort().join(",") === "action,id,occurred_at",
      ),
    );
    assert(!other.items.some((x) => initial.items.some((y) => x.id === y.id)));
    await events(guest.page, "?after=" + initial.items[0].id, 404);
    await events(owner.page, "?after=" + "f".repeat(32), 404);
    await events(owner.page, "?after=" + "0".repeat(32), 404);
    for (const query of [
      "?user_id=" + me.user_id,
      "?limit=0",
      "?limit=101",
      "?after=bad",
      "?limit=1&limit=2",
    ])
      await events(owner.page, query, 400);
    phase = "create-real-cancellation-events";
    const auth = await owner.context.newPage();
    auth.on("pageerror", () => s.pageErrors.push("auth page error"));
    for (let i = 0; i < 23; i++) {
      await auth.goto("http://auth.localhost:8080/sso/security/authenticator");
      const id = await auth
          .locator('meta[name="change-id"]')
          .getAttribute("content"),
        csrf = await auth
          .locator('meta[name="change-csrf"]')
          .getAttribute("content");
      const status = await auth.evaluate(
        async ({ id, csrf }) =>
          (
            await fetch("/v1/sso/authenticator/" + id + "/cancel", {
              method: "POST",
              headers: {
                "Content-Type": "application/json",
                "X-CSRF-Token": csrf,
              },
              body: "{}",
            })
          ).status,
        { id, csrf },
      );
      assert.equal(status, 204);
    }
    await auth.close();
    const first = await events(owner.page, "?limit=20");
    assert.equal(first.items.length, 20);
    assert(first.items.every((x) => x.action === "mfa.replacement_cancelled"));
    assert(first.next_cursor);
    let second = await events(
      owner.page,
      "?limit=20&after=" + first.next_cursor,
    );
    assert(second.items.length > 0 && second.items.length < 20);
    assert.equal(second.next_cursor, null);
    assert(!second.items.some((x) => first.items.some((y) => x.id === y.id)));
    const all = [...first.items, ...second.items];
    assert.equal(
      all.filter((x) => x.action === "mfa.replacement_cancelled").length,
      23,
    );
    for (let i = 1; i < all.length; i++) {
      const a = all[i - 1],
        b = all[i];
      assert(
        normalizeTime(a.occurred_at) > normalizeTime(b.occurred_at) ||
          (a.occurred_at === b.occurred_at && a.id > b.id),
      );
    }
    phase = "same-user-across-space-contexts";
    const spaces = (await s.bff(owner.page, "/v1/tenants")).body.items;
    const otherSpace = spaces.find((x) => x.id !== me.tenant_id);
    assert(otherSpace);
    const tab = await owner.context.newPage();
    await s.login(owner, tab, otherSpace.id);
    const across = await events(tab, "?limit=20");
    assert.deepEqual(
      across.items,
      (await events(owner.page, "?limit=20")).items,
    );
    second = await events(owner.page, "?limit=20&after=" + across.next_cursor);
    phase = "console-pagination-refresh-and-responsive-layout";
    const p = owner.page;
    await s.nav(p, "安全会话");
    await loaded(p, 20);
    const region = p.getByRole("region", { name: "个人安全记录", exact: true });
    await region
      .getByRole("button", { name: "下一页（较早记录）", exact: true })
      .click();
    await loaded(p, second.items.length);
    assert.equal(
      await region
        .getByRole("button", { name: "下一页（较早记录）", exact: true })
        .count(),
      0,
    );
    await region
      .getByRole("button", { name: "刷新最新记录", exact: true })
      .click();
    await loaded(p, 20);
    await p.screenshot({
      path: path.join(s.root, "docs/ui/security-events-desktop.png"),
      fullPage: true,
    });
    await p.setViewportSize({ width: 390, height: 844 });
    assert(
      await p.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    );
    await p.screenshot({
      path: path.join(s.root, "docs/ui/security-events-mobile.png"),
      fullPage: true,
    });
    await p.setViewportSize({ width: 1280, height: 720 });
    phase = "discard-real-late-response-after-navigation";
    let release, seen, done;
    const held = new Promise((r) => (release = r)),
      arrived = new Promise((r) => (seen = r)),
      finished = new Promise((r) => (done = r));
    let interceptFailed = false;
    await p.route("**/bff/v1/me/security-events?*", async (route) => {
      try {
        const url = new URL(route.request().url());
        const response = await route.fetch({
          url: "http://127.0.0.1:3003" + url.pathname + url.search,
          headers: {
            ...(await route.request().allHeaders()),
            host: "console.localhost:3003",
          },
          maxRedirects: 0,
          maxRetries: 0,
        });
        if (response.status() !== 200) interceptFailed = true;
        seen();
        await held;
        await route.fulfill({ response }).catch(() => {});
      } catch {
        interceptFailed = true;
        seen();
      } finally {
        done();
      }
    });
    try {
      await region
        .getByRole("button", { name: "刷新最新记录", exact: true })
        .click();
      await arrived;
      await s.nav(p, "空间概览");
      release();
      await finished;
      assert(!interceptFailed);
      assert.equal(
        await p
          .getByRole("region", { name: "个人安全记录", exact: true })
          .count(),
        0,
      );
      await p.getByRole("heading", { name: "空间概览", exact: true }).waitFor();
    } finally {
      release();
      await p.unroute("**/bff/v1/me/security-events?*");
    }
    phase = "real-playground-new-operation";
    await s.nav(p, "API Playground");
    await p
      .getByLabel("选择 API", { exact: true })
      .selectOption("listMySecurityEvents");
    await p.getByRole("button", { name: "发送读取请求", exact: true }).click();
    await p
      .getByRole("region", { name: "API 响应", exact: true })
      .getByRole("heading", { name: "HTTP 200", exact: true })
      .waitFor();
    assert(
      (
        await p
          .getByRole("region", { name: "API 响应", exact: true })
          .textContent()
      ).includes("mfa.replacement_cancelled"),
    );
    phase = "revoked-authority-rejected";
    assert.equal(
      (
        await s.bff(p, "/v1/sessions/" + me.session_id + "/revoke", {
          method: "POST",
          body: {},
        })
      ).status,
      204,
    );
    await events(p, "", 401);
    assert.deepEqual(s.pageErrors, []);
    assert.deepEqual(s.leaks, []);
    console.log(
      "Security events acceptance passed: " +
        checks +
        " contract responses; real security actions, self-only/foreign-cursor bounds, chronological pagination, cross-space identity, UI refresh/mobile, late-response cancellation, Playground and revocation",
    );
  } finally {
    clearInterval(progress);
    await s.close();
  }
})().catch((e) => {
  console.error(
    "Security events failed: phase=" +
      phase +
      ", error=" +
      e.name +
      " (private details withheld)",
  );
  console.error(JSON.stringify(s.diagnostics()));
  process.exitCode = 1;
});
