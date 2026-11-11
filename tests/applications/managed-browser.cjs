// Real owner/member login, BFF -> public API -> MySQL, and real owned Hydra
// profile-drift/reconciliation. Secrets and raw browser exceptions stay private.
const s = require("../support/console-fixtures.cjs"),
  assert = require("node:assert/strict"),
  { execFileSync } = require("node:child_process"),
  path = require("node:path");
let phase = "startup",
  checks = 0;
const progress = setInterval(
  () => console.log("Managed client acceptance phase: " + phase),
  20000,
);
function normalized(url) {
  return url
    .split("?")[0]
    .replace(/\/tenants\/[a-f0-9]{32}/, "/tenants/{tenant}")
    .replace(/\/applications\/[a-f0-9]{32}/, "/applications/{application}")
    .replace(/\/clients\/[a-f0-9]{32}/, "/clients/{client}");
}
async function call(p, method, url, body, status = 200, etag) {
  const r = await s.bff(p, url, {
    method,
    body,
    headers: etag ? { "If-Match": etag } : {},
  });
  if (r.status !== status)
    console.error(
      "Unexpected status: " +
        method +
        " " +
        normalized(url) +
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
async function creationConfirm(p) {
  const d = p.getByRole("dialog");
  await d.getByLabel("我确认在当前空间创建此登记").check();
  await d.getByRole("button", { name: "保存登记", exact: true }).click();
}
async function lifecycleConfirm(p, button) {
  const d = p.getByRole("dialog");
  await d.getByLabel("我已核对配置并确认本次登记操作").check();
  await d.getByRole("button", { name: button, exact: true }).click();
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
      apps = base + "/applications";
    const invitation = await s.bff(p, base + "/invitations", {
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
    await s.login(owner, p, tenant);
    await s.login(guest, guest.page, tenant);
    phase = "create-owned-application-ui";
    await s.nav(p, "应用接入");
    await p.getByRole("button", { name: "创建应用", exact: true }).click();
    await p.getByLabel("应用名称", { exact: true }).fill("本地集成应用");
    await creationConfirm(p);
    await p.getByRole("status").filter({ hasText: "应用已创建" }).waitFor();
    const application = (await call(p, "GET", apps)).body.items.find(
      (a) => a.name === "本地集成应用",
    );
    assert(application && application.managed);
    const app = apps + "/" + application.id;
    const other = (await s.bff(p, "/v1/tenants")).body.items.find(
        (t) => t.id !== tenant,
      ).id,
      otherPage = await owner.context.newPage();
    await s.login(owner, otherPage, other);
    const builtin = (await s.bff(p, apps)).body.items.find((a) => a.protected);
    phase = "public-client-registration-pending-and-redirect-validation";
    for (const redirect of [
      "http://outside.example.invalid/callback",
      "https://*.example.invalid/callback",
      "https://app.example.invalid/callback#token",
      "http://localhost:9876/callback",
      "http://127.0.0.1:80/callback",
    ])
      await call(
        p,
        "POST",
        app + "/clients",
        { name: "Invalid", redirect_uri: redirect },
        400,
      );
    await call(
      p,
      "POST",
      apps + "/" + builtin.id + "/clients",
      {
        name: "Shared takeover",
        redirect_uri: "http://127.0.0.1:9876/callback",
      },
      409,
    );
    let row = p.getByRole("row").filter({
      has: p.locator("strong").filter({ hasText: /^本地集成应用$/ }),
    });
    await row
      .getByRole("button", { name: "登记公开客户端", exact: true })
      .click();
    await p.getByLabel("客户端名称", { exact: true }).fill("本地桌面客户端");
    await p
      .getByLabel("精确回调 URI", { exact: true })
      .fill("http://127.0.0.1:9876/callback");
    await creationConfirm(p);
    await p
      .getByRole("dialog")
      .getByText("待完成登记", { exact: true })
      .waitFor();
    const entries = (await call(p, "GET", app + "/clients")).body.items;
    assert.equal(entries.length, 1);
    assert(
      entries[0].managed &&
        !entries[0].active &&
        entries[0].state === "pending",
    );
    const client = entries[0].client_id,
      target = app + "/clients/" + client;
    let detail = await call(p, "GET", target);
    assert.equal(detail.body.state, "pending");
    phase = "profile-drift-never-adopted-or-overwritten";
    execFileSync(
      "python3",
      ["tests/applications/managed_fixture.py", "create-drift", client],
      { cwd: s.root, stdio: "pipe" },
    );
    await lifecycleConfirm(p, "核对并完成登记");
    await p
      .getByRole("dialog")
      .getByRole("alert")
      .filter({ hasText: "协议配置与此登记不一致" })
      .waitFor();
    assert(
      await p
        .getByRole("button", { name: "核对并完成登记", exact: true })
        .isDisabled(),
    );
    detail = await call(p, "GET", target);
    assert(
      !detail.body.active &&
        detail.body.state === "pending" &&
        detail.body.version === 1,
    );
    execFileSync(
      "python3",
      ["tests/applications/managed_fixture.py", "restore", client],
      { cwd: s.root, stdio: "pipe" },
    );
    await p.getByRole("button", { name: "刷新登记", exact: true }).click();
    await p
      .getByRole("dialog")
      .getByText("待完成登记", { exact: true })
      .waitFor();
    phase = "activate-restored-profile";
    await lifecycleConfirm(p, "核对并完成登记");
    await p.getByRole("dialog").getByText("已生效", { exact: true }).waitFor();
    detail = await call(p, "GET", target);
    assert(detail.body.active && detail.body.version === 2);
    phase = "owner-boundaries-resource-tags-and-recent-mfa";
    await call(guest.page, "POST", apps, { name: "Forbidden" }, 403);
    await call(
      guest.page,
      "POST",
      app + "/clients",
      { name: "Forbidden", redirect_uri: "http://127.0.0.1:9876/callback" },
      403,
    );
    await call(guest.page, "GET", target, undefined, 403);
    await call(guest.page, "POST", target + "/revoke", {}, 403, detail.etag);
    await call(guest.page, "POST", target + "/reconcile", {}, 403, detail.etag);
    await call(
      otherPage,
      "GET",
      "/v1/tenants/" +
        other +
        "/applications/" +
        application.id +
        "/clients/" +
        client,
      undefined,
      404,
    );
    await call(p, "POST", target + "/revoke", {}, 428);
    await call(p, "POST", target + "/revoke", {}, 400, '"authz-1"');
    await call(
      p,
      "POST",
      target + "/revoke",
      {},
      412,
      '"client-' + other + "-" + client + '-2"',
    );
    await call(
      p,
      "POST",
      target + "/reconcile",
      {},
      412,
      '"client-' + tenant + "-" + "f".repeat(32) + '-2"',
    );
    await call(
      p,
      "POST",
      target + "/revoke",
      { secret: "not-accepted" },
      400,
      detail.etag,
    );
    execFileSync(
      "python3",
      ["tests/api/fixtures.py", "age-session", me.user_id, me.session_id],
      { cwd: s.root, stdio: "pipe" },
    );
    for (const [url, body, tag] of [
      [apps, { name: "Aged" }, null],
      [
        app + "/clients",
        { name: "Aged", redirect_uri: "http://127.0.0.1:9876/callback" },
        null,
      ],
      [target + "/revoke", {}, detail.etag],
      [target + "/reconcile", {}, detail.etag],
    ]) {
      const denied = await call(p, "POST", url, body, 403, tag);
      assert.equal(denied.body.code, "recent_mfa_required");
    }
    await call(p, "GET", target);
    me = await s.login(owner, p, tenant, true);
    phase = "managed-metadata-desktop-mobile-and-revocation";
    await s.nav(p, "应用接入");
    row = p.getByRole("row").filter({
      has: p.locator("strong").filter({ hasText: /^本地集成应用$/ }),
    });
    await row
      .getByRole("button", { name: "查看登记配置", exact: true })
      .click();
    await p.getByRole("button", { name: "管理登记", exact: true }).click();
    await p.getByRole("dialog").getByText("已生效", { exact: true }).waitFor();
    await p.screenshot({
      path: path.join(s.root, "docs/ui/managed-client-desktop.png"),
      fullPage: true,
    });
    await p.setViewportSize({ width: 390, height: 844 });
    assert(
      await p.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    );
    const checkBox = await p
      .getByLabel("我已核对配置并确认本次登记操作")
      .boundingBox();
    assert(checkBox && checkBox.width < 24);
    await p.screenshot({
      path: path.join(s.root, "docs/ui/managed-client-mobile.png"),
      fullPage: true,
    });
    await p
      .getByRole("button", { name: "关闭登记对话框", exact: true })
      .click();
    const mobileRow = p.getByRole("row").filter({
      has: p.locator("strong").filter({ hasText: /^本地集成应用$/ }),
    });
    const registrationButton = await mobileRow
      .getByRole("button", { name: "登记公开客户端", exact: true })
      .boundingBox();
    assert(registrationButton && registrationButton.height < 55);
    await p.screenshot({
      path: path.join(s.root, "docs/ui/managed-applications-mobile.png"),
      fullPage: true,
    });
    await mobileRow
      .getByRole("button", { name: "查看登记配置", exact: true })
      .click();
    await p.getByRole("button", { name: "管理登记", exact: true }).click();
    await p.getByRole("dialog").getByText("已生效", { exact: true }).waitFor();
    await p.setViewportSize({ width: 1280, height: 720 });
    await lifecycleConfirm(p, "撤销客户端");
    await p.getByRole("dialog").getByText("已撤销", { exact: true }).waitFor();
    const revoked = await call(p, "GET", target);
    assert(!revoked.body.active && revoked.body.version === 3);
    await lifecycleConfirm(p, "清理已撤销登记");
    await p
      .getByRole("dialog")
      .getByRole("status")
      .filter({ hasText: "协议登记已不存在" })
      .waitFor();
    assert.equal((await s.bff(p, "/v1/me")).status, 200);
    assert.equal((await s.bff(otherPage, "/v1/me")).status, 200);
    const noop = await call(
      p,
      "POST",
      target + "/revoke",
      {},
      200,
      revoked.etag,
    );
    assert.equal(noop.body.version, 3);
    await call(p, "POST", target + "/reconcile", {}, 200, revoked.etag);
    await p
      .getByRole("button", { name: "关闭登记对话框", exact: true })
      .click();
    phase = "managed-client-playground-version-read";
    await s.nav(p, "API Playground");
    await p
      .getByLabel("选择 API", { exact: true })
      .selectOption("reconcileManagedClient");
    await p.getByLabel("application", { exact: true }).fill(application.id);
    await p.getByLabel("client", { exact: true }).fill(client);
    await p.getByRole("button", { name: "读取最新 ETag", exact: true }).click();
    await p
      .getByRole("region", { name: "API 响应", exact: true })
      .getByRole("heading", { name: "HTTP 200", exact: true })
      .waitFor();
    assert.equal(
      await p.getByLabel("If-Match", { exact: true }).inputValue(),
      revoked.etag,
    );
    const audit = (await s.bff(p, base + "/audit-events?limit=100")).body.items;
    for (const action of [
      "application.created",
      "client.registration_pending",
      "client.activated",
      "client.revoked",
      "client.protocol_cleanup_checked",
    ])
      assert(audit.some((a) => a.action === action));
    assert.equal(audit.filter((a) => a.action === "client.revoked").length, 1);
    assert.deepEqual(s.pageErrors, []);
    assert.deepEqual(s.leaks, []);
    console.log(
      "Managed client acceptance passed: " +
        checks +
        " contract responses; tenant ownership, pending activation, real protocol drift refusal/recovery, exact ETags, MFA, no-op/revocation/cleanup, other-space retention, UI/mobile, audit and Playground",
    );
  } finally {
    clearInterval(progress);
    await s.close();
  }
})().catch((e) => {
  console.error(
    "Managed client acceptance failed: phase=" +
      phase +
      ", error=" +
      e.name +
      " (private details withheld)",
  );
  console.error(JSON.stringify(s.diagnostics()));
  process.exitCode = 1;
});
