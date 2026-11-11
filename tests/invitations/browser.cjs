// Actual console invitation revisions and one-time proof UI, including lost replies.
const s = require("../support/console-fixtures.cjs"),
  assert = require("node:assert/strict"),
  crypto = require("node:crypto"),
  { execFileSync } = require("node:child_process"),
  path = require("node:path");
let pageForDiagnostics;
let phase = "startup",
  checks = 0;
const secrets = [],
  routeErrors = [],
  writes = new Map(),
  started = new Date().toISOString();
const progress = setInterval(
  () => console.log("Invitation UI phase: " + phase),
  20000,
);
const title = "更新一次性邀请凭证",
  confirm = "我确认使旧凭证失效，并安全转交新凭证";
function normalized(url) {
  return url
    .split("?")[0]
    .replace(/\/tenants\/[a-f0-9]{32}/, "/tenants/{tenant}")
    .replace(/\/invitations\/[a-f0-9]{32}/, "/invitations/{invitation}");
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
}
async function call(p, method, url, body, status = 200) {
  const r = await s.bff(p, url, { method, body });
  assert.equal(r.status, status);
  validate(url, method, r, body);
  if (typeof r.body?.token === "string") secrets.push(r.body.token);
  return r;
}
async function forward(route) {
  const u = new URL(route.request().url());
  assert.equal(u.origin, "http://console.localhost:3003");
  return route.fetch({
    url: "http://127.0.0.1:3003" + u.pathname + u.search,
    headers: {
      ...(await route.request().allHeaders()),
      host: "console.localhost:3003",
    },
    maxRedirects: 0,
    timeout: 5000,
  });
}
async function record(route, response) {
  const url = new URL(route.request().url()).pathname,
    method = route.request().method(),
    body = await response.json(),
    headers = response.headers();
  validate(
    url.slice(4),
    method,
    {
      status: response.status(),
      body,
      contentType: headers["content-type"],
      requestID: headers["x-request-id"],
    },
    method === "POST" ? route.request().postDataJSON() : undefined,
  );
  if (method === "POST" && url.endsWith("/reissue"))
    writes.set(url, (writes.get(url) || 0) + 1);
  if (typeof body.token === "string") secrets.push(body.token);
  return body;
}
function absent(value) {
  const text = typeof value === "string" ? value : JSON.stringify(value);
  for (const secret of secrets) assert(!text.includes(secret));
}
function dialog(p) {
  return p.getByRole("dialog", { name: title, exact: true });
}
async function open(p, email, wait = true) {
  phase = "open-navigate-overview";
  await s.nav(p, "空间概览");
  phase = "open-navigate-invitations";
  await s.nav(p, "邀请");
  const row = p.getByRole("row").filter({ hasText: email });
  phase = "open-row-action";
  await row.getByRole("button", { name: "更新邀请凭证", exact: true }).click();
  const d = dialog(p);
  phase = "open-dialog";
  await d.waitFor();
  phase = "open-current-metadata";
  if (wait) await d.getByText(/当前版本/).waitFor();
  return d;
}
async function issue(d) {
  assert(
    await d
      .getByRole("button", { name: "确认更新一次", exact: true })
      .isDisabled(),
  );
  phase = "issue-confirmation-checkbox";
  await d.getByLabel(confirm, { exact: true }).check();
  phase = "issue-submit";
  await d.getByRole("button", { name: "确认更新一次", exact: true }).click();
  phase = "issue-once-only-result";
  await d.getByTestId("reissued-invitation-secret").waitFor();
  const value = (
    await d.getByTestId("reissued-invitation-secret").textContent()
  ).trim();
  assert(/^[A-Za-z0-9_-]{43}$/.test(value));
  secrets.push(value);
  return value;
}
async function hold(p, url) {
  let seen, release, finish;
  const arrived = new Promise((r) => (seen = r)),
    gate = new Promise((r) => (release = r)),
    done = new Promise((r) => (finish = r));
  const pattern = "**/bff" + url;
  const handler = async (route) => {
    try {
      const response = await forward(route),
        body = await record(route, response);
      assert.equal(response.status(), 200);
      seen(body);
      await gate;
      await route.fulfill({ response }).catch(() => {});
    } catch {
      routeErrors.push("held response failed");
      seen(null);
      await route.abort().catch(() => {});
    } finally {
      finish();
    }
  };
  await p.route(pattern, handler);
  return {
    arrived,
    release: async () => {
      release();
      await done;
      await p.unroute(pattern, handler);
    },
  };
}
(async () => {
  try {
    await s.start();
    const owner = await s.register(true),
      guest = await s.register();
    let me = await s.login(owner);
    await s.login(guest);
    const p = owner.page,
      tenant = me.tenant_id,
      base = "/v1/tenants/" + tenant,
      invitations = base + "/invitations";
    pageForDiagnostics = p;
    await p.route("**/bff/v1/tenants/*/invitations/**", async (route) => {
      let response;
      try {
        response = await forward(route);
      } catch {
        await route.abort().catch(() => {});
        return;
      }
      try {
        await record(route, response);
      } catch {
        routeErrors.push("response contract failed");
      }
      await route.fulfill({ response }).catch(() => {});
    });
    phase = "real-once-only-replacement-and-close";
    const original = (
      await call(p, "POST", invitations, { email: guest.email }, 201)
    ).body;
    const resource = invitations + "/" + original.id;
    let d = await open(p, guest.email);
    const first = await issue(d);
    assert.notEqual(first, original.token);
    assert.equal(writes.get("/bff" + resource + "/reissue"), 1);
    await d.getByRole("button", { name: "我已安全保存", exact: true }).click();
    assert.equal(await dialog(p).count(), 0);
    absent(await p.locator("body").innerText());
    d = await open(p, guest.email);
    assert((await d.innerText()).includes("当前版本 2"));
    assert.equal(await d.getByTestId("reissued-invitation-secret").count(), 0);
    const second = await issue(d);
    assert.notEqual(second, first);
    await d.getByRole("button", { name: "我已安全保存", exact: true }).click();
    await call(
      guest.page,
      "POST",
      "/v1/invitations/accept",
      { token: original.token },
      400,
    );
    await call(
      guest.page,
      "POST",
      "/v1/invitations/accept",
      { token: first },
      400,
    );
    await call(
      guest.page,
      "POST",
      "/v1/invitations/accept",
      { token: second },
      201,
    );
    me = await s.login(owner, p, tenant);
    await s.nav(p, "邀请");
    const closed = p.getByRole("row").filter({ hasText: guest.email });
    await closed.getByText("已关闭", { exact: true }).waitFor();
    assert.equal(
      await closed
        .getByRole("button", { name: "更新邀请凭证", exact: true })
        .count(),
      0,
    );
    phase = "expired-proof-and-committed-lost-response-no-replay";
    const expiredEmail =
      "expired-ui-" +
      crypto.randomBytes(6).toString("hex") +
      "@example.invalid";
    const expired = (
        await call(p, "POST", invitations, { email: expiredEmail }, 201)
      ).body,
      expiredPath = invitations + "/" + expired.id;
    execFileSync(
      "python3",
      ["tests/invitations/fixture.py", "expire", me.user_id, expired.id],
      { cwd: s.root, stdio: "pipe" },
    );
    d = await open(p, expiredEmail);
    assert((await d.innerText()).includes("已过期，可更新凭证"));
    let lostProof, reached;
    const received = new Promise((r) => (reached = r)),
      pattern = "**/bff" + expiredPath + "/reissue";
    const lose = async (route) => {
      try {
        const response = await forward(route),
          body = await record(route, response);
        assert.equal(response.status(), 200);
        lostProof = body.token;
        reached();
        await route.abort("failed").catch(() => {});
      } catch {
        routeErrors.push("lost-response fixture failed");
        reached();
        await route.abort().catch(() => {});
      }
    };
    await p.route(pattern, lose);
    await d.getByLabel(confirm, { exact: true }).check();
    await d
      .getByRole("button", { name: "确认更新一次", exact: true })
      .dblclick();
    await received;
    await d.getByRole("alert").waitFor();
    assert.equal(writes.get("/bff" + expiredPath + "/reissue"), 1);
    assert.equal(await d.getByTestId("reissued-invitation-secret").count(), 0);
    assert((await d.innerText()).includes("不会自动重发"));
    assert.equal(lostProof?.length, 43);
    await p.unroute(pattern, lose);
    await d.getByRole("button", { name: "读取当前邀请", exact: true }).click();
    await d.getByText(/当前版本 2/).waitFor();
    absent(await d.innerText());
    assert.equal(
      await d.getByLabel(confirm, { exact: true }).isChecked(),
      false,
    );
    phase = "safe-metadata-desktop-mobile-screenshots";
    assert.equal(await d.getByTestId("reissued-invitation-secret").count(), 0);
    await d.screenshot({
      path: path.join(s.root, "docs/ui/invitation-reissue-desktop.png"),
    });
    await p.setViewportSize({ width: 390, height: 844 });
    assert.equal(
      await p.evaluate(() => document.documentElement.scrollWidth > innerWidth),
      false,
    );
    const box = await d.boundingBox();
    assert(box.x >= 0 && box.x + box.width <= 391);
    await d.screenshot({
      path: path.join(s.root, "docs/ui/invitation-reissue-mobile.png"),
    });
    await p.setViewportSize({ width: 1365, height: 900 });
    await d.getByRole("button", { name: "关闭邀请更新", exact: true }).click();
    phase = "closed-dialog-drops-late-real-metadata";
    let held = await hold(p, expiredPath);
    d = await open(p, expiredEmail, false);
    await held.arrived;
    await d.getByRole("button", { name: "关闭邀请更新", exact: true }).click();
    await held.release();
    await p.waitForTimeout(100);
    assert.equal(await dialog(p).count(), 0);
    absent(await p.locator("body").innerText());
    d = await open(p, expiredEmail);
    await d.getByText(/当前版本 2/).waitFor();
    phase = "closed-dialog-drops-committed-late-proof";
    held = await hold(p, expiredPath + "/reissue");
    await d.getByLabel(confirm, { exact: true }).check();
    await d.getByRole("button", { name: "确认更新一次", exact: true }).click();
    const closedWrite = await held.arrived;
    assert.equal(closedWrite?.revision, 3);
    await d.getByRole("button", { name: "关闭邀请更新", exact: true }).click();
    await held.release();
    await p.waitForTimeout(100);
    assert.equal(await dialog(p).count(), 0);
    assert.equal(await p.getByTestId("reissued-invitation-secret").count(), 0);
    absent(await p.locator("body").innerText());
    phase = "workspace-change-drops-committed-late-proof";
    const spaces = (await call(p, "GET", "/v1/tenants")).body.items,
      other = spaces.find((v) => v.id !== tenant).id;
    const otherPage = await owner.context.newPage();
    await s.login(owner, otherPage, other);
    const hash = new URL(otherPage.url()).hash;
    d = await open(p, expiredEmail);
    held = await hold(p, expiredPath + "/reissue");
    await d.getByLabel(confirm, { exact: true }).check();
    await d.getByRole("button", { name: "确认更新一次", exact: true }).click();
    const late = await held.arrived;
    assert.equal(late?.revision, 4);
    await p.evaluate((hash) => {
      location.hash = hash;
    }, hash);
    await p.getByRole("heading", { name: "空间概览", exact: true }).waitFor();
    await held.release();
    await p.waitForTimeout(100);
    assert.equal(await dialog(p).count(), 0);
    assert.equal(await p.getByTestId("reissued-invitation-secret").count(), 0);
    assert.equal((await call(p, "GET", "/v1/me")).body.tenant_id, other);
    assert.equal(writes.get("/bff" + expiredPath + "/reissue"), 3);
    absent(await p.locator("body").innerText());
    phase = "recent-mfa-remains-required-for-ui-update";
    me = await s.login(owner, p, tenant);
    d = await open(p, expiredEmail);
    await d.getByText(/当前版本 4/).waitFor();
    execFileSync(
      "python3",
      ["tests/api/fixtures.py", "age-session", me.user_id, me.session_id],
      { cwd: s.root, stdio: "pipe" },
    );
    await d.getByLabel(confirm, { exact: true }).check();
    await d.getByRole("button", { name: "确认更新一次", exact: true }).click();
    await d.getByRole("alert").filter({ hasText: "请重新验证身份" }).waitFor();
    assert.equal(await d.getByTestId("reissued-invitation-secret").count(), 0);
    await d.getByRole("button", { name: "关闭邀请更新", exact: true }).click();
    await p.getByRole("link", { name: "重新验证身份", exact: true }).waitFor();
    absent(await p.locator("body").innerText());
    assert.equal(
      await p.evaluate(() => localStorage.length + sessionStorage.length),
      0,
    );
    assert.deepEqual(routeErrors, []);
    assert.deepEqual(s.pageErrors, []);
    assert.deepEqual(s.leaks, []);
    const privacy = require("../support/private-material.cjs")(
      s.root,
      secrets,
      started,
    );
    console.log(
      "Invitation UI passed: " +
        checks +
        " contract responses; real proof handoff/acceptance, close/reopen, expired invitation, committed response loss/no duplicate replay, late metadata/proof namespace safety, recent MFA, desktop/mobile and private material exclusion in " +
        privacy.files +
        " files/runtime logs",
    );
  } catch (error) {
    if (pageForDiagnostics) {
      console.error(
        JSON.stringify({
          safeInvitationUI: {
            dialogCount: await dialog(pageForDiagnostics).count(),
            alertCount: await pageForDiagnostics.getByRole("alert").count(),
            invalidData: (
              await pageForDiagnostics.locator("body").innerText()
            ).includes("邀请数据不正确"),
            routeErrors: routeErrors.length,
            pageErrors: s.pageErrors.length,
          },
        }),
      );
    }
    throw error;
  } finally {
    clearInterval(progress);
    await s.close();
  }
})().catch((error) => {
  console.error(
    "Invitation UI failed: phase=" +
      phase +
      ", error=" +
      error.name +
      " (private details withheld)",
  );
  console.error(JSON.stringify(s.diagnostics()));
  process.exitCode = 1;
});
