// Actual React -> isolated BFF -> public API -> business primary -> Hydra.
const { chromium } = require("playwright");
const fs = require("node:fs/promises"),
  path = require("node:path"),
  crypto = require("node:crypto"),
  assert = require("node:assert/strict");
const { execFileSync } = require("node:child_process");
const root = path.resolve(__dirname, "../.."),
  mailbox = path.join(root, ".local/mailbox");
const fixtures = [],
  pageErrors = [],
  leaks = [];
let browser,
  stage = "startup",
  success = "";
const progress = setInterval(
  () => console.log("Console acceptance phase: " + stage),
  20000,
);
const wait = (ms) => new Promise((r) => setTimeout(r, ms));
async function mail(email) {
  for (let i = 0; i < 100; i++) {
    for (const name of await fs.readdir(mailbox)) {
      if (!/^[a-f0-9]{32}\.json$/.test(name)) continue;
      let data;
      try {
        data = JSON.parse(await fs.readFile(path.join(mailbox, name), "utf8"));
      } catch {
        continue;
      }
      if (data.recipient === email && data.purpose === "register")
        return data.token;
    }
    await wait(150);
  }
  throw new Error("local proof unavailable");
}
function totp(secret) {
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
  let bits = "";
  for (const c of secret)
    bits += alphabet.indexOf(c).toString(2).padStart(5, "0");
  const bytes = [];
  for (let i = 0; i + 8 <= bits.length; i += 8)
    bytes.push(parseInt(bits.slice(i, i + 8), 2));
  const counter = Buffer.alloc(8);
  counter.writeBigUInt64BE(BigInt(Math.floor(Date.now() / 30000)));
  const sum = crypto
    .createHmac("sha1", Buffer.from(bytes))
    .update(counter)
    .digest();
  return ((sum.readUInt32BE(sum.at(-1) & 15) & 0x7fffffff) % 1000000)
    .toString()
    .padStart(6, "0");
}
function watch(page) {
  page.on("pageerror", () => pageErrors.push("page error"));
  page.on("response", async (response) => {
    try {
      const u = new URL(response.url());
      if (
        u.hostname !== "console.localhost" ||
        !(u.pathname === "/session" || u.pathname.startsWith("/bff/"))
      )
        return;
      const text = await response.text();
      if (
        /eyJ[A-Za-z0-9_-]{15,}\.[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{20,}/.test(
          text,
        )
      )
        leaks.push("JWT crossed BFF");
    } catch {}
  });
}
async function register(second = false) {
  const f = {
    email:
      "browser-" + crypto.randomBytes(16).toString("hex") + "@example.invalid",
    password: "Console private passphrase 123!",
    codes: [],
    used: 0,
  };
  fixtures.push(f);
  f.context = await browser.newContext();
  f.page = await f.context.newPage();
  watch(f.page);
  const p = f.page;
  await p.goto("http://127.0.0.1:8080/account");
  await p.getByLabel("邮箱地址").fill(f.email);
  await p.getByRole("button", { name: "发送验证说明", exact: true }).click();
  await p.getByRole("status").filter({ hasText: "若地址符合条件" }).waitFor();
  const proof = await mail(f.email);
  await p.goto("http://127.0.0.1:8080/account#purpose=register&token=" + proof);
  await p.getByLabel("设置新密码", { exact: true }).fill(f.password);
  await p.getByLabel("再次输入密码").fill(f.password);
  await p
    .getByRole("button", { name: "验证邮箱并创建账户", exact: true })
    .click();
  await p
    .getByRole("status")
    .filter({ hasText: "邮箱验证与账户创建完成" })
    .waitFor();
  execFileSync(
    "scripts/console",
    ["enable", "--email", f.email, ...(second ? ["--second-tenant"] : [])],
    { cwd: root, stdio: "pipe" },
  );
  return f;
}
async function login(f, page = f.page, tenant, force = false) {
  await page.goto(
    "http://console.localhost:3003/login" + (force ? "?force=1" : ""),
  );
  await page.waitForFunction(() =>
    ["登录你的账户", "确认本次授权"].includes(
      document.querySelector("h1")?.textContent,
    ),
  );
  if (
    await page
      .getByRole("heading", { name: "登录你的账户", exact: true })
      .count()
  ) {
    await page.getByLabel("邮箱地址").fill(f.email);
    await page.getByLabel("密码", { exact: true }).fill(f.password);
    await page.getByRole("button", { name: "继续", exact: true }).click();
    await page.waitForFunction(() =>
      ["设置双重验证", "验证你的身份"].includes(
        document.querySelector("h1")?.textContent,
      ),
    );
    if (
      await page
        .getByRole("heading", { name: "设置双重验证", exact: true })
        .count()
    ) {
      const secret = await page.locator("#secret").textContent();
      await page.getByLabel("动态验证码").fill(totp(secret));
    } else {
      await page.locator("#recovery-option summary").click();
      await page.getByLabel("一次性恢复码").fill(f.codes[f.used++]);
    }
    await page.getByRole("button", { name: "验证身份", exact: true }).click();
    await page
      .getByRole("heading", { name: "身份验证完成", exact: true })
      .waitFor();
    const codes = (await page.locator("#codes").textContent()).trim();
    if (codes) f.codes = codes.split("\n");
    await page.locator("#continue").click();
  }
  await page
    .getByRole("heading", { name: "确认本次授权", exact: true })
    .waitFor();
  if (tenant) await page.locator("#tenant").selectOption(tenant);
  await page.getByRole("button", { name: "同意并继续", exact: true }).click();
  await page.waitForURL(
    (url) =>
      url.origin === "http://console.localhost:3003" &&
      /^#workspace=[a-f0-9]{32}$/.test(url.hash),
  );
  await page.getByRole("heading", { name: "空间概览", exact: true }).waitFor();
  await page.getByRole("heading", { name: "我的空间", exact: true }).waitFor();
  return (await bff(page, "/v1/me")).body;
}
async function bff(page, url, { method = "GET", body, headers = {} } = {}) {
  return page.evaluate(
    async (args) => {
      const context = location.hash.slice("#workspace=".length);
      const session = await fetch("/session", {
        headers: { "X-Console-Context": context },
      });
      let csrf = "";
      try {
        csrf = (await session.json()).csrf || "";
      } catch {}
      const response = await fetch("/bff" + args.url, {
        method: args.method,
        headers: {
          "X-Console-Context": context,
          ...(args.method !== "GET"
            ? { "Content-Type": "application/json", "X-CSRF-Token": csrf }
            : {}),
          ...args.headers,
        },
        body: args.body === undefined ? undefined : JSON.stringify(args.body),
      });
      let body;
      try {
        body = await response.json();
      } catch {}
      return {
        status: response.status,
        body,
        etag: response.headers.get("ETag"),
      };
    },
    { url, method, body, headers },
  );
}
async function nav(page, name) {
  await page
    .getByRole("navigation", { name: "主导航" })
    .getByRole("button", { name, exact: true })
    .click();
  await page.getByRole("heading", { name, exact: true }).waitFor();
  await page
    .getByRole("status")
    .filter({ hasText: "正在载入资料…" })
    .waitFor({ state: "hidden" });
}
async function submit(page) {
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "保存并继续", exact: true })
    .click();
}
async function reauthorize(f, page, tenant) {
  await page
    .getByRole("link", { name: "重新确认空间授权", exact: true })
    .waitFor();
  return login(f, page, tenant);
}
async function createOrg(page, name, parent) {
  await page.getByRole("button", { name: "创建组织", exact: true }).click();
  await page.getByLabel("组织名称").fill(name);
  if (!parent) {
    const tenant = (await bff(page, "/v1/me")).body.tenant_id;
    parent = (await bff(page, "/v1/tenants/" + tenant)).body.root_id;
  }
  await page
    .getByRole("status")
    .filter({ hasText: "正在读取上级组织" })
    .waitFor({ state: "hidden" });
  await page.getByLabel("上级组织", { exact: true }).selectOption(parent);
  await submit(page);
  await page.getByRole("dialog").waitFor({ state: "hidden" });
  await page.getByRole("table").getByText(name, { exact: true }).waitFor();
  return (
    await bff(
      page,
      "/v1/tenants/" +
        (await bff(page, "/v1/me")).body.tenant_id +
        "/organizations",
    )
  ).body.items.find((x) => x.name === name).id;
}
(async () => {
  try {
    browser = await chromium.launch({
      executablePath: process.env.CHROMIUM_PATH || "/usr/bin/chromium",
      headless: true,
      args: ["--no-sandbox"],
    });
    stage = "register-fixtures";
    const owner = await register(true),
      guest = await register();
    const p = owner.page;
    stage = "real-react-login";
    await owner.page.goto("http://console.localhost:3003/");
    assert.equal(
      await owner.page.evaluate(
        async () => (await fetch("/bff-openapi.yaml")).status,
      ),
      200,
    );
    const me = await login(owner);
    owner.user = me.user_id;
    const tenantA = me.tenant_id;
    const spaces = (await bff(p, "/v1/tenants")).body.items;
    assert.equal(spaces.length, 2);
    const tenantB = spaces.find((x) => x.id !== tenantA).id;
    stage = "independent-tab-authorizations";
    const second = await owner.context.newPage();
    watch(second);
    await login(owner, second, tenantB);
    const contextB = new URL(second.url()).hash;
    assert.equal((await bff(p, "/v1/me")).body.tenant_id, tenantA);
    assert.equal((await bff(second, "/v1/me")).body.tenant_id, tenantB);
    stage = "keys-sql-permissions-and-refresh";
    execFileSync("scripts/console", ["verify-fixture", "--user", owner.user], {
      cwd: root,
      stdio: "pipe",
    });
    assert.equal((await bff(p, "/v1/me")).status, 200);
    const refreshed = await p.evaluate(
      async () =>
        await (
          await fetch("/session", {
            headers: { "X-Console-Context": location.hash.slice(11) },
          })
        ).json(),
    );
    assert(new Date(refreshed.expires_at) > new Date());
    stage = "csrf-cookie-gateway-boundaries";
    const missing = await p.evaluate(async () => {
      const context = location.hash.slice(11);
      return (
        await fetch("/bff/v1/tenants", {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            "X-Console-Context": context,
          },
          body: "{}",
        })
      ).status;
    });
    assert.equal(missing, 403);
    assert.equal(
      (await bff(p, "/v1/me", { headers: { "X-Tenant-ID": tenantB } })).status,
      403,
    );
    assert.equal((await bff(p, "/v1/tenants/" + tenantB)).status, 404);
    assert.equal(
      (
        await bff(p, "/v1/me", {
          headers: { Authorization: "Bearer user-supplied" },
        })
      ).status,
      400,
    );
    assert.equal(
      await p.evaluate(async () => (await fetch("/bff/admin/clients")).status),
      404,
    );
    const foreign = await browser.newContext();
    const foreignPage = await foreign.newPage();
    await foreignPage.goto("http://console.localhost:3003/");
    const copiedContext = new URL(p.url()).hash.slice(11);
    assert.equal(
      await foreignPage.evaluate(
        async (context) =>
          (
            await fetch("/session", {
              headers: { "X-Console-Context": context },
            })
          ).status,
        copiedContext,
      ),
      401,
    );
    await foreign.close();
    stage = "create-real-organizations";
    await nav(p, "组织");
    const orgA = await createOrg(p, "设计团队");
    const orgB = await createOrg(p, "产品团队");
    await createOrg(p, "交互小组", orgA);
    const untrusted = "<img src=x onerror=window.__unsafe=1>";
    await createOrg(p, untrusted);
    assert.equal(await p.locator("td img").count(), 0);
    assert.equal(await p.evaluate(() => window.__unsafe === 1), false);
    stage = "step-up-without-mutation-replay";
    const identity = (await bff(p, "/v1/me")).body;
    execFileSync(
      "python3",
      ["tests/api/fixtures.py", "age-session", owner.user, identity.session_id],
      { cwd: root, stdio: "pipe" },
    );
    await nav(p, "邀请");
    await p.getByRole("button", { name: "创建邀请", exact: true }).click();
    await p.getByLabel("受邀邮箱").fill(guest.email);
    await submit(p);
    await p.getByRole("alert").filter({ hasText: "请重新验证身份" }).waitFor();
    assert.equal(
      (await bff(p, "/v1/tenants/" + tenantA + "/invitations")).body.items
        .length,
      0,
    );
    await login(owner, p, tenantA, true);
    assert.equal(
      (await bff(p, "/v1/tenants/" + tenantA + "/invitations")).body.items
        .length,
      0,
    );
    stage = "one-time-invitation-ui";
    await nav(p, "邀请");
    await p.getByRole("button", { name: "创建邀请", exact: true }).click();
    await p.getByLabel("受邀邮箱").fill(guest.email);
    await submit(p);
    await p
      .getByRole("heading", { name: "保存一次性邀请凭证", exact: true })
      .waitFor();
    const invitation = await p.getByTestId("invitation-secret").textContent();
    assert.equal(invitation.length, 43);
    await p.getByRole("button", { name: "我已安全保存", exact: true }).click();
    assert.equal(await p.getByTestId("invitation-secret").count(), 0);
    assert.equal(
      (await bff(p, "/v1/tenants/" + tenantA + "/invitations")).body.items.some(
        (x) => "token" in x,
      ),
      false,
    );
    stage = "accept-invitation-ui";
    await login(guest);
    await guest.page
      .getByRole("button", { name: "接受邀请", exact: true })
      .click();
    await guest.page.getByLabel("一次性邀请凭证").fill(invitation);
    await submit(guest.page);
    await guest.page
      .getByRole("status")
      .filter({ hasText: "已加入空间" })
      .waitFor();
    await p.getByRole("button", { name: "刷新资料", exact: true }).click();
    await reauthorize(owner, p, tenantA);
    stage = "late-old-space-response";
    const contextA = new URL(p.url()).hash;
    let release, started;
    const gate = new Promise((r) => (release = r)),
      seen = new Promise((r) => (started = r));
    let routeFailed = false;
    await p.route(
      "**/bff/v1/tenants/" + tenantA + "/members?*",
      async (route) => {
        try {
          const target = new URL(route.request().url());
          target.hostname = "127.0.0.1";
          const headers = await route.request().allHeaders();
          headers.host = "console.localhost:3003";
          const response = await route.fetch({
            url: target.toString(),
            headers,
          });
          started();
          await gate;
          try {
            await route.fulfill({ response });
          } catch {}
        } catch {
          routeFailed = true;
          started();
          try {
            await route.abort();
          } catch {}
        }
      },
    );
    await p
      .getByRole("navigation", { name: "主导航" })
      .getByRole("button", { name: "成员", exact: true })
      .click();
    await seen;
    assert.equal(routeFailed, false);
    await p.evaluate((hash) => (location.hash = hash), contextB);
    await p.getByRole("heading", { name: "我的空间", exact: true }).waitFor();
    release();
    await wait(150);
    assert.equal(await p.getByRole("table").count(), 0);
    assert.equal((await bff(p, "/v1/me")).body.tenant_id, tenantB);
    await p.unroute("**/bff/v1/tenants/" + tenantA + "/members?*");
    await p.evaluate((hash) => (location.hash = hash), contextA);
    await p.getByRole("heading", { name: "我的空间", exact: true }).waitFor();
    assert.equal((await bff(p, "/v1/me")).body.tenant_id, tenantA);
    stage = "guest-has-no-owner-interface";
    const shared = await guest.context.newPage();
    watch(shared);
    await login(guest, shared, tenantA);
    assert.equal(
      await shared
        .getByRole("navigation", { name: "主导航" })
        .getByRole("button", { name: "成员", exact: true })
        .count(),
      0,
    );
    assert.equal(
      (await bff(shared, "/v1/tenants/" + tenantA + "/members")).status,
      403,
    );
    assert.equal(
      (
        await bff(shared, "/v1/tenants/" + tenantA + "/invitations", {
          method: "POST",
          body: { email: owner.email },
        })
      ).status,
      403,
    );
    stage = "organization-membership-no-role-grant";
    await nav(p, "组织");
    await p.getByRole("button", { name: "关联成员", exact: true }).click();
    const memberRows = (await bff(p, "/v1/tenants/" + tenantA + "/members"))
      .body.items;
    const guestMember = memberRows.find((x) => x.email === guest.email).id;
    await p.getByLabel("选择成员").selectOption(guestMember);
    await p.getByLabel("选择组织").selectOption(orgA);
    await p
      .getByLabel("我确认关联所选成员与组织，归属不授予角色", { exact: true })
      .check();
    await submit(p);
    await reauthorize(owner, p, tenantA);
    assert.equal((await bff(shared, "/v1/me")).status, 401);
    await login(guest, shared, tenantA);
    assert.equal(
      (await bff(shared, "/v1/tenants/" + tenantA + "/members")).status,
      403,
    );
    stage = "last-owner-guard";
    await nav(p, "成员");
    const ownRow = p.getByRole("row").filter({ hasText: owner.email });
    await ownRow.getByRole("button", { name: "停用成员", exact: true }).click();
    await p
      .getByRole("dialog")
      .getByRole("button", { name: "确认操作", exact: true })
      .click();
    await p
      .getByRole("alert")
      .filter({ hasText: "至少需要保留一位" })
      .waitFor();
    await p.getByRole("button", { name: "关闭对话框", exact: true }).click();
    stage = "suspend-restore-does-not-revive-old-grant";
    await p
      .getByRole("row")
      .filter({ hasText: guest.email })
      .getByRole("button", { name: "停用成员", exact: true })
      .click();
    await p
      .getByRole("dialog")
      .getByRole("button", { name: "确认操作", exact: true })
      .click();
    await reauthorize(owner, p, tenantA);
    assert.equal((await bff(shared, "/v1/me")).status, 401);
    assert.equal((await bff(guest.page, "/v1/me")).status, 200);
    await nav(p, "成员");
    await p
      .getByRole("row")
      .filter({ hasText: guest.email })
      .getByRole("button", { name: "恢复成员", exact: true })
      .click();
    await p
      .getByRole("dialog")
      .getByRole("button", { name: "确认操作", exact: true })
      .click();
    await reauthorize(owner, p, tenantA);
    assert.equal((await bff(shared, "/v1/me")).status, 401);
    await login(guest, shared, tenantA);
    assert.equal((await bff(shared, "/v1/me")).status, 200);
    stage = "organization-move-preview-and-execution";
    await nav(p, "组织");
    await p
      .getByRole("row")
      .filter({ has: p.locator("strong").filter({ hasText: /^设计团队$/ }) })
      .getByRole("button", { name: "移动组织", exact: true })
      .click();
    await p.getByLabel("上级组织").selectOption(orgB);
    await p.getByRole("button", { name: "预览移动", exact: true }).click();
    await p
      .getByRole("status")
      .filter({ hasText: "将移动 2 个组织" })
      .waitFor();
    await p
      .getByLabel("我确认按当前预览移动组织及其下级", { exact: true })
      .check();
    await p.getByRole("button", { name: "确认移动", exact: true }).click();
    await reauthorize(owner, p, tenantA);
    stage = "new-space-and-audit-ui";
    await p.getByRole("button", { name: "创建空间", exact: true }).click();
    await p.getByLabel("空间名称").fill("控制台验收空间");
    await p.getByLabel("空间标识").fill("console-" + owner.user);
    await submit(p);
    await p
      .getByRole("heading", { name: "控制台验收空间", exact: true })
      .waitFor();
    await p.screenshot({
      path: path.join(root, "docs/ui/console-overview.png"),
      fullPage: true,
    });
    await nav(p, "成员");
    await p.getByText(guest.email, { exact: true }).waitFor();
    await p.screenshot({
      path: path.join(root, "docs/ui/console-members.png"),
      fullPage: true,
    });
    await nav(p, "审计");
    await p.getByRole("table").getByText("移动组织", { exact: true }).waitFor();
    await p.screenshot({
      path: path.join(root, "docs/ui/console-audit.png"),
      fullPage: true,
    });
    await nav(p, "空间概览");
    await p.setViewportSize({ width: 390, height: 844 });
    assert.equal(
      await p.evaluate(() => document.documentElement.scrollWidth > innerWidth),
      false,
    );
    await p.screenshot({
      path: path.join(root, "docs/ui/console-mobile.png"),
      fullPage: true,
    });
    await p.setViewportSize({ width: 1280, height: 900 });
    stage = "local-context-logout-isolation";
    await second
      .getByRole("button", { name: "退出此标签页", exact: true })
      .click();
    await second
      .getByRole("link", { name: "登录管理控制台", exact: true })
      .waitFor();
    assert.equal((await bff(p, "/v1/me")).status, 200);
    stage = "own-session-revocation-ui";
    await login(guest, shared, tenantA);
    await nav(shared, "安全会话");
    const currentRow = shared.getByRole("row").filter({ hasText: "当前会话" });
    await currentRow
      .getByRole("button", { name: "撤销会话", exact: true })
      .click();
    await shared
      .getByRole("dialog")
      .getByRole("button", { name: "确认操作", exact: true })
      .click();
    await shared
      .getByRole("link", { name: "重新确认空间授权", exact: true })
      .waitFor();
    assert.equal((await bff(p, "/v1/me")).status, 200);
    stage = "browser-credential-and-cookie-boundaries";
    for (const page of [p, second, shared])
      assert.deepEqual(
        await page.evaluate(() => [localStorage.length, sessionStorage.length]),
        [0, 0],
      );
    const cookies = await owner.context.cookies();
    assert(cookies.every((x) => x.httpOnly));
    assert(
      cookies
        .filter((x) => x.name.startsWith("local_console_"))
        .every((x) => x.domain === "console.localhost"),
    );
    assert.deepEqual(leaks, []);
    assert.deepEqual(pageErrors, []);
    success =
      "Console browser acceptance passed: real React/BFF/API, two-tab tenant isolation, stale response rejection, CSRF, SQL/key/network boundaries, refresh, MFA step-up, invitations, owner/member controls, organization move, audit, sessions and desktop/mobile";
  } finally {
    clearInterval(progress);
    if (browser) await browser.close();
    if (fixtures.length)
      execFileSync(
        "python3",
        ["tests/api/cleanup.py", ...fixtures.map((x) => x.email)],
        { cwd: root, stdio: "pipe" },
      );
    if (success) console.log(success);
  }
})().catch((error) => {
  console.error(
    "Console acceptance failed: stage=" +
      stage +
      ", error=" +
      error.name +
      " (sensitive details withheld)",
  );
  process.exitCode = 1;
});
