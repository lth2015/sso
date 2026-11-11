// Real console/profile observation; controlled late/error data never leaves memory.
const s = require("../support/console-fixtures.cjs"),
  assert = require("node:assert/strict"),
  { execFileSync } = require("node:child_process"),
  path = require("node:path");
let phase = "startup",
  checks = 0;
const secrets = [],
  errors = [],
  started = new Date().toISOString();
const progress = setInterval(
  () => console.log("Protocol observation UI phase: " + phase),
  20000,
);
const appName = "协议观察演示",
  spName = "协议核对机器人",
  keyName = "只读观察凭证";
function norm(url) {
  return url
    .split("?")[0]
    .replace(/\/tenants\/[a-f0-9]{32}/, "/tenants/{tenant}")
    .replace(/\/applications\/[a-f0-9]{32}/, "/applications/{application}")
    .replace(/\/clients\/[a-f0-9]{32}/, "/clients/{client}")
    .replace(
      /\/service-principals\/[a-f0-9]{32}/,
      "/service-principals/{principal}",
    )
    .replace(/\/credentials\/[a-f0-9]{32}/, "/credentials/{credential}");
}
function validate(url, method, r, body) {
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
}
async function call(p, method, url, body, status = 200, etag) {
  const r = await s.bff(p, url, {
    method,
    body,
    headers: etag ? { "If-Match": etag } : {},
  });
  assert.equal(r.status, status);
  validate(url, method, r, body);
  if (typeof r.body?.token === "string") secrets.push(r.body.token);
  if (typeof r.body?.secret_key === "string") secrets.push(r.body.secret_key);
  return r;
}
function fixture(args) {
  execFileSync("python3", ["tests/applications/managed_fixture.py", ...args], {
    cwd: s.root,
    stdio: "pipe",
  });
}
async function forward(route) {
  const u = new URL(route.request().url());
  assert.equal(u.origin, "http://console.localhost:3003");
  return route.fetch({
    url: "http://127.0.0.1:3003" + u.pathname + u.search,
    headers: {
      ...(await route.request().allHeaders()),
      host: "console.localhost:3003",
    },
    timeout: 5000,
    maxRedirects: 0,
  });
}
async function record(route, response) {
  const body = await response.json(),
    h = response.headers();
  validate(new URL(route.request().url()).pathname.slice(4), "GET", {
    status: response.status(),
    body,
    contentType: h["content-type"],
    requestID: h["x-request-id"],
  });
  return body;
}
function absent(value) {
  const text = typeof value === "string" ? value : JSON.stringify(value);
  for (const secret of secrets) assert(!text.includes(secret));
}
async function inspect(region, text) {
  await region
    .getByRole("button", { name: "只读核对协议登记", exact: true })
    .click();
  await region.getByRole("status").filter({ hasText: text }).waitFor();
}
async function hold(p, url) {
  let seen, release, finish;
  const arrived = new Promise((r) => (seen = r)),
    gate = new Promise((r) => (release = r)),
    done = new Promise((r) => (finish = r)),
    pattern = "**/bff" + url;
  const handler = async (route) => {
    try {
      const r = await forward(route),
        body = await record(route, r);
      assert.equal(r.status(), 200);
      seen(body);
      await gate;
      await route.fulfill({ response: r }).catch(() => {});
    } catch {
      errors.push("held observation failed");
      seen(null);
      await route.abort().catch(() => {});
    } finally {
      finish();
    }
  };
  await p.route(pattern, handler);
  return {
    arrived,
    release: async () => {
      release();
      await done;
      await p.unroute(pattern, handler);
    },
  };
}
function publicDialog(p) {
  return p.getByRole("dialog", { name: "管理客户端登记", exact: true });
}
function machineDialog(p) {
  return p.getByRole("dialog", { name: "机器身份与凭证", exact: true });
}
async function machines(p) {
  await s.nav(p, "应用接入");
  const row = p.getByRole("row").filter({
    has: p
      .locator("strong")
      .filter({ hasText: new RegExp("^" + appName + "$") }),
  });
  await row.getByRole("button", { name: "机器身份", exact: true }).click();
  const d = machineDialog(p);
  await d
    .getByRole("article")
    .filter({ has: p.getByRole("heading", { name: spName, exact: true }) })
    .getByRole("button", { name: "管理机器凭证", exact: true })
    .click();
  await d.getByRole("heading", { name: keyName, exact: true }).waitFor();
  return d;
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
    const app = (
        await call(p, "POST", base + "/applications", { name: appName }, 201)
      ).body,
      apps = base + "/applications/" + app.id;
    const clientReply = await call(
        p,
        "POST",
        apps + "/clients",
        {
          name: "观察用公开客户端",
          redirect_uri: "http://127.0.0.1:9876/callback",
        },
        201,
      ),
      client = clientReply.body,
      cp = apps + "/clients/" + client.client_id,
      cs = cp + "/protocol-status";
    const spReply = await call(
        p,
        "POST",
        apps + "/service-principals",
        { name: spName },
        201,
      ),
      sp = spReply.body,
      spPath = apps + "/service-principals/" + sp.id;
    const key = (
      await call(
        p,
        "POST",
        spPath + "/credentials",
        { name: keyName, expires_in_days: 30 },
        201,
        spReply.etag,
      )
    ).body;
    const kp = spPath + "/credentials/" + key.credential.id,
      ks = kp + "/protocol-status";
    const invite = (
      await call(p, "POST", base + "/invitations", { email: guest.email }, 201)
    ).body;
    await call(
      guest.page,
      "POST",
      "/v1/invitations/accept",
      { token: invite.token },
      201,
    );
    me = await s.login(owner, p, tenant);
    await s.login(guest, guest.page, tenant);
    await call(guest.page, "GET", cs, undefined, 403);
    await call(guest.page, "GET", ks, undefined, 403);
    let reads = 0,
      writes = 0;
    p.on("request", (r) => {
      const u = new URL(r.url());
      if (u.pathname.endsWith("/protocol-status")) reads++;
      if (r.method() === "POST" && u.pathname.startsWith("/bff" + apps))
        writes++;
    });
    await p.route(
      "**/bff/v1/tenants/*/applications/**/protocol-status",
      async (route) => {
        let r;
        try {
          r = await forward(route);
        } catch {
          await route.abort().catch(() => {});
          return;
        }
        try {
          await record(route, r);
        } catch {
          errors.push("observation contract failed");
        }
        await route.fulfill({ response: r }).catch(() => {});
      },
    );
    phase = "public-explicit-read-and-no-activation";
    await s.nav(p, "应用接入");
    let row = p.getByRole("row").filter({
      has: p
        .locator("strong")
        .filter({ hasText: new RegExp("^" + appName + "$") }),
    });
    await row
      .getByRole("button", { name: "查看登记配置", exact: true })
      .click();
    await p.getByRole("button", { name: "管理登记", exact: true }).click();
    let d = publicDialog(p),
      region = d.getByRole("region", { name: "协议登记核对", exact: true });
    await region.waitFor();
    await p.waitForTimeout(70);
    assert.equal(reads, 0);
    await inspect(region, "未发现协议登记");
    assert.equal(writes, 0);
    assert.equal((await call(p, "GET", cp)).body.active, false);
    fixture(["create-drift", client.client_id]);
    await inspect(region, "受控字段存在差异");
    await region
      .getByRole("listitem")
      .filter({ hasText: "授权范围" })
      .waitFor();
    assert(!(await region.innerText()).includes("platform:write"));
    phase = "safe-controlled-field-screenshots";
    absent(await region.innerText());
    await region.screenshot({
      path: path.join(s.root, "docs/ui/protocol-inspection-desktop.png"),
    });
    await p.setViewportSize({ width: 390, height: 844 });
    assert.equal(
      await p.evaluate(() => document.documentElement.scrollWidth > innerWidth),
      false,
    );
    await region.screenshot({
      path: path.join(s.root, "docs/ui/protocol-inspection-mobile.png"),
    });
    await p.setViewportSize({ width: 1365, height: 900 });
    fixture(["restore", client.client_id]);
    await inspect(region, "受控字段一致");
    assert.equal((await call(p, "GET", cp)).body.state, "pending");
    await d.getByLabel("我已核对配置并确认本次登记操作").check();
    await d
      .getByRole("button", { name: "核对并完成登记", exact: true })
      .click();
    await d.getByText("已生效", { exact: true }).waitFor();
    assert.equal(writes, 1);
    assert.equal(
      await region.getByTestId("protocol-observation-result").count(),
      0,
    );
    await inspect(region, "受控字段一致");
    phase = "failed-read-clears-healthy-observation-and-does-not-retry";
    const pattern = "**/bff" + cs;
    let refusals = 0;
    const unavailable = async (route) => {
      refusals++;
      await route
        .fulfill({
          status: 503,
          headers: {
            "Content-Type": "application/problem+json",
            "X-Request-ID": "0".repeat(24),
          },
          body: JSON.stringify({
            type: "urn:sso:problem:temporarily_unavailable",
            title: "temporarily_unavailable",
            status: 503,
            code: "temporarily_unavailable",
            request_id: "0".repeat(24),
          }),
        })
        .catch(() => {});
    };
    await p.route(pattern, unavailable);
    await region
      .getByRole("button", { name: "只读核对协议登记", exact: true })
      .click();
    await region.getByRole("alert").waitFor();
    assert.equal(refusals, 1);
    assert.equal(
      await region.getByTestId("protocol-observation-result").count(),
      0,
    );
    await p.unroute(pattern, unavailable);
    phase = "unknown-secret-field-refused-before-render";
    const current = (await s.bff(p, cs)).body;
    const invalid = async (route) => {
      await route
        .fulfill({
          status: 200,
          headers: {
            "Content-Type": "application/json",
            ETag: `"client-${tenant}-${client.client_id}-${current.revision}"`,
          },
          body: JSON.stringify({ ...current, secret_key: key.secret_key }),
        })
        .catch(() => {});
    };
    await p.route(pattern, invalid);
    await region
      .getByRole("button", { name: "只读核对协议登记", exact: true })
      .click();
    await region
      .getByRole("alert")
      .filter({ hasText: "协议观察数据不正确" })
      .waitFor();
    absent(await region.innerText());
    await p.unroute(pattern, invalid);
    await d
      .getByRole("button", { name: "关闭登记对话框", exact: true })
      .click();
    phase = "machine-matching-read-without-secret-view";
    d = await machines(p);
    region = d.getByRole("region", { name: "协议登记核对", exact: true });
    await inspect(region, "受控字段一致");
    assert.equal(await p.getByTestId("credential-secret").count(), 0);
    absent(await region.innerText());
    await region.screenshot({
      path: path.join(s.root, "docs/ui/machine-protocol-inspection.png"),
    });
    phase = "cancelled-read-drops-real-late-observation";
    let held = await hold(p, ks);
    await region
      .getByRole("button", { name: "只读核对协议登记", exact: true })
      .click();
    await held.arrived;
    await region
      .getByRole("button", { name: "取消核对读取", exact: true })
      .click();
    await held.release();
    await p.waitForTimeout(80);
    assert.equal(
      await region.getByTestId("protocol-observation-result").count(),
      0,
    );
    phase = "resource-revocation-and-refresh-drops-old-observation";
    held = await hold(p, ks);
    await region
      .getByRole("button", { name: "只读核对协议登记", exact: true })
      .click();
    await held.arrived;
    let detail = await call(p, "GET", kp);
    await call(p, "POST", kp + "/revoke", {}, 200, detail.etag);
    await d.getByRole("button", { name: "刷新机器凭证", exact: true }).click();
    await d.getByText("已撤销", { exact: true }).waitFor();
    region = d.getByRole("region", { name: "协议登记核对", exact: true });
    await region.waitFor();
    await held.release();
    await p.waitForTimeout(80);
    assert.equal(
      await region.getByTestId("protocol-observation-result").count(),
      0,
    );
    await inspect(region, "受控字段一致");
    phase = "closed-modal-drops-real-late-observation";
    held = await hold(p, ks);
    await region
      .getByRole("button", { name: "只读核对协议登记", exact: true })
      .click();
    await held.arrived;
    await d
      .getByRole("button", { name: "关闭机器身份对话框", exact: true })
      .click();
    await held.release();
    await p.waitForTimeout(80);
    assert.equal(await machineDialog(p).count(), 0);
    phase = "workspace-change-drops-real-late-observation";
    const spaces = (await call(p, "GET", "/v1/tenants")).body.items,
      other = spaces.find((v) => v.id !== tenant).id,
      otherPage = await owner.context.newPage();
    await s.login(owner, otherPage, other);
    const hash = new URL(otherPage.url()).hash;
    d = await machines(p);
    region = d.getByRole("region", { name: "协议登记核对", exact: true });
    held = await hold(p, ks);
    await region
      .getByRole("button", { name: "只读核对协议登记", exact: true })
      .click();
    await held.arrived;
    await p.evaluate((hash) => {
      location.hash = hash;
    }, hash);
    await p.getByRole("heading", { name: "空间概览", exact: true }).waitFor();
    await held.release();
    await p.waitForTimeout(80);
    assert.equal(
      await p
        .getByRole("region", { name: "协议登记核对", exact: true })
        .count(),
      0,
    );
    assert.equal((await call(p, "GET", "/v1/me")).body.tenant_id, other);
    absent(await p.locator("body").innerText());
    assert.equal(
      await p.evaluate(() => localStorage.length + sessionStorage.length),
      0,
    );
    assert.deepEqual(errors, []);
    assert.deepEqual(s.pageErrors, []);
    assert.deepEqual(s.leaks, []);
    const privacy = require("../support/private-material.cjs")(
      s.root,
      secrets,
      started,
    );
    console.log(
      "Protocol observation UI passed: " +
        checks +
        " contract responses; explicit-only reads, real absent/drift/matching public/machine states, no activation until confirmation, safe field metadata, error/unknown-secret refusal, cancellation/resource-refresh/modal/workspace late-response safety, mobile and private exclusion in " +
        privacy.files +
        " files/runtime logs",
    );
  } finally {
    clearInterval(progress);
    await s.close();
  }
})().catch((e) => {
  console.error(
    "Protocol observation UI failed: phase=" +
      phase +
      ", error=" +
      e.name +
      " (private details withheld)",
  );
  console.error(JSON.stringify(s.diagnostics()));
  process.exitCode = 1;
});
