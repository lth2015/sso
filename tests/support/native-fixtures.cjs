// Native-client acceptance: actual loopback callback, Hydra code+PKCE and HTTP APIs.
// Bearer/proof material stays in process memory and is never logged.
const { chromium } = require("playwright");
const fs = require("node:fs/promises"),
  path = require("node:path"),
  crypto = require("node:crypto"),
  http = require("node:http"),
  assert = require("node:assert/strict");
const { execFileSync } = require("node:child_process");
const root = path.resolve(__dirname, "../.."),
  mailbox = path.join(root, ".local/mailbox");
const created = new Set(),
  fixtures = [],
  covered = new Set();
let stage = "startup",
  browser,
  callbackServer,
  checks = 0,
  successMessage = "",
  applicationTestClient;
const wait = (ms) => new Promise((r) => setTimeout(r, ms));
function request(port, host, method, url, body, headers = {}) {
  return new Promise((resolve, reject) => {
    const req = http.request(
      {
        host: "127.0.0.1",
        port,
        path: url,
        method,
        headers: {
          Host: host,
          ...(body !== undefined
            ? { "Content-Length": Buffer.byteLength(body) }
            : {}),
          ...headers,
        },
      },
      (res) => {
        let data = "";
        res.on("data", (chunk) => {
          data += chunk;
          if (data.length > 1_000_000) req.destroy();
        });
        res.on("end", () => {
          let parsed;
          try {
            parsed = JSON.parse(data);
          } catch {}
          resolve({
            status: res.statusCode,
            headers: res.headers,
            body: parsed,
            text: data,
          });
        });
      },
    );
    req.setTimeout(12000, () => req.destroy());
    req.on("error", () => reject(new Error("local request failed")));
    if (body !== undefined) req.write(body);
    req.end();
  });
}
function operationPath(url) {
  return new URL("http://local" + url).pathname
    .replace(/\/tenants\/[a-f0-9]{32}/, "/tenants/{tenant}")
    .replace(/\/organizations\/[a-f0-9]{32}/, "/organizations/{org}")
    .replace(/\/members\/[a-f0-9]{32}/, "/members/{member}")
    .replace(/\/roles\/[a-f0-9]{32}/, "/roles/{role}")
    .replace(/\/invitations\/[a-f0-9]{32}/, "/invitations/{invitation}")
    .replace(/\/sessions\/[a-f0-9]{32}/, "/sessions/{session}")
    .replace(/\/applications\/[a-f0-9]{32}/, "/applications/{application}")
    .replace(/\/clients\/[a-f0-9]{32}/, "/clients/{client}")
    .replace(
      /\/service-principals\/[a-f0-9]{32}/,
      "/service-principals/{principal}",
    )
    .replace(/\/credentials\/[a-f0-9]{32}/, "/credentials/{credential}");
}
async function api(
  method,
  url,
  token,
  body,
  headers = {},
  status = 200,
  raw = false,
) {
  const h = { ...headers };
  if (token) h.Authorization = "Bearer " + token;
  if (body !== undefined) h["Content-Type"] ??= "application/json";
  const response = await request(
    8083,
    "api.localhost:8083",
    method,
    url,
    body === undefined ? undefined : raw ? body : JSON.stringify(body),
    h,
  );
  if (
    !(Array.isArray(status)
      ? status.includes(response.status)
      : response.status === status)
  )
    console.error(
      "Unexpected status: " +
        method +
        " " +
        operationPath(url) +
        " expected=" +
        JSON.stringify(status) +
        " received=" +
        response.status,
    );
  if (Array.isArray(status)) assert(status.includes(response.status));
  else assert.equal(response.status, status);
  assert.equal(response.headers["cache-control"], "no-store");
  const sample = {
    path: operationPath(url),
    method,
    status: response.status,
    body: response.body,
    content_type: response.headers["content-type"],
    request_id: response.headers["x-request-id"],
    request_body: body,
  };
  covered.add(method.toLowerCase() + " " + sample.path);
  execFileSync("python3", ["tests/api/validate_contract.py"], {
    cwd: root,
    input: JSON.stringify(sample),
    stdio: ["pipe", "pipe", "pipe"],
  });
  checks++;
  return response;
}
async function mail(email) {
  for (let i = 0; i < 100; i++) {
    for (const name of await fs.readdir(mailbox)) {
      if (!/^[a-f0-9]{32}\.json$/.test(name)) continue;
      const file = path.join(mailbox, name);
      let data;
      try {
        data = JSON.parse(await fs.readFile(file, "utf8"));
      } catch {
        continue;
      }
      if (data.recipient === email && data.purpose === "register") {
        created.add(file);
        return data.token;
      }
    }
    await wait(150);
  }
  throw new Error("fixture proof unavailable");
}
function totp(secret) {
  const alpha = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
  let bits = "";
  for (const c of secret) bits += alpha.indexOf(c).toString(2).padStart(5, "0");
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
async function register() {
  const f = {
    email:
      "browser-" + crypto.randomBytes(16).toString("hex") + "@example.invalid",
    password: "API private local passphrase 123!",
    codes: [],
    used: 0,
  };
  fixtures.push(f);
  f.context = await browser.newContext();
  f.page = await f.context.newPage();
  const page = f.page;
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
  execFileSync(
    "scripts/sso",
    ["enable", "--email", f.email, "--application", "local-platform"],
    { cwd: root, stdio: "pipe" },
  );
  return f;
}
async function authorize(
  f,
  tenant,
  scopes = "openid offline_access platform:read platform:write",
  force = false,
  client = "local-platform-cli",
) {
  const verifier = crypto.randomBytes(32).toString("base64url"),
    state = crypto.randomBytes(32).toString("base64url"),
    nonce = crypto.randomBytes(32).toString("base64url");
  const params = new URLSearchParams({
    client_id: client,
    response_type: "code",
    redirect_uri: "http://127.0.0.1:9876/callback",
    scope: scopes,
    audience: "https://api.sso.local",
    state,
    nonce,
    code_challenge: crypto
      .createHash("sha256")
      .update(verifier)
      .digest("base64url"),
    code_challenge_method: "S256",
  });
  if (force) params.set("max_age", "0");
  const page = f.page;
  await page.goto("http://auth.localhost:4464/oauth2/auth?" + params);
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
      url.origin === "http://127.0.0.1:9876" && url.pathname === "/callback",
  );
  const callback = new URL(page.url());
  assert.equal(callback.searchParams.get("state"), state);
  const code = callback.searchParams.get("code");
  assert(code);
  await page.evaluate(() => history.replaceState(null, "", "/callback"));
  const response = await request(
    4464,
    "auth.localhost:4464",
    "POST",
    "/oauth2/token",
    new URLSearchParams({
      grant_type: "authorization_code",
      client_id: client,
      redirect_uri: "http://127.0.0.1:9876/callback",
      code,
      code_verifier: verifier,
    }).toString(),
    { "Content-Type": "application/x-www-form-urlencoded" },
  );
  assert.equal(response.status, 200);
  assert(response.body.access_token);
  return response.body;
}

module.exports = {
  root,
  request,
  api,
  register,
  authorize,
  fixtures,
  wait,
  diagnostics: () => ({ stage }),
  async start() {
    callbackServer = http.createServer((req, res) => {
      if (req.method !== "GET" || !req.url.startsWith("/callback?")) {
        res.writeHead(404).end();
        return;
      }
      res.writeHead(200, {
        "Content-Type": "text/html; charset=utf-8",
        "Cache-Control": "no-store",
        "Referrer-Policy": "no-referrer",
      });
      res.end(
        "<!doctype html><meta charset=utf-8><title>授权完成</title><p>本地客户端收到授权结果。</p>",
      );
    });
    await new Promise((resolve, reject) => {
      callbackServer.once("error", reject);
      callbackServer.listen(9876, "127.0.0.1", resolve);
    });
    browser = await chromium.launch({
      executablePath: process.env.CHROMIUM_PATH || "/usr/bin/chromium",
      headless: true,
      args: ["--no-sandbox"],
    });
  },
  async close() {
    if (browser) await browser.close();
    if (callbackServer) await new Promise((r) => callbackServer.close(r));
    if (fixtures.length)
      execFileSync(
        "python3",
        ["tests/api/cleanup.py", ...fixtures.map((f) => f.email)],
        { cwd: root, stdio: "pipe" },
      );
    for (const file of created)
      await fs.unlink(file).catch((e) => {
        if (e.code !== "ENOENT") throw e;
      });
  },
};
