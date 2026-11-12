// Real captured changes and owner UI; each source response is counted once.
const s = require("../support/console-fixtures.cjs"),
  assert = require("node:assert/strict"),
  { execFileSync } = require("node:child_process"),
  path = require("node:path");
let debugPage;
let phase = "startup",
  checks = 0,
  base,
  hold = null,
  fault = null;
const errors = [],
  secrets = [],
  started = new Date().toISOString(),
  progress = setInterval(
    () => console.log("Access history UI phase: " + phase),
    20000,
  );
function normalized(url) {
  return url
    .split("?")[0]
    .replace(/\/tenants\/[a-f0-9]{32}/, "/tenants/{tenant}")
    .replace(/\/applications\/[a-f0-9]{32}/, "/applications/{application}")
    .replace(/\/members\/[a-f0-9]{32}/, "/members/{member}")
    .replace(/\/roles\/[a-f0-9]{32}/, "/roles/{role}");
}
function validate(url, method, r, body) {
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
  if (typeof r.body?.token === "string") secrets.push(r.body.token);
}
async function call(p, method, url, body, etag, status = 200) {
  const r = await s.bff(p, url, {
    method,
    body,
    headers: etag ? { "If-Match": etag } : {},
  });
  assert.equal(r.status, status);
  if (!url.startsWith(base + "/")) validate(url, method, r, body);
  return r;
}
async function install(p) {
  await p.route("**/bff" + base + "/**", async (route) => {
    const req = route.request(),
      u = new URL(req.url()),
      url = u.pathname.slice(4) + u.search,
      method = req.method();
    try {
      const response = await route.fetch({
        url: "http://127.0.0.1:3003" + u.pathname + u.search,
        headers: {
          ...(await req.allHeaders()),
          host: "console.localhost:3003",
        },
        maxRedirects: 0,
        timeout: 5000,
      });
      let body;
      try {
        body = await response.json();
      } catch {}
      const headers = response.headers();
      validate(
        url,
        method,
        {
          status: response.status(),
          body,
          contentType: headers["content-type"],
          requestID: headers["x-request-id"],
        },
        method === "GET" ? undefined : req.postDataJSON(),
      );
      if (hold && !hold.used && hold.match(url, method)) {
        const h = hold;
        h.used = true;
        h.resolve();
        await h.gate;
      }
      if (fault && fault.match(url, method)) {
        const f = fault;
        fault = null;
        if (f.kind === "invalid")
          await route.fulfill({
            response,
            json: { ...body, secret_key: "private-access-history-marker" },
          });
        else
          await route.fulfill({
            status: 503,
            contentType: "application/problem+json",
            body: JSON.stringify({ code: "temporarily_unavailable" }),
          });
        return;
      }
      await route.fulfill({ response }).catch(() => {});
    } catch {
      errors.push("history source forwarding failed");
      if (hold && !hold.used) {
        hold.used = true;
        hold.resolve();
      }
      await route.abort().catch(() => {});
    }
  });
}
function arm(match) {
  let resolve, release;
  const arrived = new Promise((r) => (resolve = r)),
    gate = new Promise((r) => (release = r));
  hold = { match, resolve, release, gate, used: false };
  return { ...hold, arrived };
}
const dialog = (p) =>
  p.getByRole("dialog", { name: "应用访问历史", exact: true });
async function ready(d) {
  await d
    .getByRole("status")
    .filter({ hasText: "正在读取应用访问历史…" })
    .waitFor({ state: "hidden" });
}
async function open(p, name, empty = false) {
  await p
    .getByRole("row")
    .filter({
      has: p
        .locator("strong")
        .filter({ hasText: new RegExp("^" + name + "$") }),
    })
    .getByRole("button", { name: "访问历史", exact: true })
    .click();
  const d = dialog(p);
  await d.waitFor();
  if (empty)
    await d.getByRole("status").filter({ hasText: "没有已捕获" }).waitFor();
  else await d.getByRole("article").first().waitFor();
  await ready(d);
  return d;
}
async function close(p) {
  await dialog(p).getByRole("button", { name: "关闭", exact: true }).click();
  await dialog(p).waitFor({ state: "hidden" });
}
(async () => {
  try {
    await s.start();
    phase = "register-owner";
    const owner = await s.register(true);
    phase = "register-guest";
    const guest = await s.register();
    phase = "login-owner";
    let me = await s.login(owner);
    const p = owner.page,
      tenant = me.tenant_id;
    base = "/v1/tenants/" + tenant;
    debugPage = p;
    await install(p);
    const invite = (
      await call(
        p,
        "POST",
        base + "/invitations",
        { email: guest.email },
        undefined,
        201,
      )
    ).body;
    await s.login(guest);
    const member = (
      await call(
        guest.page,
        "POST",
        "/v1/invitations/accept",
        { token: invite.token },
        undefined,
        201,
      )
    ).body.membership_id;
    await s.login(owner, p, tenant);
    const app = (
      await call(
        p,
        "POST",
        base + "/applications",
        { name: "历史演示工作台" },
        undefined,
        201,
      )
    ).body;
    const other = (
      await call(
        p,
        "POST",
        base + "/applications",
        { name: "空历史工作台" },
        undefined,
        201,
      )
    ).body;
    const policy = base + "/applications/" + app.id + "/user-access",
      history = policy + "/history";
    phase = "legacy-empty-history";
    await s.nav(p, "应用接入");
    let d = await open(p, app.name, true);
    assert.equal(await d.getByRole("article").count(), 0);
    await close(p);
    phase = "genuine-captured-mode-member-changes";
    let tag = (await call(p, "GET", policy)).etag;
    for (let n = 0; n < 21; n++) {
      const r = await call(
        p,
        "PUT",
        policy,
        { mode: n % 2 ? "all_members" : "selected_members" },
        tag,
      );
      tag = r.etag;
    }
    tag = (await call(p, "PUT", policy + "/members/" + member, {}, tag)).etag;
    await call(p, "DELETE", policy + "/members/" + member, {}, tag);
    d = await open(p, app.name);
    assert.equal(await d.getByRole("article").count(), 20);
    await d
      .getByRole("button", { name: "下一批访问变化", exact: true })
      .click();
    await ready(d);
    assert.equal(await d.getByRole("article").count(), 3);
    assert.equal(await d.getByText(member, { exact: true }).count(), 2);
    await d
      .getByRole("heading", { name: "授予成员访问", exact: true })
      .waitFor();
    await d
      .getByRole("heading", { name: "撤销成员访问", exact: true })
      .waitFor();
    phase = "safe-desktop-mobile-capture";
    await p.setViewportSize({ width: 1365, height: 1000 });
    await d.evaluate((e) => (e.scrollTop = 0));
    await p.screenshot({
      path: path.join(s.root, "docs/ui/application-access-history-desktop.png"),
    });
    await p.setViewportSize({ width: 390, height: 844 });
    assert.equal(
      await p.evaluate(
        () => document.documentElement.scrollWidth > innerWidth + 1,
      ),
      false,
    );
    await d.evaluate((e) => (e.scrollTop = 0));
    await p.screenshot({
      path: path.join(s.root, "docs/ui/application-access-history-mobile.png"),
    });
    await p.setViewportSize({ width: 1365, height: 1000 });
    await d.getByRole("button", { name: "重新读取", exact: true }).click();
    await ready(d);
    assert.equal(await d.getByRole("article").count(), 3);
    await d.getByRole("button", { name: "返回首批", exact: true }).click();
    await ready(d);
    assert.equal(await d.getByRole("article").count(), 20);
    phase = "malformed-private-and-dependency-clearing";
    for (const kind of ["invalid", "unavailable"]) {
      fault = {
        kind,
        match: (url, m) => m === "GET" && url.startsWith(history + "?"),
      };
      await d.getByRole("button", { name: "重新读取", exact: true }).click();
      await d.getByRole("alert").waitFor();
      assert.equal(await d.getByRole("article").count(), 0);
      assert.equal(
        await d
          .getByRole("button", { name: "下一批访问变化", exact: true })
          .count(),
        0,
      );
      assert(!(await d.innerText()).includes("private-access-history-marker"));
      await close(p);
      d = await open(p, app.name);
    }
    phase = "cancel-reopen-and-resource-late-response";
    let h = arm((url, m) => m === "GET" && url.startsWith(history + "?"));
    await d.getByRole("button", { name: "重新读取", exact: true }).click();
    await h.arrived;
    assert.equal(await d.getByRole("article").count(), 0);
    await close(p);
    d = await open(p, other.name, true);
    h.release();
    hold = null;
    await p.waitForTimeout(100);
    assert.equal(await d.getByRole("article").count(), 0);
    await close(p);
    d = await open(p, app.name);
    await close(p);
    phase = "workspace-isolation";
    const original = p.url(),
      secondary = (await call(p, "GET", "/v1/tenants")).body.items.find(
        (v) => v.id !== tenant,
      ).id,
      otherPage = await owner.context.newPage();
    await s.login(owner, otherPage, secondary);
    const target = new URL(otherPage.url()).hash;
    h = arm((url, m) => m === "GET" && url.startsWith(history + "?"));
    await p
      .getByRole("row")
      .filter({
        has: p
          .locator("strong")
          .filter({ hasText: new RegExp("^" + app.name + "$") }),
      })
      .getByRole("button", { name: "访问历史", exact: true })
      .click();
    await h.arrived;
    phase = "workspace-apply-target";
    await p.evaluate((hash) => {
      location.hash = hash;
    }, target);
    await p.getByRole("heading", { name: "空间概览", exact: true }).waitFor();
    h.release();
    hold = null;
    assert.equal(await dialog(p).count(), 0);
    phase = "workspace-return-original";
    await p.goto(original);
    await p.getByRole("heading", { name: "空间概览", exact: true }).waitFor();
    await s.nav(p, "应用接入");
    phase = "owner-with-directory-deny-and-live-authorization-loss";
    const version = async () =>
      (await call(p, "GET", base + "/permissions")).etag;
    const deny = (
      await call(
        p,
        "POST",
        base + "/roles",
        {
          code: "history_owner_privacy",
          policies: [
            { permission: "members.read", effect: "deny" },
            { permission: "audit.read", effect: "deny" },
          ],
        },
        await version(),
        201,
      )
    ).body.id;
    await s.login(owner, p, tenant);
    me = (await call(p, "GET", "/v1/me")).body;
    await call(
      p,
      "PUT",
      base + "/members/" + me.membership_id + "/roles/" + deny,
      {},
      await version(),
      204,
    );
    await s.login(owner, p, tenant);
    await call(p, "GET", base + "/members", undefined, undefined, 403);
    await s.nav(p, "应用接入");
    d = await open(p, app.name);
    assert.equal(await d.getByRole("article").count(), 20);
    assert(!(await d.innerText()).includes("example.invalid"));
    await call(
      p,
      "POST",
      base + "/roles",
      { code: "history_live_revoke", policies: [] },
      await version(),
      201,
    );
    await d.getByRole("button", { name: "重新读取", exact: true }).click();
    await p
      .getByRole("heading", { name: "请重新确认访问权限", exact: true })
      .waitFor();
    assert.equal(await dialog(p).count(), 0);
    assert.deepEqual(errors, []);
    assert.deepEqual(s.pageErrors, []);
    assert.deepEqual(s.leaks, []);
    const privacy = require("../support/private-material.cjs")(
      s.root,
      [
        ...secrets,
        ...s.fixtures.flatMap((f) => [f.password, f.totpSecret, ...f.codes]),
      ],
      started,
    );
    console.log(
      "Access history UI passed: " +
        checks +
        " unique real BFF contract responses; 23 genuine mode/grant/revoke events, bounded pagination, desktop/mobile, strict clearing, cancellation/resource/workspace isolation and owner directory denial/live authority; private exclusion in " +
        privacy.files +
        " files/runtime logs",
    );
  } catch (e) {
    console.error(
      "Access history fixture stage: " +
        s.diagnostics().step +
        ", rateLimited=" +
        s.diagnostics().rejected.some((r) => r.status === 429),
    );
    if (debugPage && !debugPage.isClosed()) {
      console.error(
        "Access history safe diagnostic: " +
          JSON.stringify({
            overview: await debugPage
              .getByRole("heading", { name: "空间概览", exact: true })
              .count(),
            reauthorization: await debugPage
              .getByRole("heading", { name: "请重新确认访问权限", exact: true })
              .count(),
            dialog: await dialog(debugPage).count(),
            articles: await dialog(debugPage).getByRole("article").count(),
          }),
      );
    }

    throw e;
  } finally {
    if (hold) hold.release();
    clearInterval(progress);
    await s.close();
  }
})().catch((e) => {
  console.error(
    "Access history UI failed: phase=" +
      phase +
      ", error=" +
      e.name +
      " (private details withheld)",
  );
  process.exitCode = 1;
});
