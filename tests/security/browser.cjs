const s = require("../support/console-fixtures.cjs"),
  assert = require("node:assert/strict"),
  { execFileSync } = require("node:child_process"),
  path = require("node:path");
let phase = "startup",
  success = false,
  checks = 0;
const progress = setInterval(
  () => console.log("Security acceptance phase: " + phase),
  20000,
);
async function contract(page, method, url, body, status = 200) {
  const result = await s.bff(page, url, { method, body });
  assert.equal(result.status, status);
  execFileSync("python3", ["tests/api/validate_contract.py"], {
    cwd: s.root,
    input: JSON.stringify({
      contract: "console",
      path: "/bff" + url,
      method,
      status,
      body: result.body,
      request_body: body,
      content_type: result.contentType,
      request_id: result.requestID,
    }),
    stdio: ["pipe", "pipe", "pipe"],
  });
  checks++;
  return result;
}
function validatePrivate(status, body, input) {
  execFileSync("python3", ["tests/api/validate_contract.py"], {
    cwd: s.root,
    input: JSON.stringify({
      contract: "security",
      path: "/v1/sso/recovery/regenerate/{id}",
      method: "POST",
      status,
      body,
      request_body: input,
      content_type: "application/json",
    }),
    stdio: ["pipe", "pipe", "pipe"],
  });
  checks++;
}
async function action(page, id, csrf, password, code, extra = {}) {
  const result = await page.evaluate(
    async (args) => {
      const r = await fetch("/v1/sso/recovery/regenerate/" + args.id, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          ...(args.csrf ? { "X-CSRF-Token": args.csrf } : {}),
        },
        body: JSON.stringify({
          password: args.password,
          code: args.code,
          ...args.extra,
        }),
      });
      return { status: r.status, body: await r.json() };
    },
    { id, csrf, password, code, extra },
  );
  validatePrivate(result.status, result.body);
  return result;
}
(async () => {
  try {
    await s.start();
    const owner = await s.register(),
      guest = await s.register();
    let me = await s.login(owner);
    await s.login(guest);
    const otherContext = await owner.context.browser().newContext(),
      otherPage = await otherContext.newPage();
    const other = { ...owner, context: otherContext, page: otherPage, used: 0 };
    await s.login(other);
    owner.used = other.used;
    const sameSession = await owner.context.newPage();
    await s.login(owner, sameSession, me.tenant_id);
    const p = owner.page;
    phase = "self-summary-and-other-session-revocation";
    const before = await contract(p, "GET", "/v1/me/security");
    assert.equal(before.body.active_sessions, 2);
    assert.equal(before.body.mfa_enabled, true);
    assert.equal(before.body.recovery_codes_remaining, 9);
    const revoked = await contract(p, "POST", "/v1/sessions/revoke-others", {});
    assert.equal(revoked.body.revoked_sessions, 1);
    assert.equal((await s.bff(otherPage, "/v1/me")).status, 401);
    assert.equal((await s.bff(sameSession, "/v1/me")).status, 200);
    assert.equal((await s.bff(guest.page, "/v1/me")).status, 200);
    assert.equal(
      (await contract(p, "POST", "/v1/sessions/revoke-others", {})).body
        .revoked_sessions,
      0,
    );
    phase = "recent-mfa-and-console-self-service";
    execFileSync(
      "python3",
      ["tests/api/fixtures.py", "age-session", me.user_id, me.session_id],
      { cwd: s.root, stdio: "pipe" },
    );
    await contract(p, "POST", "/v1/sessions/revoke-others", {}, 403);
    me = await s.login(owner, p, me.tenant_id, true);
    phase = "console-security-overview";
    await s.nav(p, "安全会话");
    await p
      .getByRole("heading", { name: "账户安全概览", exact: true })
      .waitFor();
    phase = "console-confirm-other-revocation";
    await p.getByRole("button", { name: "撤销其他会话", exact: true }).click();
    await p
      .getByRole("dialog")
      .getByRole("button", { name: "确认操作", exact: true })
      .click();
    await p
      .getByRole("status")
      .filter({ hasText: "其他现有会话已撤销" })
      .waitFor();
    await p.screenshot({
      path: path.join(s.root, "docs/ui/account-security.png"),
      fullPage: true,
    });
    const securityPage = await owner.context.newPage();
    securityPage.on("pageerror", () =>
      s.pageErrors.push("security page error"),
    );
    await securityPage.goto("http://auth.localhost:8080/sso/security");
    await securityPage
      .getByRole("heading", { name: "重新生成恢复码", exact: true })
      .waitFor();
    const id = await securityPage
        .locator('meta[name="security-id"]')
        .getAttribute("content"),
      csrf = await securityPage
        .locator('meta[name="security-csrf"]')
        .getAttribute("content");
    phase = "csrf-browser-session-and-body-bounds";
    assert.equal(
      (await action(securityPage, id, "", owner.password, "000000")).status,
      403,
    );
    const foreign = await guest.context.newPage();
    await foreign.goto("http://auth.localhost:8080/sso/security");
    assert.equal(
      (await action(foreign, id, csrf, owner.password, "000000")).status,
      403,
    );
    assert.equal(
      (
        await action(securityPage, id, csrf, owner.password, "000000", {
          recovery: owner.codes[9],
        })
      ).status,
      400,
    );
    phase = "password-and-totp-required";
    assert.equal(
      (
        await action(
          securityPage,
          id,
          csrf,
          "not the account password",
          "000000",
        )
      ).status,
      403,
    );
    assert.equal(
      (await action(securityPage, id, csrf, owner.password, "invalid")).status,
      403,
    );
    phase = "waiting-for-unused-totp";
    await new Promise((done) => setTimeout(done, 30050 - (Date.now() % 30000)));
    const freshCode = s.totp(owner.totpSecret),
      oldCode = owner.codes[9];
    phase = "trusted-recovery-regeneration";
    await securityPage
      .getByLabel("当前密码", { exact: true })
      .fill(owner.password);
    await securityPage
      .getByLabel("认证器动态验证码", { exact: true })
      .fill(freshCode);
    await securityPage.getByLabel("我确认使旧恢复码和全部登录会话失效").check();
    const generatedResponse = securityPage.waitForResponse(
      (r) =>
        new URL(r.url()).pathname === "/v1/sso/recovery/regenerate/" + id &&
        r.request().method() === "POST",
    );
    await securityPage
      .getByRole("button", { name: "验证并生成新恢复码", exact: true })
      .click();
    await securityPage
      .getByRole("heading", { name: "保存新的恢复码", exact: true })
      .waitFor();
    const generated = await generatedResponse;
    validatePrivate(generated.status(), await generated.json(), {
      password: owner.password,
      code: freshCode,
    });
    const codes = (await securityPage.locator("#codes").textContent())
      .trim()
      .split("\n");
    assert.equal(codes.length, 10);
    assert.equal(new Set(codes).size, 10);
    assert(!codes.includes(oldCode));
    assert.equal((await s.bff(p, "/v1/me")).status, 401);
    assert.equal((await s.bff(sameSession, "/v1/me")).status, 401);
    assert.equal((await s.bff(guest.page, "/v1/me")).status, 200);
    const replay = await action(
      securityPage,
      id,
      csrf,
      owner.password,
      freshCode,
    );
    assert.equal(replay.status, 403);
    assert(!("recovery_codes" in replay.body));
    assert.equal(
      await securityPage.evaluate(
        () => localStorage.length + sessionStorage.length,
      ),
      0,
    );
    await securityPage
      .getByRole("button", { name: "我已安全保存，重新登录", exact: true })
      .click();
    phase = "old-code-rejected-new-code-accepted";
    await securityPage
      .getByRole("heading", { name: "登录你的账户", exact: true })
      .waitFor();
    phase = "new-login-password";
    await securityPage.getByLabel("邮箱地址").fill(owner.email);
    await securityPage.getByLabel("密码", { exact: true }).fill(owner.password);
    await securityPage
      .getByRole("button", { name: "继续", exact: true })
      .click();
    await securityPage
      .getByRole("heading", { name: "验证你的身份", exact: true })
      .waitFor();
    await securityPage.locator("#recovery-option summary").click();
    phase = "old-recovery-denial";
    await securityPage.getByLabel("一次性恢复码").fill(oldCode);
    await securityPage
      .getByRole("button", { name: "验证身份", exact: true })
      .click();
    await securityPage.locator("#error").filter({ hasText: /.+/ }).waitFor();
    phase = "new-recovery-verification";
    await securityPage.getByLabel("一次性恢复码").fill(codes[0]);
    await securityPage
      .getByRole("button", { name: "验证身份", exact: true })
      .click();
    await securityPage
      .getByRole("heading", { name: "身份验证完成", exact: true })
      .waitFor();
    phase = "new-login-completion";
    await securityPage.locator("#continue").click();
    await securityPage
      .getByRole("button", { name: "同意并继续", exact: true })
      .click();
    await securityPage
      .getByRole("heading", { name: "空间概览", exact: true })
      .waitFor();
    phase = "new-login-security-contract";
    assert.equal(
      (await contract(securityPage, "GET", "/v1/me/security")).body
        .recovery_codes_remaining,
      9,
    );
    phase = "totp-replay-after-new-login";
    await securityPage.goto("http://auth.localhost:8080/sso/security");
    const nextID = await securityPage
        .locator('meta[name="security-id"]')
        .getAttribute("content"),
      nextCSRF = await securityPage
        .locator('meta[name="security-csrf"]')
        .getAttribute("content");
    assert.equal(
      (await action(securityPage, nextID, nextCSRF, owner.password, freshCode))
        .status,
      403,
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
      "Account security acceptance passed: " +
        checks +
        " contract responses, self-only summary, revoke others/current grant preservation, recent MFA, trusted password+TOTP, CSRF/browser/session bounds, one-time code replacement, all-session revocation, old-code/TOTP replay rejection and new-code login",
    );
})().catch((e) => {
  console.error(
    "Account security failed: phase=" +
      phase +
      ", error=" +
      e.name +
      " (private details withheld)",
  );
  console.error(JSON.stringify(s.diagnostics()));
  process.exitCode = 1;
});
