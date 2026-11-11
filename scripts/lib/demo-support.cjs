// Local-only demo orchestration: real account proofs, MFA, OAuth and public API.
const fs = require("node:fs/promises"),
  path = require("node:path"),
  crypto = require("node:crypto"),
  { execFileSync } = require("node:child_process"),
  assert = require("node:assert/strict"),
  s = require("../../tests/support/console-fixtures.cjs");
const root = s.root,
  directory = path.join(root, ".local/demo"),
  manifestPath = path.join(directory, "manifest.json");
const idPattern = /^[a-f0-9]{32}$/,
  emailPattern = /^browser-[a-f0-9]{32}@example\.invalid$/;
const wait = (ms) => new Promise((r) => setTimeout(r, ms));
async function fingerprint() {
  return (
    await fs.readFile(path.join(root, ".local/sso-key.id"), "utf8")
  ).trim();
}
async function privateDirectory() {
  await fs.mkdir(directory, { recursive: true, mode: 0o700 });
  const stat = await fs.lstat(directory);
  assert(stat.isDirectory() && !stat.isSymbolicLink());
  await fs.chmod(directory, 0o700);
}
async function writePrivate(name, text) {
  await privateDirectory();
  const temp = path.join(
    directory,
    ".write-" + crypto.randomBytes(8).toString("hex"),
  );
  await fs.writeFile(temp, text, { mode: 0o600, flag: "wx" });
  await fs.rename(temp, path.join(directory, name));
}
async function save(manifest) {
  await writePrivate("manifest.json", JSON.stringify(manifest, null, 2) + "\n");
}
async function load(required = true) {
  let text;
  try {
    const stat = await fs.lstat(manifestPath);
    assert(stat.isFile() && !stat.isSymbolicLink());
    text = await fs.readFile(manifestPath, "utf8");
  } catch (e) {
    if (e.code === "ENOENT" && !required) return null;
    throw new Error("demo manifest unavailable");
  }
  const m = JSON.parse(text);
  assert(
    m.format === 1 && m.tool === "scripts/demo" && idPattern.test(m.runId),
  );
  assert(m.fingerprint === (await fingerprint()));
  assert(
    Array.isArray(m.accounts) &&
      m.accounts.length <= 2 &&
      m.accounts.every(
        (a) =>
          emailPattern.test(a.email) && ["owner", "auditor"].includes(a.role),
      ),
  );
  assert(new Set(m.accounts.map((a) => a.email)).size === m.accounts.length);
  return m;
}
function watch(page, errors) {
  page.on("pageerror", () => errors.push("page error"));
}
async function api(
  page,
  url,
  { method = "GET", body, headers = {} } = {},
  expected = 200,
) {
  const response = await s.bff(page, url, { method, body, headers });
  assert.equal(response.status, expected, "demo API status mismatch");
  return response;
}
async function mail(email) {
  for (let attempt = 0; attempt < 100; attempt++) {
    for (const name of await fs.readdir(path.join(root, ".local/mailbox"))) {
      if (!/^[a-f0-9]{32}\.json$/.test(name)) continue;
      let message;
      try {
        message = JSON.parse(
          await fs.readFile(path.join(root, ".local/mailbox", name), "utf8"),
        );
      } catch {
        continue;
      }
      if (message.recipient === email && message.purpose === "register")
        return message.token;
    }
    await wait(150);
  }
  throw new Error("private local registration proof unavailable");
}
async function register(browser, m, role, errors) {
  const account = {
    role,
    email:
      "browser-" + crypto.randomBytes(16).toString("hex") + "@example.invalid",
    password: "Demo-" + crypto.randomBytes(24).toString("base64url"),
    codes: [],
    used: 0,
  };
  m.accounts.push(account);
  await save(m);
  const context = await browser.newContext(),
    page = await context.newPage();
  watch(page, errors);
  const f = { ...account, context, page };
  await page.goto("http://127.0.0.1:8080/account");
  await page.getByLabel("邮箱地址").fill(f.email);
  await page.getByRole("button", { name: "发送验证说明", exact: true }).click();
  await page
    .getByRole("status")
    .filter({ hasText: "若地址符合条件" })
    .waitFor();
  const proof = await mail(f.email);
  await page.goto(
    "http://127.0.0.1:8080/account#purpose=register&token=" + proof,
  );
  await page.getByLabel("设置新密码", { exact: true }).fill(f.password);
  await page.getByLabel("再次输入密码").fill(f.password);
  await page
    .getByRole("button", { name: "验证邮箱并创建账户", exact: true })
    .click();
  await page
    .getByRole("status")
    .filter({ hasText: "邮箱验证与账户创建完成" })
    .waitFor();
  execFileSync("scripts/console", ["enable", "--email", f.email], {
    cwd: root,
    stdio: "pipe",
  });
  const me = await s.login(f);
  Object.assign(account, {
    user: me.user_id,
    totpSecret: f.totpSecret,
    codes: f.codes,
    lastStep: Math.floor(Date.now() / 30000),
  });
  await save(m);
  f.user = me.user_id;
  return f;
}
async function login(browser, m, account, errors, tenant = m.primary.id) {
  const context = await browser.newContext(),
    page = await context.newPage();
  watch(page, errors);
  const f = { ...account, context, page };
  await page.goto("http://console.localhost:3003/login");
  await page
    .getByRole("heading", { name: "登录你的账户", exact: true })
    .waitFor();
  await page.getByLabel("邮箱地址").fill(account.email);
  await page.getByLabel("密码", { exact: true }).fill(account.password);
  await page.getByRole("button", { name: "继续", exact: true }).click();
  await page
    .getByRole("heading", { name: "验证你的身份", exact: true })
    .waitFor();
  let passed = false;
  for (let attempt = 0; attempt < 2; attempt++) {
    if (attempt || account.lastStep >= Math.floor(Date.now() / 30000))
      await wait(30100 - (Date.now() % 30000));
    const step = Math.floor(Date.now() / 30000);
    await page
      .getByLabel("动态验证码", { exact: true })
      .fill(s.totp(account.totpSecret));
    await page.getByRole("button", { name: "验证身份", exact: true }).click();
    await page.waitForFunction(
      () =>
        document.querySelector("h1")?.textContent === "身份验证完成" ||
        Boolean(document.getElementById("error")?.textContent),
    );
    if (
      await page
        .getByRole("heading", { name: "身份验证完成", exact: true })
        .count()
    ) {
      account.lastStep = step;
      await save(m);
      passed = true;
      break;
    }
  }
  assert(passed, "demo MFA could not be verified");
  await page.locator("#continue").click();
  await page
    .getByRole("heading", { name: "确认本次授权", exact: true })
    .waitFor();
  await page.locator("#tenant").selectOption(tenant);
  await page.getByRole("button", { name: "同意并继续", exact: true }).click();
  await page.getByRole("heading", { name: "空间概览", exact: true }).waitFor();
  const me = (await api(page, "/v1/me")).body;
  assert.equal(me.user_id, account.user);
  assert.equal(me.tenant_id, tenant);
  return f;
}
async function authorizeApp(f, origin, tenant) {
  const page = await f.context.newPage();
  await page.goto(origin + "/login");
  await page
    .getByRole("heading", { name: "确认本次授权", exact: true })
    .waitFor();
  assert.equal(
    await page
      .getByRole("heading", { name: "登录你的账户", exact: true })
      .count(),
    0,
  );
  await page.locator("#tenant").selectOption(tenant);
  await page.getByRole("button", { name: "同意并继续", exact: true }).click();
  await page.locator("#session").waitFor({ state: "visible" });
  assert.equal(await page.locator("#tenant").textContent(), tenant);
  assert.equal(await page.locator("#user").textContent(), f.user);
  return page;
}
async function presenter(m) {
  let text =
    "# 周五演示登录资料（本地私有，不进入 Git/文档包）\n\n控制台：http://console.localhost:3003\n\n先将 TOTP 密钥导入自己的认证器，SHA-1、6 位、30 秒周期；TOTP 时间片已使用时等下一组。以下恢复码只能使用一次，请自行标记使用情况。\n";
  for (const a of m.accounts)
    text +=
      "\n## " +
      (a.role === "owner" ? "管理员" : "只读审计员") +
      "\n\n邮箱：" +
      a.email +
      "\n\n密码：" +
      a.password +
      "\n\nTOTP 密钥：" +
      a.totpSecret +
      "\n\n恢复码（每个仅限一次）：\n\n```text\n" +
      a.codes.join("\n") +
      "\n```\n";
  await writePrivate("presenter.md", text);
  await writePrivate(
    "dataset.md",
    "# 演示数据（无登录凭证）\n\n主空间：星河科技\n\n隔离空间：远山合作社\n\n主空间 ID：" +
      m.primary.id +
      "\n\n隔离空间 ID：" +
      m.secondary.id +
      "\n\n主根组织 ID：" +
      m.primary.root_id +
      "\n\nAPI 演示：选择 listOrganizations，租户参数使用当前空间；选择 getMySecurity/listMySecurityEvents 查看个人安全。\n\n登录资料在 presenter.md；讲解主线见 docs/runbooks/friday-demo.md。\n",
  );
}
module.exports = {
  root,
  directory,
  manifestPath,
  save,
  load,
  fingerprint,
  writePrivate,
  register,
  login,
  api,
  authorizeApp,
  presenter,
  watch,
  s,
  assert,
  fs,
  path,
  crypto,
  execFileSync,
};
