// Actual UI/BFF/API calls. No tokens, proofs or raw network exceptions in output.
const s = require("../support/console-fixtures.cjs"),
  assert = require("node:assert/strict"),
  { execFileSync } = require("node:child_process"),
  path = require("node:path");
let phase = "startup",
  success = false;
const progress = setInterval(
  () => console.log("Playground acceptance phase: " + phase),
  20000,
);
async function choose(page, operation) {
  await page.getByLabel("选择 API", { exact: true }).selectOption(operation);
}
async function status(page, code) {
  await page
    .getByRole("region", { name: "API 响应", exact: true })
    .getByRole("heading", { name: "HTTP " + code, exact: true })
    .waitFor();
}
async function read(page, operation, code = 200) {
  await choose(page, operation);
  await page.getByRole("button", { name: "发送读取请求", exact: true }).click();
  await status(page, code);
}
(async () => {
  try {
    await s.start();
    const owner = await s.register(),
      guest = await s.register();
    const me = await s.login(owner),
      p = owner.page,
      base = "/v1/tenants/" + me.tenant_id;
    phase = "invite-real-member";
    const invitation = await s.bff(p, base + "/invitations", {
      method: "POST",
      body: { email: guest.email },
    });
    assert.equal(invitation.status, 201);
    await s.login(guest);
    assert.equal(
      (
        await s.bff(guest.page, "/v1/invitations/accept", {
          method: "POST",
          body: { token: invitation.body.token },
        })
      ).status,
      201,
    );
    await s.login(owner, p, me.tenant_id);
    await s.login(guest, guest.page, me.tenant_id);
    phase = "real-read-and-schema";
    await s.nav(p, "API Playground");
    await p.getByLabel("选择 API", { exact: true }).waitFor();
    assert.equal(
      await p.getByLabel("选择 API", { exact: true }).locator("option").count(),
      55,
    );
    await choose(p, "createMachineCredential");
    await p
      .getByRole("status")
      .filter({ hasText: "一次性凭证创建请使用机器身份的凭证专页" })
      .waitFor();
    assert(
      await p
        .getByRole("button", { name: "检查写入请求", exact: true })
        .isDisabled(),
    );
    await read(p, "getMe");
    assert(
      (await p.getByTestId("playground-response").textContent()).includes(
        me.tenant_id,
      ),
    );
    phase = "real-audit-export-query-and-default";
    await read(p, "exportAuditBatch");
    const exported = JSON.parse(
      await p.getByTestId("playground-response").textContent(),
    );
    assert.equal(exported.format, "sso.audit.batch.v1");
    assert.equal(exported.kind, "management");
    assert.equal(exported.tenant_id, me.tenant_id);
    await p.getByLabel("kind", { exact: true }).fill("machine");
    await p.getByRole("button", { name: "发送读取请求", exact: true }).click();
    await status(p, 200);
    assert.equal(
      JSON.parse(await p.getByTestId("playground-response").textContent()).kind,
      "machine",
    );
    await p.getByLabel("kind", { exact: true }).fill("invalid");
    await p.getByRole("button", { name: "发送读取请求", exact: true }).click();
    await status(p, 400);
    await read(p, "getMe");
    await p.getByText("外部客户端 cURL 模板", { exact: true }).click();
    assert(
      (await p.locator(".playground details pre").textContent()).includes(
        "<ACCESS_TOKEN>",
      ),
    );
    phase = "write-preview-single-send-and-etag";
    await choose(p, "createOrganization");
    assert.equal(
      await p.getByLabel("tenant", { exact: true }).isEditable(),
      false,
    );
    await p.getByRole("button", { name: "读取最新 ETag", exact: true }).click();
    await status(p, 200);
    const body = JSON.parse(
      await p.getByLabel("请求 JSON", { exact: true }).inputValue(),
    );
    body.name = "Playground <img src=x onerror=alert(1)>";
    await p.getByLabel("请求 JSON", { exact: true }).fill(JSON.stringify(body));
    let writes = 0;
    p.on("request", (r) => {
      if (
        new URL(r.url()).pathname === "/bff" + base + "/organizations" &&
        r.method() === "POST"
      )
        writes++;
    });
    await p.getByRole("button", { name: "检查写入请求", exact: true }).click();
    assert.equal(writes, 0);
    assert.equal(
      await p
        .getByRole("button", { name: "确认发送一次", exact: true })
        .isDisabled(),
      true,
    );
    await p.getByLabel("我确认向当前空间发送这项写入请求").check();
    await p.getByRole("button", { name: "确认发送一次", exact: true }).click();
    await status(p, 201);
    assert.equal(writes, 1);
    await read(p, "listOrganizations");
    assert(
      (await p.getByTestId("playground-response").textContent()).includes(
        body.name,
      ),
    );
    assert.equal(await p.locator(".playground img").count(), 0);
    phase = "sensitive-response-redaction";
    await choose(p, "createInvitation");
    await p
      .getByLabel("请求 JSON", { exact: true })
      .fill(JSON.stringify({ email: guest.email }));
    await p.getByRole("button", { name: "检查写入请求", exact: true }).click();
    await p.getByLabel("我确认向当前空间发送这项写入请求").check();
    await p.getByRole("button", { name: "确认发送一次", exact: true }).click();
    await status(p, 201);
    assert.equal(
      JSON.parse(await p.getByTestId("playground-response").textContent())
        .token,
      "[已隐藏]",
    );
    phase = "actual-permission-denial";
    await s.nav(guest.page, "API Playground");
    await read(guest.page, "listMembers", 403);
    assert.equal(
      JSON.parse(
        await guest.page.getByTestId("playground-response").textContent(),
      ).code,
      "forbidden",
    );
    phase = "stale-precondition";
    await choose(p, "createOrganization");
    await p.getByLabel("If-Match", { exact: true }).fill('"org-1"');
    await p.getByRole("button", { name: "检查写入请求", exact: true }).click();
    await p.getByLabel("我确认向当前空间发送这项写入请求").check();
    await p.getByRole("button", { name: "确认发送一次", exact: true }).click();
    await status(p, 412);
    assert.equal(writes, 2);
    phase = "recent-mfa-is-still-enforced";
    execFileSync(
      "python3",
      ["tests/api/fixtures.py", "age-session", me.user_id, me.session_id],
      { cwd: s.root, stdio: "pipe" },
    );
    await p.getByRole("button", { name: "读取最新 ETag", exact: true }).click();
    await status(p, 200);
    await p.getByRole("button", { name: "检查写入请求", exact: true }).click();
    await p.getByLabel("我确认向当前空间发送这项写入请求").check();
    await p.getByRole("button", { name: "确认发送一次", exact: true }).click();
    await status(p, 403);
    await p.getByRole("link", { name: "重新验证身份", exact: true }).waitFor();
    assert.equal(writes, 3);
    phase = "safe-business-screenshots-and-navigation-reset";
    await read(p, "getMyPermissions");
    await p.screenshot({
      path: path.join(s.root, "docs/ui/playground-desktop.png"),
      fullPage: true,
    });
    await p.setViewportSize({ width: 390, height: 844 });
    assert.equal(
      await p.evaluate(() => document.documentElement.scrollWidth > innerWidth),
      false,
    );
    await p.screenshot({
      path: path.join(s.root, "docs/ui/playground-mobile.png"),
      fullPage: true,
    });
    await s.nav(p, "空间概览");
    await s.nav(p, "API Playground");
    assert.equal(
      await p.getByRole("region", { name: "API 响应", exact: true }).count(),
      0,
    );
    assert.equal(
      await p.evaluate(() => localStorage.length + sessionStorage.length),
      0,
    );
    assert.deepEqual(s.pageErrors, []);
    assert.deepEqual(s.leaks, []);
    success = true;
  } finally {
    clearInterval(progress);
    await s.close();
  }
  if (success)
    console.log(
      "Playground acceptance passed: 55 management contracts (54 executable; one-time SK uses dedicated UI), real reads/writes, explicit confirmation, one send per action, ETag/412, member denial, MFA, redaction, escaping, navigation reset and desktop/mobile",
    );
})().catch((e) => {
  console.error(
    "Playground acceptance failed: phase=" +
      phase +
      ", error=" +
      e.name +
      " (private details withheld)",
  );
  console.error(JSON.stringify(s.diagnostics()));
  process.exitCode = 1;
});
