// Real React/BFF/MySQL/Hydra flow. Never screenshot the one-time secret view.
const s = require("../support/console-fixtures.cjs"),
  n = require("../support/native-fixtures.cjs"),
  assert = require("node:assert/strict"),
  { execFileSync } = require("node:child_process");
let phase = "startup",
  checks = 0;
const secrets = [];
const started = new Date().toISOString();
const progress = setInterval(
  () => console.log("Machine UI acceptance phase: " + phase),
  20000,
);
function norm(url) {
  return url
    .split("?")[0]
    .replace(/\/tenants\/[a-f0-9]{32}/, "/tenants/{tenant}")
    .replace(/\/applications\/[a-f0-9]{32}/, "/applications/{application}")
    .replace(
      /\/service-principals\/[a-f0-9]{32}/,
      "/service-principals/{principal}",
    )
    .replace(/\/credentials\/[a-f0-9]{32}/, "/credentials/{credential}");
}
async function call(p, method, url, body, status = 200, etag) {
  const r = await s.bff(p, url, {
    method,
    body,
    headers: etag ? { "If-Match": etag } : {},
  });
  if (r.status !== status)
    console.error(
      "Unexpected BFF status: " +
        method +
        " " +
        norm(url) +
        " expected=" +
        status +
        " received=" +
        r.status,
    );
  assert.equal(r.status, status);
  execFileSync("python3", ["tests/api/validate_contract.py"], {
    cwd: s.root,
    input: JSON.stringify({
      contract: "console",
      path: "/bff" + norm(url),
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
function privateAbsent(value) {
  const text = JSON.stringify(value);
  for (const secret of secrets) assert(!text.includes(secret));
}
async function confirm(p, label) {
  const d = p.getByRole("dialog");
  await d.getByLabel("我确认执行当前机器身份或凭证操作").check();
  await d.getByRole("button", { name: label, exact: true }).click();
}
async function createKey(p, name) {
  const d = p.getByRole("dialog");
  await d.getByRole("button", { name: "创建机器凭证", exact: true }).click();
  await d.getByLabel("机器凭证名称", { exact: true }).fill(name);
  await d.getByLabel("有效天数", { exact: true }).fill("90");
  await confirm(p, "创建并显示一次性 SK");
}
async function openMachines(p) {
  await s.nav(p, "应用接入");
  const row = p
    .getByRole("row")
    .filter({ has: p.locator("strong").filter({ hasText: /^机器测试应用$/ }) });
  await row.getByRole("button", { name: "机器身份", exact: true }).click();
  await p
    .getByRole("dialog")
    .getByRole("button", { name: "创建机器身份", exact: true })
    .waitFor();
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
      base = "/v1/tenants/" + tenant;
    const invitation = await call(
      p,
      "POST",
      base + "/invitations",
      { email: guest.email },
      201,
    );
    await call(
      guest.page,
      "POST",
      "/v1/invitations/accept",
      { token: invitation.body.token },
      201,
    );
    me = await s.login(owner, p, tenant);
    await s.login(guest, guest.page, tenant);
    const app = (
        await call(
          p,
          "POST",
          base + "/applications",
          { name: "机器测试应用" },
          201,
        )
      ).body,
      appPath = base + "/applications/" + app.id,
      spBase = appPath + "/service-principals";
    phase = "owner-only-create-ui";
    await openMachines(p);
    let d = p.getByRole("dialog");
    await d.getByRole("button", { name: "创建机器身份", exact: true }).click();
    await d.getByLabel("机器身份名称", { exact: true }).fill("批处理机器人");
    await confirm(p, "保存机器身份");
    await d.getByText("暂无机器凭证。", { exact: true }).waitFor();
    const sp = (await call(p, "GET", spBase)).body.items[0],
      spPath = spBase + "/" + sp.id,
      keys = spPath + "/credentials";
    await createKey(p, "第一次凭证");
    const sk = await d.getByTestId("credential-secret").textContent();
    assert.match(sk, /^[A-Za-z0-9_-]{43}$/);
    secrets.push(sk);
    const key = (await call(p, "GET", keys)).body.items[0];
    privateAbsent((await call(p, "GET", keys + "/" + key.id)).body);
    privateAbsent(
      await p.evaluate(() => ({
        local: { ...localStorage },
        session: { ...sessionStorage },
        url: location.href,
      })),
    );
    phase = "one-time-secret-close-and-reopen";
    await d
      .getByRole("button", { name: "我已安全保存 SK", exact: true })
      .click();
    await d.getByRole("heading", { name: "第一次凭证", exact: true }).waitFor();
    assert.equal(await d.getByTestId("credential-secret").count(), 0);
    privateAbsent(await p.locator("body").innerText());
    await d
      .getByRole("button", { name: "关闭机器身份对话框", exact: true })
      .click();
    await openMachines(p);
    d = p.getByRole("dialog");
    await d.getByRole("button", { name: "管理机器凭证", exact: true }).click();
    await d.getByRole("heading", { name: "第一次凭证", exact: true }).waitFor();
    privateAbsent(await d.innerText());
    assert.equal(await d.getByTestId("credential-secret").count(), 0);
    await p.screenshot({
      path: s.root + "/docs/ui/machine-credentials-desktop.png",
      fullPage: true,
    });
    phase = "real-token-and-machine-audit";
    const exchanged = await n.request(
      4464,
      "auth.localhost:4464",
      "POST",
      "/oauth2/token",
      new URLSearchParams({
        grant_type: "client_credentials",
        scope: "machine:invoke",
        audience: key.audience,
      }).toString(),
      {
        "Content-Type": "application/x-www-form-urlencoded",
        Authorization:
          "Basic " + Buffer.from(key.access_key + ":" + sk).toString("base64"),
      },
    );
    assert.equal(exchanged.status, 200);
    const access = exchanged.body.access_token;
    secrets.push(access);
    const resource = "/v1/machine/applications/" + app.id + "/identity";
    assert.equal(
      (
        await n.request(
          8083,
          "api.localhost:8083",
          "GET",
          resource,
          undefined,
          { Authorization: "Bearer " + access },
        )
      ).status,
      200,
    );
    // Exact BFF route whitelist has no machine resource route, even with a User session.
    assert.equal((await s.bff(p, resource)).status, 404);
    phase = "lost-success-reply-no-automatic-create-retry";
    let writes = 0;
    await p.route("**/bff" + keys, async (route) => {
      try {
        if (route.request().method() !== "POST") return route.continue();
        writes++;
        const response = await route.fetch({
          url:
            "http://127.0.0.1:3003" + new URL(route.request().url()).pathname,
          headers: {
            ...route.request().headers(),
            host: "console.localhost:3003",
          },
        });
        assert.equal(response.status(), 201);
        const hidden = await response.json();
        secrets.push(hidden.secret_key);
        await route.fulfill({
          status: 503,
          contentType: "application/problem+json",
          body: JSON.stringify({
            type: "about:blank",
            title: "temporarily_unavailable",
            status: 503,
            code: "temporarily_unavailable",
          }),
        });
      } catch {
        console.error(
          "Machine lost-reply injection failed (private details withheld)",
        );
        await route.abort();
      }
    });
    await createKey(p, "未收妥凭证");
    phase = "lost-reply-error-display";
    await d.getByRole("alert").waitFor();
    assert.equal(await d.getByTestId("credential-secret").count(), 0);
    assert(
      await d
        .getByRole("button", { name: "创建并显示一次性 SK", exact: true })
        .isDisabled(),
    );
    await p.waitForTimeout(300);
    assert.equal(writes, 1);
    await p.unroute("**/bff" + keys);
    await d.getByRole("button", { name: "返回列表核对", exact: true }).click();
    const lost = d.locator("article").filter({
      has: p.getByRole("heading", { name: "未收妥凭证", exact: true }),
    });
    phase = "lost-reply-return-list";
    await lost.waitFor();
    privateAbsent(await d.innerText());
    await lost
      .getByRole("button", { name: "撤销机器凭证", exact: true })
      .click();
    phase = "lost-reply-revoke-confirmation";
    await confirm(p, "确认机器操作");
    await d.getByRole("status").filter({ hasText: "凭证已撤销" }).waitFor();
    phase = "lost-reply-cleanup-entry";
    await lost
      .getByRole("button", { name: "清理机器协议登记", exact: true })
      .click();
    await confirm(p, "确认机器操作");
    await d.getByRole("status").filter({ hasText: "本次协议清理" }).waitFor();
    phase = "mobile-layout-and-pause-restore";
    await p.setViewportSize({ width: 390, height: 844 });
    await d.getByRole("heading", { name: "第一次凭证", exact: true }).waitFor();
    assert(
      await p.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ),
    );
    privateAbsent(await d.innerText());
    await p.screenshot({
      path: s.root + "/docs/ui/machine-credentials-mobile.png",
      fullPage: true,
    });
    await d.getByRole("button", { name: "停用机器身份", exact: true }).click();
    await confirm(p, "确认机器操作");
    await d
      .getByRole("button", { name: "恢复机器身份", exact: true })
      .waitFor();
    assert.equal(
      (
        await n.request(
          8083,
          "api.localhost:8083",
          "GET",
          resource,
          undefined,
          { Authorization: "Bearer " + access },
        )
      ).status,
      401,
    );
    await d.getByRole("button", { name: "恢复机器身份", exact: true }).click();
    await confirm(p, "确认机器操作");
    await d
      .getByRole("button", { name: "停用机器身份", exact: true })
      .waitFor();
    assert.equal(
      (
        await n.request(
          8083,
          "api.localhost:8083",
          "GET",
          resource,
          undefined,
          { Authorization: "Bearer " + access },
        )
      ).status,
      401,
    );
    await d
      .getByRole("button", { name: "关闭机器身份对话框", exact: true })
      .click();
    await p.setViewportSize({ width: 1365, height: 900 });
    await s.nav(p, "审计");
    const audit = p.getByRole("region", { name: "机器签发审计" });
    await audit.getByText(sp.id, { exact: true }).first().waitFor();
    privateAbsent(await audit.innerText());
    privateAbsent((await call(p, "GET", base + "/machine-events")).body);
    phase = "member-cross-tenant-and-preconditions";
    for (const [method, url, body] of [
      ["GET", spBase],
      ["GET", spPath],
      ["GET", keys],
      ["POST", spBase, { name: "Denied" }],
      ["POST", keys, { name: "Denied", expires_in_days: 1 }],
    ])
      await call(guest.page, method, url, body, 403);
    await call(guest.page, "GET", base + "/machine-events", undefined, 403);
    const other = (await s.bff(p, "/v1/tenants")).body.items.find(
        (t) => t.id !== tenant,
      ).id,
      otherPage = await owner.context.newPage();
    await s.login(owner, otherPage, other);
    await call(
      otherPage,
      "GET",
      "/v1/tenants/" +
        other +
        "/applications/" +
        app.id +
        "/service-principals",
      undefined,
      404,
    );
    const detail = await call(p, "GET", keys + "/" + key.id);
    await call(p, "POST", keys + "/" + key.id + "/revoke", {}, 428);
    await call(
      p,
      "POST",
      keys + "/" + key.id + "/revoke",
      {},
      412,
      '"cred-' + other + "-" + key.id + "-" + detail.body.version + '"',
    );
    phase = "recent-mfa-management-boundary";
    execFileSync(
      "python3",
      ["tests/api/fixtures.py", "age-session", me.user_id, me.session_id],
      { cwd: s.root, stdio: "pipe" },
    );
    for (const [url, body, tag] of [
      [spBase, { name: "Aged" }, null],
      [keys, { name: "Aged", expires_in_days: 1 }, null],
      [spPath + "/disable", {}, (await call(p, "GET", spPath)).etag],
      [keys + "/" + key.id + "/revoke", {}, detail.etag],
      [keys + "/" + key.id + "/cleanup", {}, detail.etag],
    ]) {
      const r = await call(p, "POST", url, body, 403, tag);
      assert.equal(r.body.code, "recent_mfa_required");
    }
    await call(p, "GET", keys);
    await s.login(owner, p, tenant, true);
    await openMachines(p);
    d = p.getByRole("dialog");
    await d.getByRole("button", { name: "管理机器凭证", exact: true }).click();
    const first = d.locator("article").filter({
      has: p.getByRole("heading", { name: "第一次凭证", exact: true }),
    });
    await first
      .getByRole("button", { name: "撤销机器凭证", exact: true })
      .click();
    await confirm(p, "确认机器操作");
    await d.getByRole("status").filter({ hasText: "凭证已撤销" }).waitFor();
    privateAbsent(await d.innerText());
    assert.equal(s.pageErrors.length, 0);
    assert.equal(s.leaks.length, 0);
    phase = "private-material-public-file-and-runtime-log-exclusion";
    const privacy = require("../support/private-material.cjs")(
      s.root,
      secrets,
      started,
    );
    console.log(
      "Machine UI private-material exclusion passed: " +
        privacy.files +
        " candidate files and runtime logs",
    );
    console.log(
      "Machine UI acceptance passed: " +
        checks +
        " contract responses; real owner/member/MFA, one-time display and close, no secret browser storage, lost creation reply/revocation/cleanup, mobile, machine audit and realm isolation",
    );
  } finally {
    clearInterval(progress);
    await s.close();
  }
})().catch((e) => {
  console.error(
    "Machine UI failed: phase=" +
      phase +
      ", error=" +
      e.name +
      " (private details withheld)",
  );
  console.error(JSON.stringify(s.diagnostics()));
  process.exitCode = 1;
});
