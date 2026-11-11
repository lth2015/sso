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
  stage = "startup";
const rejected = [];
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
        response.status() >= 400 &&
        ["127.0.0.1", "auth.localhost", "console.localhost"].includes(
          u.hostname,
        )
      )
        rejected.push({
          path: u.pathname.replace(/[a-f0-9]{32}/g, "{id}"),
          status: response.status(),
        });
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
  stage = "registration-request";
  await p.goto("http://127.0.0.1:8080/account");
  await p.getByLabel("邮箱地址").fill(f.email);
  await p.getByRole("button", { name: "发送验证说明", exact: true }).click();
  await p.getByRole("status").filter({ hasText: "若地址符合条件" }).waitFor();
  const proof = await mail(f.email);
  stage = "registration-completion";
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
  stage = "registration-ready";
  return f;
}
async function login(f, page = f.page, tenant, force = false) {
  stage = "login-start";
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
  stage = "login-callback";
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
        contentType: response.headers.get("Content-Type"),
        requestID: response.headers.get("X-Request-ID"),
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
  if (parent) await page.getByLabel("上级组织").selectOption(parent);
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
module.exports = {
  diagnostics: () => ({ step: stage, rejected: rejected.slice(-5) }),
  fixtures,
  pageErrors,
  leaks,
  register,
  login,
  bff,
  nav,
  submit,
  root,
  async start() {
    browser = await chromium.launch({
      executablePath: process.env.CHROMIUM_PATH || "/usr/bin/chromium",
      headless: true,
      args: ["--no-sandbox"],
    });
  },
  async close() {
    if (browser) await browser.close();
    if (fixtures.length)
      execFileSync(
        "python3",
        ["tests/api/cleanup.py", ...fixtures.map((x) => x.email)],
        { cwd: root, stdio: "pipe" },
      );
  },
};
