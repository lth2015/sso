// Add a separate showcase space/application using the retained accounts.
// Never reset/recreate credentials or alter the original primary/secondary data.
const d = require("./lib/demo-support.cjs"),
  { chromium } = require("playwright");
let browser,
  lock,
  phase = "startup";
const errors = [],
  secrets = [],
  started = new Date().toISOString(),
  progress = setInterval(
    () => console.log("Application access demo preparation phase: " + phase),
    20000,
  );
async function api(p, url, method = "GET", body, etag, status = 200) {
  const r = await d.api(
    p,
    url,
    { method, body, headers: etag ? { "If-Match": etag } : {} },
    status,
  );
  if (typeof r.body?.token === "string") secrets.push(r.body.token);
  return r;
}
async function warmAuthorization(actor, tenant) {
  const p = actor.page;
  await p.goto("http://console.localhost:3003/login");
  await p.waitForFunction(() =>
    ["登录你的账户", "确认本次授权"].includes(
      document.querySelector("h1")?.textContent,
    ),
  );
  d.assert.equal(
    await p.getByRole("heading", { name: "确认本次授权", exact: true }).count(),
    1,
    "warm SSO unavailable; no recovery code fallback attempted",
  );
  await p.locator("#tenant").selectOption(tenant);
  await p.getByRole("button", { name: "同意并继续", exact: true }).click();
  await p.getByRole("heading", { name: "空间概览", exact: true }).waitFor();
  const me = (await api(p, "/v1/me")).body;
  d.assert.equal(me.user_id, actor.user);
  d.assert.equal(me.tenant_id, tenant);
}
(async () => {
  try {
    const m = await d.load();
    d.assert(
      m && m.state === "ready",
      "prepare the retained baseline demo first",
    );
    lock = await d.fs.open(
      d.path.join(d.directory, "command.lock"),
      "wx",
      0o600,
    );
    await lock.writeFile(String(process.pid) + "\n");
    browser = await chromium.launch({
      executablePath: process.env.CHROMIUM_PATH || "/usr/bin/chromium",
      headless: true,
      args: ["--no-sandbox"],
    });
    const ownerAccount = m.accounts.find((a) => a.role === "owner"),
      auditorAccount = m.accounts.find((a) => a.role === "auditor");
    phase = "retained-password-and-totp";
    const owner = await d.login(browser, m, ownerAccount, errors),
      p = owner.page;
    if (m.applicationUserAccess) {
      const f = m.applicationUserAccess;
      d.assert(
        f.state === "ready" && f.tool === "scripts/demo-user-access",
        "partial feature demo retained; no automatic re-creation or reset",
      );
      await warmAuthorization(owner, f.tenant.id);
      const root =
        "/v1/tenants/" + f.tenant.id + "/applications/" + f.application.id;
      const policy = (await api(p, root + "/user-access")).body;
      const client = (await api(p, root + "/clients/" + f.client_id)).body;
      d.assert(
        policy.policy.application_id === f.application.id &&
          client.active &&
          client.state === "active",
      );
      console.log(
        "Application access demo retained and verified; existing admission settings were preserved. Start with scripts/demo user-access",
      );
      return;
    }
    const f = { tool: "scripts/demo-user-access", state: "preparing" };
    m.applicationUserAccess = f;
    await d.save(m);
    phase = "separate-showcase-space";
    const created = (
      await api(
        p,
        "/v1/tenants",
        "POST",
        { slug: "app-access-" + m.runId, name: "应用准入演示空间" },
        undefined,
        201,
      )
    ).body;
    f.tenant = { ...created, name: "应用准入演示空间" };
    await d.save(m);
    await warmAuthorization(owner, f.tenant.id);
    const base = "/v1/tenants/" + f.tenant.id;
    const auditor = await d.login(browser, m, auditorAccount, errors);
    const invitation = (
      await api(
        p,
        base + "/invitations",
        "POST",
        { email: auditorAccount.email },
        undefined,
        201,
      )
    ).body;
    const accepted = (
      await api(
        auditor.page,
        "/v1/invitations/accept",
        "POST",
        { token: invitation.token },
        undefined,
        201,
      )
    ).body;
    f.member_id = accepted.membership_id;
    await d.save(m);
    await warmAuthorization(owner, f.tenant.id);
    phase = "owned-application-and-public-pkce-client";
    f.application = (
      await api(
        p,
        base + "/applications",
        "POST",
        { name: "应用准入演示工作台" },
        undefined,
        201,
      )
    ).body;
    await d.save(m);
    const appURL = base + "/applications/" + f.application.id;
    const client = await api(
      p,
      appURL + "/clients",
      "POST",
      {
        name: "本地独立应用演示",
        redirect_uri: "http://127.0.0.1:9876/callback",
      },
      undefined,
      201,
    );
    f.client_id = client.body.client_id;
    await d.save(m);
    await api(
      p,
      appURL + "/clients/" + f.client_id + "/reconcile",
      "POST",
      {},
      client.etag,
    );
    phase = "initial-owner-only-admission";
    const policy = await api(p, appURL + "/user-access");
    await api(
      p,
      appURL + "/user-access",
      "PUT",
      { mode: "selected_members" },
      policy.etag,
    );
    const decision = (
      await api(p, appURL + "/user-access/members/" + f.member_id)
    ).body;
    d.assert(
      !decision.allowed &&
        !decision.explicit_grant &&
        decision.reason === "not_granted",
    );
    const directory = (await api(p, base + "/members")).body.items;
    d.assert.equal(directory.length, 2);
    d.assert(
      directory.some(
        (v) => v.id === f.member_id && v.user_id === auditorAccount.user,
      ),
    );
    d.assert.equal(
      (await api(p, "/v1/me/security")).body.recovery_codes_remaining,
      ownerAccount.codes.length,
    );
    d.assert.equal(
      (await api(auditor.page, "/v1/me/security")).body
        .recovery_codes_remaining,
      auditorAccount.codes.length,
    );
    f.state = "ready";
    await d.save(m);
    await d.presenter(m);
    await d.writePrivate(
      "application-access-demo.md",
      "# 应用准入演示（本地私有元数据）\n\n原主空间和隔离空间保持。\n\n演示空间：" +
        f.tenant.name +
        "\n\n工作台：" +
        f.application.name +
        "\n\n成员使用原只读审计员账号，登录资料仍在 presenter.md。此新空间中该账号只有普通成员资格。\n\n成员 ID：" +
        f.member_id +
        "\n\n公开客户端 ID：" +
        f.client_id +
        "\n\n启动：scripts/demo user-access\n\n独立应用：http://127.0.0.1:9876\n\n初始 selected_members 且未给成员显式授权。控制台切换到演示空间 → 应用接入 → 成员访问，按 docs/runbooks/application-access-demo.md 完成授权/登录/撤销。重新准备只核验，保留你在演示中修改的设置。\n",
    );
    const privacy = require("../tests/support/private-material.cjs")(
      d.root,
      secrets,
      started,
    );
    d.assert.deepEqual(errors, []);
    console.log(
      "Application access demo ready: separate space, retained accounts and unused recovery codes, owned PKCE application, initial member refusal; private exclusion in " +
        privacy.files +
        " files/runtime logs. Start with scripts/demo user-access",
    );
  } finally {
    clearInterval(progress);
    if (browser) await browser.close();
    if (lock) {
      await lock.close();
      await d.fs.unlink(d.path.join(d.directory, "command.lock"));
    }
  }
})().catch((e) => {
  console.error(
    "Application access demo preparation failed: phase=" +
      phase +
      ", error=" +
      e.name +
      " (private details withheld); retained data was not reset",
  );
  process.exitCode = 1;
});
