const s = require("../support/console-fixtures.cjs"),
  assert = require("node:assert/strict"),
  { execFileSync } = require("node:child_process"),
  path = require("node:path");
let phase = "startup",
  checks = 0;
const progress = setInterval(
  () => console.log("Application access acceptance phase: " + phase),
  20000,
);
function normalized(url) {
  return url
    .split("?")[0]
    .replace(/\/tenants\/[a-f0-9]{32}/, "/tenants/{tenant}")
    .replace(/\/applications\/[a-f0-9]{32}/, "/applications/{application}");
}
async function call(page, method, url, body, status = 200, etag) {
  const r = await s.bff(page, url, {
    method,
    body,
    headers: etag ? { "If-Match": etag } : {},
  });
  if (
    !(Array.isArray(status) ? status.includes(r.status) : r.status === status)
  )
    console.error(
      "Unexpected status: " +
        method +
        " " +
        normalized(url) +
        " expected=" +
        JSON.stringify(status) +
        " received=" +
        r.status,
    );
  if (Array.isArray(status)) assert(status.includes(r.status));
  else assert.equal(r.status, status);
  execFileSync("python3", ["tests/api/validate_contract.py"], {
    cwd: s.root,
    input: JSON.stringify({
      contract: "console",
      path: "/bff" + normalized(url),
      method,
      status: r.status,
      body: r.body,
      request_body: body,
      content_type: r.contentType,
      request_id: r.requestID,
    }),
    stdio: ["pipe", "pipe", "pipe"],
  });
  checks++;
  return r;
}
async function appLogin(f, origin, tenant) {
  const p = await f.context.newPage();
  p.on("pageerror", () => s.pageErrors.push("app error"));
  await p.goto(origin + "/login");
  await p.getByRole("heading", { name: "确认本次授权", exact: true }).waitFor();
  await p.locator("#tenant").selectOption(tenant);
  await p.getByRole("button", { name: "同意并继续", exact: true }).click();
  await p.locator("#session").waitFor({ state: "visible" });
  return p;
}
async function holdResponse(page, pattern) {
  let release, seen, finished;
  const gate = new Promise((r) => (release = r)),
    arrived = new Promise((r) => (seen = r)),
    done = new Promise((r) => (finished = r)),
    state = { failed: false };
  await page.route(pattern, async (route) => {
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
      if (response.status() !== 200) state.failed = true;
      seen();
      await gate;
      await route.fulfill({ response }).catch(() => {});
    } catch {
      state.failed = true;
      seen();
    } finally {
      finished();
    }
  });
  return {
    arrived,
    done,
    state,
    release,
    async close() {
      release();
      await page.unroute(pattern);
    },
  };
}
async function resource(p) {
  return p.evaluate(async () => {
    const r = await fetch("/api/me");
    return r.status;
  });
}
(async () => {
  try {
    await s.start();
    const owner = await s.register(true),
      guest = await s.register();
    let me = await s.login(owner);
    await s.login(guest);
    execFileSync("scripts/sso", ["enable", "--email", owner.email], {
      cwd: s.root,
      stdio: "pipe",
    });
    const tenant = me.tenant_id,
      base = "/v1/tenants/" + tenant,
      invitation = await s.bff(owner.page, base + "/invitations", {
        method: "POST",
        body: { email: guest.email },
      });
    assert.equal(invitation.status, 201);
    assert.equal(
      (
        await s.bff(guest.page, "/v1/invitations/accept", {
          method: "POST",
          body: { token: invitation.body.token },
        })
      ).status,
      201,
    );
    me = await s.login(owner, owner.page, tenant);
    await s.login(guest, guest.page, tenant);
    const otherTenant = (
      await s.bff(owner.page, "/v1/tenants")
    ).body.items.find((t) => t.id !== tenant).id;
    const otherPage = await owner.context.newPage();
    await s.login(owner, otherPage, otherTenant);
    phase = "owner-metadata-client-pagination-and-boundaries";
    const apps = (await call(owner.page, "GET", base + "/applications")).body
      .items;
    const demo = apps.find((a) => a.code === "local-demo"),
      platform = apps.find((a) => a.protected),
      target = base + "/applications/" + demo.id;
    assert(demo.enabled && demo.available && platform.enabled);
    await call(owner.page, "GET", base + "/applications?limit=1");
    let detail = await call(owner.page, "GET", target);
    const clients = await call(owner.page, "GET", target + "/clients?limit=1");
    assert(clients.body.next_cursor);
    assert.deepEqual(Object.keys(clients.body.items[0]).sort(), [
      "active",
      "audience",
      "client_id",
      "redirect_uri",
      "scopes",
      "version",
    ]);
    await call(
      owner.page,
      "GET",
      target + "/clients?after=" + encodeURIComponent(clients.body.next_cursor),
    );
    for (const query of [
      "?after=client%0A",
      "?after=" + "a".repeat(129),
      "?after=a&after=b",
      "?limit=101",
    ])
      await call(
        owner.page,
        "GET",
        target + "/clients" + query,
        undefined,
        400,
      );
    await call(guest.page, "GET", base + "/applications", undefined, 403);
    await call(guest.page, "GET", target, undefined, 403);
    await call(guest.page, "GET", target + "/clients", undefined, 403);
    await call(guest.page, "POST", target + "/disable", {}, 403, detail.etag);
    await call(
      owner.page,
      "GET",
      base + "/applications/" + "f".repeat(32),
      undefined,
      404,
    );
    await call(owner.page, "POST", target + "/disable", {}, 428);
    await call(owner.page, "POST", target + "/disable", {}, 400, '"authz-1"');
    await call(
      owner.page,
      "POST",
      target + "/disable",
      {},
      412,
      '"app-' + otherTenant + "-" + demo.id + '-1"',
    );
    await call(
      owner.page,
      "POST",
      target + "/disable",
      { scope: "all-users" },
      400,
      detail.etag,
    );
    const protectedDetail = await call(
      owner.page,
      "GET",
      base + "/applications/" + platform.id,
    );
    await call(
      owner.page,
      "POST",
      base + "/applications/" + platform.id + "/disable",
      {},
      409,
      protectedDetail.etag,
    );
    phase = "two-applications-in-different-spaces";
    let appA = await appLogin(owner, "http://app-a.localhost:3001", tenant),
      appB = await appLogin(owner, "http://app-b.localhost:3002", otherTenant);
    assert.equal(await resource(appA), 200);
    assert.equal(await resource(appB), 200);
    phase = "recent-mfa-required";
    execFileSync(
      "python3",
      ["tests/api/fixtures.py", "age-session", me.user_id, me.session_id],
      { cwd: s.root, stdio: "pipe" },
    );
    await call(owner.page, "GET", target);
    const staleMFA = await call(
      owner.page,
      "POST",
      target + "/disable",
      {},
      403,
      detail.etag,
    );
    assert.equal(staleMFA.body.code, "recent_mfa_required");
    me = await s.login(owner, owner.page, tenant, true);
    phase = "concurrent-disable-and-scoped-authority-revocation";
    const results = await Promise.all([
      call(
        owner.page,
        "POST",
        target + "/disable",
        {},
        [200, 412],
        detail.etag,
      ),
      call(
        owner.page,
        "POST",
        target + "/disable",
        {},
        [200, 412],
        detail.etag,
      ),
    ]);
    assert.deepEqual(results.map((r) => r.status).sort(), [200, 412]);
    const disabled = results.find((r) => r.status === 200);
    assert.equal(disabled.body.version, detail.body.version + 1);
    assert.equal(disabled.body.enabled, false);
    assert.equal(await resource(appA), 401);
    assert.equal(await resource(appB), 200);
    assert.equal((await s.bff(owner.page, "/v1/me")).status, 200);
    assert.equal((await s.bff(otherPage, "/v1/me")).status, 200);
    await appB
      .getByRole("button", { name: "续期当前会话", exact: true })
      .click();
    await appB.getByRole("status").filter({ hasText: "会话已续期" }).waitFor();
    const noop = await call(
      owner.page,
      "POST",
      target + "/disable",
      {},
      200,
      disabled.etag,
    );
    assert.equal(noop.body.version, disabled.body.version);
    await call(owner.page, "POST", target + "/enable", {}, 412, detail.etag);
    detail = await call(
      owner.page,
      "POST",
      target + "/enable",
      {},
      200,
      disabled.etag,
    );
    assert.equal(await resource(appA), 401);
    assert.equal(await resource(appB), 200);
    appA = await appLogin(owner, "http://app-a.localhost:3001", tenant);
    assert.equal(await resource(appA), 200);
    phase = "console-owner-controls-client-metadata-and-mobile";
    const p = owner.page;
    await s.nav(p, "应用接入");
    let row = p.getByRole("row").filter({ hasText: "local-demo" });
    await row
      .getByRole("button", { name: "查看登记配置", exact: true })
      .click();
    await p
      .getByRole("dialog")
      .getByText("local-demo-a", { exact: true })
      .waitFor();
    assert(
      (await p.getByRole("dialog").textContent()).includes(
        "app-a.localhost:3001/oidc/callback",
      ),
    );
    await p
      .getByRole("button", { name: "关闭应用对话框", exact: true })
      .click();
    const protectedRow = p
      .getByRole("row")
      .filter({ hasText: "local-platform" });
    assert(
      await protectedRow
        .getByRole("button", { name: "停用应用", exact: true })
        .isDisabled(),
    );
    await p.screenshot({
      path: path.join(s.root, "docs/ui/application-access-desktop.png"),
      fullPage: true,
    });
    await p.setViewportSize({ width: 390, height: 844 });
    assert(
      await p.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    );
    await p.screenshot({
      path: path.join(s.root, "docs/ui/application-access-mobile.png"),
      fullPage: true,
    });
    await p.setViewportSize({ width: 1280, height: 720 });
    await row.getByRole("button", { name: "停用应用", exact: true }).click();
    let dialog = p.getByRole("dialog");
    await dialog.getByLabel("我确认对当前空间执行此操作").check();
    await dialog.getByRole("button", { name: "确认操作", exact: true }).click();
    await p
      .getByRole("status")
      .filter({ hasText: "当前空间的应用已停用" })
      .waitFor();
    assert.equal(await resource(appA), 401);
    assert.equal(await resource(appB), 200);
    await row.getByRole("button", { name: "恢复应用", exact: true }).click();
    dialog = p.getByRole("dialog");
    await dialog.getByLabel("我确认对当前空间执行此操作").check();
    await dialog.getByRole("button", { name: "确认操作", exact: true }).click();
    await p.getByRole("status").filter({ hasText: "应用已恢复" }).waitFor();
    assert.equal(await resource(appA), 401);
    phase = "closed-dialog-discards-real-late-metadata";
    let held = await holdResponse(
      p,
      "**/bff/v1/tenants/" + tenant + "/applications/" + demo.id + "/clients?*",
    );
    try {
      await row
        .getByRole("button", { name: "查看登记配置", exact: true })
        .click();
      await held.arrived;
      await p
        .getByRole("button", { name: "关闭应用对话框", exact: true })
        .click();
      held.release();
      await held.done;
      assert(!held.state.failed);
      assert.equal(await p.getByRole("dialog").count(), 0);
    } finally {
      await held.close();
    }
    phase = "navigation-during-parent-list-load";
    held = await holdResponse(p, "**/bff/v1/tenants/" + tenant + "/members?*");
    try {
      await p
        .getByRole("navigation", { name: "主导航" })
        .getByRole("button", { name: "成员", exact: true })
        .click();
      await held.arrived;
      await s.nav(p, "应用接入");
      await p.getByRole("row").filter({ hasText: "local-demo" }).waitFor();
      held.release();
      await held.done;
      assert(!held.state.failed);
      await p.getByRole("heading", { name: "应用接入", exact: true }).waitFor();
    } finally {
      await held.close();
    }
    phase = "member-navigation-and-application-audit";
    await s.nav(guest.page, "安全会话");
    assert.equal(
      await guest.page
        .getByRole("navigation", { name: "主导航" })
        .getByRole("button", { name: "应用接入", exact: true })
        .count(),
      0,
    );
    const audit = (await s.bff(p, base + "/audit-events?limit=100")).body.items;
    assert.equal(
      audit.filter((a) => a.action === "application.disabled").length,
      2,
    );
    assert.equal(
      audit.filter((a) => a.action === "application.enabled").length,
      2,
    );
    phase = "playground-application-operation";
    await s.nav(p, "API Playground");
    await p
      .getByLabel("选择 API", { exact: true })
      .selectOption("listTenantApplications");
    await p.getByRole("button", { name: "发送读取请求", exact: true }).click();
    await p
      .getByRole("region", { name: "API 响应", exact: true })
      .getByRole("heading", { name: "HTTP 200", exact: true })
      .waitFor();
    assert.deepEqual(s.pageErrors, []);
    assert.deepEqual(s.leaks, []);
    console.log(
      "Application access acceptance passed: " +
        checks +
        " contract responses; owner/member/metadata bounds, client cursor, resource ETags, recent MFA, concurrent disable, space/application isolation, no grant resurrection, UI/mobile, audit and Playground",
    );
  } finally {
    clearInterval(progress);
    await s.close();
  }
})().catch((e) => {
  console.error(
    "Application access failed: phase=" +
      phase +
      ", error=" +
      e.name +
      " (private details withheld)",
  );
  console.error(JSON.stringify(s.diagnostics()));
  process.exitCode = 1;
});
