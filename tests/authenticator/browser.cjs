const s = require("../support/console-fixtures.cjs"),
  assert = require("node:assert/strict"),
  { execFileSync } = require("node:child_process"),
  path = require("node:path");
let phase = "startup",
  checks = 0;
const progress = setInterval(
  () => console.log("Authenticator acceptance phase: " + phase),
  20000,
);
function validate(action, status, body, requestBody) {
  execFileSync("python3", ["tests/api/validate_contract.py"], {
    cwd: s.root,
    input: JSON.stringify({
      contract: "security",
      path: "/v1/sso/authenticator/{id}/" + action,
      method: "POST",
      status,
      body,
      request_body: requestBody,
      content_type: status === 204 ? "" : "application/json",
    }),
    stdio: ["pipe", "pipe", "pipe"],
  });
  checks++;
}
async function post(p, binding, action, body, csrf = binding.csrf) {
  const r = await p.evaluate(
    async ({ id, csrf, action, body }) => {
      const r = await fetch("/v1/sso/authenticator/" + id + "/" + action, {
        method: "POST",
        headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf },
        body: JSON.stringify(body),
      });
      return {
        status: r.status,
        body: r.status === 204 ? null : await r.json(),
      };
    },
    { id: binding.id, csrf, action, body },
  );
  validate(action, r.status, r.body, r.status < 300 ? body : undefined);
  return r;
}
async function manager(f) {
  const p = await f.context.newPage();
  p.on("pageerror", () => s.pageErrors.push("authenticator page error"));
  await p.goto("http://auth.localhost:8080/sso/security/authenticator");
  await p.getByRole("heading", { name: "更换认证器", exact: true }).waitFor();
  return {
    p,
    id: await p.locator('meta[name="change-id"]').getAttribute("content"),
    csrf: await p.locator('meta[name="change-csrf"]').getAttribute("content"),
  };
}
async function stage(f, b) {
  const code = s.totp(f.totpSecret);
  await b.p.getByLabel("当前密码", { exact: true }).fill(f.password);
  await b.p.getByLabel("旧认证器动态验证码", { exact: true }).fill(code);
  const response = b.p.waitForResponse(
    (r) =>
      new URL(r.url()).pathname === "/v1/sso/authenticator/" + b.id + "/start",
  );
  await b.p
    .getByRole("button", { name: "验证旧认证器并继续", exact: true })
    .click();
  await b.p.locator("#new-factor-form").waitFor({ state: "visible" });
  const r = await response;
  validate("start", r.status(), await r.json(), { password: f.password, code });
  const secret = await b.p.locator("#new-secret").textContent();
  assert.match(secret, /^[A-Z2-7]{32}$/);
  assert.equal((await s.bff(f.page, "/v1/me")).status, 200);
  return secret;
}
async function password(p, f) {
  await p.getByRole("heading", { name: "登录你的账户", exact: true }).waitFor();
  await p.getByLabel("邮箱地址").fill(f.email);
  await p.getByLabel("密码", { exact: true }).fill(f.password);
  await p.getByRole("button", { name: "继续", exact: true }).click();
  await p.getByRole("heading", { name: "验证你的身份", exact: true }).waitFor();
}
async function finish(p) {
  await p.getByRole("heading", { name: "身份验证完成", exact: true }).waitFor();
  await p.locator("#continue").click();
  await p.getByRole("button", { name: "同意并继续", exact: true }).click();
  await p.getByRole("heading", { name: "空间概览", exact: true }).waitFor();
}
(async () => {
  try {
    await s.start();
    const owner = await s.register(),
      cancelled = await s.register();
    await s.login(owner);
    await s.login(cancelled);
    const b = await manager(owner),
      c = await manager(cancelled);
    phase = "empty-entry-and-boundaries";
    await b.p.screenshot({
      path: path.join(s.root, "docs/ui/authenticator-entry.png"),
      fullPage: true,
    });
    await b.p.setViewportSize({ width: 390, height: 844 });
    assert(
      await b.p.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    );
    await b.p.setViewportSize({ width: 1280, height: 720 });
    assert.equal(
      (
        await post(
          b.p,
          b,
          "start",
          { password: owner.password, code: "000000" },
          "wrong",
        )
      ).status,
      403,
    );
    assert.equal(
      (
        await post(c.p, b, "start", {
          password: owner.password,
          code: "000000",
        })
      ).status,
      403,
    );
    assert.equal(
      (await post(b.p, b, "confirm", { code: "000000" })).status,
      403,
    );
    assert.equal(
      (
        await post(b.p, b, "start", {
          password: owner.password,
          code: "000000",
          secret: "client-selected",
        })
      ).status,
      400,
    );
    for (const raw of [
      "null",
      '{"code":"000000","code":"000000"}',
      '{"Code":"000000"}',
      '{"code":null}',
    ]) {
      const result = await b.p.evaluate(
        async ({ id, csrf, raw }) => {
          const r = await fetch("/v1/sso/authenticator/" + id + "/confirm", {
            method: "POST",
            headers: {
              "Content-Type": "application/json",
              "X-CSRF-Token": csrf,
            },
            body: raw,
          });
          return { status: r.status, body: await r.json() };
        },
        { id: b.id, csrf: b.csrf, raw },
      );
      assert.equal(result.status, 400);
      validate("confirm", result.status, result.body);
    }
    const wrongPurpose = await b.p.evaluate(
      async ({ id, csrf, password }) => {
        const r = await fetch("/v1/sso/recovery/regenerate/" + id, {
          method: "POST",
          headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf },
          body: JSON.stringify({ password, code: "000000" }),
        });
        return { status: r.status, body: await r.json() };
      },
      { id: b.id, csrf: b.csrf, password: owner.password },
    );
    assert.equal(wrongPurpose.status, 403);
    assert(!("recovery_codes" in wrongPurpose.body));
    phase = "waiting-for-unused-old-totp";
    await new Promise((r) => setTimeout(r, 30080 - (Date.now() % 30000)));
    phase = "cancel-preserves-old-authority";
    await stage(cancelled, c);
    const cancelResponse = c.p.waitForResponse((r) =>
      new URL(r.url()).pathname.endsWith("/" + c.id + "/cancel"),
    );
    await c.p
      .getByRole("button", { name: "取消更换，保留原认证器", exact: true })
      .click();
    await c.p.getByRole("status").filter({ hasText: "已取消" }).waitFor();
    validate("cancel", (await cancelResponse).status(), null, {});
    assert.equal(await c.p.locator("#new-secret").textContent(), "");
    assert.equal(
      (await s.bff(cancelled.page, "/v1/me/security")).body
        .recovery_codes_remaining,
      10,
    );
    assert.equal(
      (await post(c.p, c, "confirm", { code: "000000" })).status,
      403,
    );
    phase = "stage-and-confirm-new-authenticator";
    const newSecret = await stage(owner, b),
      oldRecovery = owner.codes[9];
    assert.equal(
      (
        await post(b.p, b, "start", {
          password: owner.password,
          code: "000000",
        })
      ).status,
      403,
    );
    assert.equal(
      (await post(b.p, b, "confirm", { code: "invalid" })).status,
      403,
    );
    const newCode = s.totp(newSecret);
    await b.p.getByLabel("新认证器动态验证码", { exact: true }).fill(newCode);
    await b.p
      .getByLabel("我确认替换认证器，使旧密钥、恢复码和全部旧会话失效")
      .check();
    const confirmed = b.p.waitForResponse((r) =>
      new URL(r.url()).pathname.endsWith("/" + b.id + "/confirm"),
    );
    await b.p
      .getByRole("button", { name: "确认启用新认证器", exact: true })
      .click();
    await b.p
      .getByRole("heading", { name: "保存新的恢复码", exact: true })
      .waitFor();
    const r = await confirmed;
    validate("confirm", r.status(), await r.json(), { code: newCode });
    const codes = (await b.p.locator("#codes").textContent())
      .trim()
      .split("\n");
    assert.equal(codes.length, 10);
    assert.equal(new Set(codes).size, 10);
    assert(!codes.includes(oldRecovery));
    assert.equal(await b.p.locator("#new-secret").textContent(), "");
    assert.equal((await s.bff(owner.page, "/v1/me")).status, 401);
    assert.equal((await s.bff(cancelled.page, "/v1/me")).status, 200);
    const replay = await post(b.p, b, "confirm", { code: newCode });
    assert.equal(replay.status, 403);
    assert(!("recovery_codes" in replay.body));
    assert.equal(
      await b.p.evaluate(() => localStorage.length + sessionStorage.length),
      0,
    );
    phase = "old-recovery-rejected-new-recovery-accepted";
    await b.p
      .getByRole("button", { name: "我已安全保存，重新登录", exact: true })
      .click();
    await password(b.p, owner);
    await b.p.locator("#recovery-option summary").click();
    await b.p.getByLabel("一次性恢复码").fill(oldRecovery);
    await b.p.getByRole("button", { name: "验证身份", exact: true }).click();
    await b.p.locator("#error").filter({ hasText: /.+/ }).waitFor();
    await b.p.getByLabel("一次性恢复码").fill(codes[0]);
    await b.p.getByRole("button", { name: "验证身份", exact: true }).click();
    await finish(b.p);
    assert.equal(
      (await s.bff(b.p, "/v1/me/security")).body.recovery_codes_remaining,
      9,
    );
    phase = "waiting-for-unused-new-totp";
    await new Promise((r) => setTimeout(r, 30080 - (Date.now() % 30000)));
    await b.p.goto("http://console.localhost:3003/login?force=1");
    await password(b.p, owner);
    phase = "old-authenticator-rejected-new-authenticator-accepted";
    await b.p
      .getByLabel("动态验证码", { exact: true })
      .fill(s.totp(owner.totpSecret));
    await b.p.getByRole("button", { name: "验证身份", exact: true }).click();
    await b.p.locator("#error").filter({ hasText: /.+/ }).waitFor();
    await b.p.getByLabel("动态验证码", { exact: true }).fill(s.totp(newSecret));
    await b.p.getByRole("button", { name: "验证身份", exact: true }).click();
    await finish(b.p);
    assert.equal((await s.bff(b.p, "/v1/me")).status, 200);
    phase = "committed-confirm-response-lost";
    const lost = await manager(cancelled),
      lostSecret = await stage(cancelled, lost);
    const lostCode = s.totp(lostSecret);
    let committed = false,
      interceptionFailed = false;
    await lost.p.route(
      "**/v1/sso/authenticator/" + lost.id + "/confirm",
      async (route) => {
        try {
          const response = await route.fetch({
              url:
                "http://127.0.0.1:8080/v1/sso/authenticator/" +
                lost.id +
                "/confirm",
              headers: {
                ...(await route.request().allHeaders()),
                host: "auth.localhost:8080",
              },
              maxRedirects: 0,
              maxRetries: 0,
            }),
            data = await response.json();
          validate("confirm", response.status(), data, { code: lostCode });
          committed = response.status() === 200;
          data.recovery_codes = [];
          await route.abort("failed");
        } catch {
          interceptionFailed = true;
          await route.abort("failed").catch(() => {});
        }
      },
    );
    await lost.p
      .getByLabel("新认证器动态验证码", { exact: true })
      .fill(lostCode);
    await lost.p
      .getByLabel("我确认替换认证器，使旧密钥、恢复码和全部旧会话失效")
      .check();
    await lost.p
      .getByRole("button", { name: "确认启用新认证器", exact: true })
      .click();
    await lost.p
      .getByRole("alert")
      .filter({ hasText: "连接中断，更换可能已完成" })
      .waitFor();
    phase = "lost-response-commit-verified";
    assert(committed && !interceptionFailed);
    assert.equal(await lost.p.locator("#new-secret").textContent(), "");
    assert.equal(await lost.p.locator("#codes").textContent(), "");
    assert(await lost.p.locator("#new-factor-form").isHidden());
    assert(await lost.p.locator("#cancel").isHidden());
    assert.equal((await s.bff(cancelled.page, "/v1/me")).status, 401);
    phase = "lost-response-new-authenticator-login";
    await new Promise((r) => setTimeout(r, 30080 - (Date.now() % 30000)));
    await lost.p
      .getByRole("link", { name: "重新登录核对", exact: true })
      .click();
    await password(lost.p, cancelled);
    await lost.p
      .getByLabel("动态验证码", { exact: true })
      .fill(s.totp(lostSecret));
    await lost.p.getByRole("button", { name: "验证身份", exact: true }).click();
    await finish(lost.p);
    assert.equal((await s.bff(lost.p, "/v1/me")).status, 200);
    assert.deepEqual(s.pageErrors, []);
    assert.deepEqual(s.leaks, []);
    console.log(
      "Authenticator acceptance passed: " +
        checks +
        " contract responses; staged cancellation, proof/CSRF/session/purpose bounds, key replacement, revocation, old/new proofs and committed response-loss recovery",
    );
  } finally {
    clearInterval(progress);
    await s.close();
  }
})().catch((e) => {
  console.error(
    "Authenticator acceptance failed: phase=" +
      phase +
      ", error=" +
      e.name +
      " (private details withheld)",
  );
  console.error(JSON.stringify(s.diagnostics()));
  process.exitCode = 1;
});
