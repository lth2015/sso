const s = require("../support/console-fixtures.cjs"),
  assert = require("node:assert/strict"),
  { execFileSync, spawn } = require("node:child_process"),
  path = require("node:path");
let phase = "startup",
  checks = 0,
  writes = 0,
  owner,
  tenant,
  base,
  child,
  hold = null,
  fault = null;
let appPage, appDiagnostic;
const appResponses = [];
const errors = [],
  secrets = [],
  started = new Date().toISOString(),
  progress = setInterval(
    () => console.log("Application User access UI phase: " + phase),
    20000,
  );
function normalized(url) {
  return url
    .split("?")[0]
    .replace(/\/tenants\/[a-f0-9]{32}/, "/tenants/{tenant}")
    .replace(/\/applications\/[a-f0-9]{32}/, "/applications/{application}")
    .replace(/\/clients\/[a-f0-9]{32}/, "/clients/{client}")
    .replace(/\/members\/[a-f0-9]{32}/, "/members/{member}")
    .replace(/\/roles\/[a-f0-9]{32}/, "/roles/{role}");
}
function validate(url, method, r, body) {
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
  if (typeof r.body?.token === "string") secrets.push(r.body.token);
}
async function call(p, method, url, body, etag, status = 200) {
  const r = await s.bff(p, url, {
    method,
    body,
    headers: etag ? { "If-Match": etag } : {},
  });
  assert.equal(r.status, status);
  if (!url.startsWith(base + "/")) validate(url, method, r, body);
  return r;
}
async function install(p) {
  await p.route("**/bff" + base + "/**", async (route) => {
    const req = route.request(),
      u = new URL(req.url()),
      url = u.pathname.slice(4) + u.search,
      method = req.method();
    try {
      const response = await route.fetch({
        url: "http://127.0.0.1:3003" + u.pathname + u.search,
        headers: {
          ...(await req.allHeaders()),
          host: "console.localhost:3003",
        },
        maxRedirects: 0,
        timeout: 5000,
      });
      let body;
      try {
        body = await response.json();
      } catch {}
      const headers = response.headers();
      validate(
        url,
        method,
        {
          status: response.status(),
          body,
          contentType: headers["content-type"],
          requestID: headers["x-request-id"],
        },
        method === "GET" ? undefined : req.postDataJSON(),
      );
      const input = method === "GET" ? undefined : req.postDataJSON();
      if (
        ["PUT", "DELETE"].includes(method) &&
        u.pathname.includes("/user-access")
      )
        writes++;
      if (hold && !hold.used && hold.match(url, method, input)) {
        const h = hold;
        h.used = true;
        h.resolve();
        await h.gate;
      }
      if (fault && fault.match(url, method, input)) {
        const f = fault;
        fault = null;
        if (f.kind === "invalid")
          await route.fulfill({
            response,
            json: { ...body, secret_key: "private-directory-marker" },
          });
        else if (f.kind === "lost") await route.abort("failed");
        else
          await route.fulfill({
            status: 503,
            contentType: "application/problem+json",
            body: JSON.stringify({ code: "temporarily_unavailable" }),
          });
        return;
      }
      await route.fulfill({ response }).catch(() => {});
    } catch {
      errors.push("User access forwarding failed");
      if (hold && !hold.used) {
        hold.used = true;
        hold.resolve();
      }
      await route.abort().catch(() => {});
    }
  });
}
const dialog = (p) =>
    p.getByRole("dialog", { name: "应用成员访问", exact: true }),
  confirm = "我确认修改所选应用的准入设置";
async function ready(d) {
  await d
    .getByRole("status")
    .filter({ hasText: "正在读取" })
    .waitFor({ state: "hidden" });
}
async function open(p, name) {
  await p
    .getByRole("row")
    .filter({
      has: p
        .locator("strong")
        .filter({ hasText: new RegExp("^" + name + "$") }),
    })
    .getByRole("button", { name: "成员访问", exact: true })
    .click();
  const d = dialog(p);
  await d.getByText(/^准入版本 /).waitFor();
  await ready(d);
  return d;
}
async function close(p) {
  await dialog(p).getByRole("button", { name: "关闭", exact: true }).click();
  await dialog(p).waitFor({ state: "hidden" });
}
function arm(match) {
  let resolve, release;
  const arrived = new Promise((r) => (resolve = r)),
    gate = new Promise((r) => (release = r));
  hold = { match, resolve, gate, release, used: false };
  return { ...hold, arrived };
}
async function select(d, member) {
  await d.getByLabel("选择应用访问成员", { exact: true }).selectOption(member);
  await d.getByRole("button", { name: "检查成员访问", exact: true }).click();
  await d
    .getByRole("status")
    .filter({ hasText: /当前允许访问|当前不允许访问/ })
    .waitFor();
}
async function change(d, button, version) {
  await d.getByLabel(confirm, { exact: true }).check();
  await d.getByRole("button", { name: button, exact: true }).click();
  await d.getByText("准入版本 " + version, { exact: true }).waitFor();
  await ready(d);
}
async function appLogin(p, tenant) {
  await p.goto("http://127.0.0.1:9876");
  await p.getByRole("button", { name: "使用 SSO 登录", exact: true }).click();
  await p.getByRole("heading", { name: "确认本次授权", exact: true }).waitFor();
  await p.locator("#tenant").selectOption(tenant);
  await p.getByRole("button", { name: "同意并继续", exact: true }).click();
  await p.waitForURL("http://127.0.0.1:9876/");
  await p.getByRole("status").filter({ hasText: "当前准入已确认" }).waitFor();
}
function launch(client) {
  return new Promise((resolve, reject) => {
    child = spawn(
      s.root + "/.local/user-application-example",
      ["--client", client],
      { cwd: s.root, stdio: ["ignore", "pipe", "pipe"] },
    );
    child.once("error", () =>
      reject(new Error("local example startup failed")),
    );
    child.once("exit", () =>
      reject(new Error("local example stopped before readiness")),
    );
    child.stdout.on("data", (data) => {
      if (
        data
          .toString()
          .includes("User application demo ready at http://127.0.0.1:9876")
      )
        resolve();
      if (/eyJ[\w-]+\.[\w-]+\.[\w-]+/.test(data.toString()))
        errors.push("example stdout contained JWT");
    });
    child.stderr.on("data", (data) => {
      if (/eyJ[\w-]+\.[\w-]+\.[\w-]+/.test(data.toString()))
        errors.push("example stderr contained JWT");
    });
  });
}
(async () => {
  try {
    await s.start();
    owner = await s.register(true);
    const guest = await s.register();
    const me = await s.login(owner);
    await s.login(guest);
    tenant = me.tenant_id;
    base = "/v1/tenants/" + tenant;
    const p = owner.page,
      gp = guest.page;
    appPage = gp;
    gp.on("response", (r) => {
      const u = new URL(r.url());
      if (u.origin === "http://127.0.0.1:9876" && u.pathname !== "/style.css")
        appResponses.push({ path: u.pathname, status: r.status() });
    });
    gp.on("console", (v) => {
      if (v.type() === "error" && v.text().includes("Content Security Policy"))
        errors.push("example CSP blocked navigation");
    });

    await install(p);
    const invitation = (
      await call(
        p,
        "POST",
        base + "/invitations",
        { email: guest.email },
        undefined,
        201,
      )
    ).body;
    const joined = (
      await call(
        gp,
        "POST",
        "/v1/invitations/accept",
        { token: invitation.token },
        undefined,
        201,
      )
    ).body;
    await s.login(owner, p, tenant);
    await s.login(guest, gp, tenant);
    const app = (
      await call(
        p,
        "POST",
        base + "/applications",
        { name: "准入演示工作台" },
        undefined,
        201,
      )
    ).body;
    const client = (
      await call(
        p,
        "POST",
        base + "/applications/" + app.id + "/clients",
        {
          name: "独立演示应用",
          redirect_uri: "http://127.0.0.1:9876/callback",
        },
        undefined,
        201,
      )
    ).body;
    await call(
      p,
      "POST",
      base +
        "/applications/" +
        app.id +
        "/clients/" +
        client.client_id +
        "/reconcile",
      {},
      `"client-${tenant}-${client.client_id}-${client.version}"`,
    );
    await launch(client.client_id);
    const policyURL = base + "/applications/" + app.id + "/user-access";
    phase = "confirmed-policy-mode-once-only-and-real-consent-denial";
    await s.nav(p, "应用接入");
    let d = await open(p, app.name);
    assert.equal(
      await d.getByLabel("应用访问模式", { exact: true }).inputValue(),
      "all_members",
    );
    await d
      .getByLabel("应用访问模式", { exact: true })
      .selectOption("selected_members");
    assert(
      await d
        .getByRole("button", { name: "保存访问模式", exact: true })
        .isDisabled(),
    );
    await d.getByLabel(confirm, { exact: true }).check();
    let h = arm((url, m) => m === "PUT" && url === policyURL);
    const before = writes;
    await d.getByRole("button", { name: "保存访问模式", exact: true }).click();
    await h.arrived;
    assert(
      await d.getByRole("button", { name: "关闭", exact: true }).isDisabled(),
    );
    await p.keyboard.press("Escape");
    assert(await d.isVisible());
    assert.equal(writes, before + 1);
    h.release();
    hold = null;
    await d.getByText("准入版本 2", { exact: true }).waitFor();
    await ready(d);
    await gp.goto("http://127.0.0.1:9876");
    await gp
      .getByRole("button", { name: "使用 SSO 登录", exact: true })
      .click();
    await gp
      .getByRole("heading", { name: "确认本次授权", exact: true })
      .waitFor();
    await gp
      .getByText(
        "没有可访问的空间。请联系空间管理员确认应用启用和成员访问授权。",
        { exact: true },
      )
      .waitFor();
    assert.equal(await gp.locator("#tenant option").count(), 0);
    phase = "explicit-member-check-grant-and-independent-user-application";
    await select(d, joined.membership_id);
    assert(
      await d
        .getByRole("status")
        .filter({ hasText: "当前不允许访问" })
        .isVisible(),
    );
    assert(
      await d
        .getByRole("button", { name: "添加显式授权", exact: true })
        .isDisabled(),
    );
    await change(d, "添加显式授权", 3);
    await appLogin(gp, tenant);
    const cookies = await guest.context.cookies("http://127.0.0.1:9876");
    assert(cookies.some((c) => c.name === "sso_user_app_demo" && c.httpOnly));
    assert(
      !(await gp.evaluate(() => document.cookie)).includes("sso_user_app_demo"),
    );
    assert(!/eyJ[\w-]+\.[\w-]+\.[\w-]+/.test(await gp.content()));
    await select(d, joined.membership_id);
    await p.setViewportSize({ width: 1365, height: 1000 });
    await p.screenshot({
      path: path.join(s.root, "docs/ui/application-user-access-desktop.png"),
    });
    await p.setViewportSize({ width: 390, height: 844 });
    assert(
      await d.evaluate((dialog) => {
        const actions = dialog.querySelector(".actions"),
          note = actions.nextElementSibling;
        return (
          note.getBoundingClientRect().top >=
          actions.getBoundingClientRect().bottom + 10
        );
      }),
      "member action explanation must stay clear of the buttons",
    );
    assert.equal(
      await p.evaluate(
        () => document.documentElement.scrollWidth > innerWidth + 1,
      ),
      false,
    );
    await d.evaluate((dialog) => {
      dialog.scrollTop = 0;
    });
    await p.screenshot({
      path: path.join(s.root, "docs/ui/application-user-access-mobile.png"),
    });
    await p.setViewportSize({ width: 1365, height: 1000 });
    await gp.setViewportSize({ width: 390, height: 844 });
    assert.equal(
      await gp.evaluate(
        () => document.documentElement.scrollWidth > innerWidth + 1,
      ),
      false,
    );
    await gp.screenshot({
      path: path.join(s.root, "docs/ui/user-application-mobile.png"),
    });
    phase = "live-revoke-regrant-no-cached-identity-or-automatic-login";
    await change(d, "移除显式授权", 4);
    await gp.getByRole("button", { name: "重新检查访问", exact: true }).click();
    await gp
      .getByRole("status")
      .filter({ hasText: "当前访问已失效" })
      .waitFor();
    assert.equal(
      await gp.getByText(joined.membership_id, { exact: true }).count(),
      0,
    );
    await select(d, joined.membership_id);
    await change(d, "添加显式授权", 5);
    await gp.reload();
    assert.equal(
      await gp.getByText("当前准入已确认", { exact: true }).count(),
      0,
    );
    await appLogin(gp, tenant);
    phase = "stale-decision-and-write-version-clear-old-authorization";
    await select(d, joined.membership_id);
    let meta = await call(p, "GET", policyURL);
    await call(p, "PUT", policyURL, { mode: "all_members" }, meta.etag);
    await d.getByRole("button", { name: "检查成员访问", exact: true }).click();
    await d.getByRole("alert").waitFor();
    assert.equal(
      await d.getByRole("status").filter({ hasText: "当前允许访问" }).count(),
      0,
    );
    await close(p);
    d = await open(p, app.name);
    await d
      .getByLabel("应用访问模式", { exact: true })
      .selectOption("selected_members");
    await d.getByLabel(confirm, { exact: true }).check();
    meta = await call(p, "GET", policyURL);
    await call(
      p,
      "PUT",
      policyURL + "/members/" + me.membership_id,
      {},
      meta.etag,
    );
    await d.getByRole("button", { name: "保存访问模式", exact: true }).click();
    await d
      .getByRole("alert")
      .filter({ hasText: "资料已被其他操作更新" })
      .waitFor();
    await close(p);
    phase = "malformed-and-dependency-response-hide-old-selection";
    for (const kind of ["invalid", "unavailable"]) {
      d = await open(p, app.name);
      await select(d, joined.membership_id);
      fault = {
        kind,
        match: (url, m) => m === "GET" && url.startsWith(policyURL + "?limit="),
      };
      await d
        .getByRole("button", { name: "重新读取设置", exact: true })
        .click();
      await d.getByRole("alert").waitFor();
      assert.equal(
        await d.getByRole("status").filter({ hasText: "当前允许访问" }).count(),
        0,
      );
      assert(!(await d.innerText()).includes("private-directory-marker"));
      await close(p);
    }
    phase = "committed-lost-policy-reply-is-not-replayed";
    d = await open(p, app.name);
    await d
      .getByLabel("应用访问模式", { exact: true })
      .selectOption("selected_members");
    await d.getByLabel(confirm, { exact: true }).check();
    const beforeLost = writes;
    fault = {
      kind: "lost",
      match: (url, m) => m === "PUT" && url === policyURL,
    };
    await d.getByRole("button", { name: "保存访问模式", exact: true }).click();
    await d.getByRole("alert").waitFor();
    assert.equal(writes, beforeLost + 1);
    assert.equal(
      await d
        .getByRole("button", { name: "保存访问模式", exact: true })
        .count(),
      0,
    );
    const committed = (await call(p, "GET", policyURL)).body;
    assert.equal(committed.policy.mode, "selected_members");
    assert.equal(committed.policy.version, "8");
    await close(p);
    d = await open(p, app.name);
    await select(d, joined.membership_id);
    fault = {
      kind: "invalid",
      match: (url, m) =>
        m === "GET" && url === policyURL + "/members/" + joined.membership_id,
    };
    await d.getByRole("button", { name: "检查成员访问", exact: true }).click();
    await d.getByRole("alert").waitFor();
    assert.equal(
      await d.getByRole("status").filter({ hasText: "当前允许访问" }).count(),
      0,
    );
    assert(!(await d.innerText()).includes("private-directory-marker"));
    await close(p);
    phase = "cancel-and-workspace-isolation";
    h = arm((url, m) => m === "GET" && url.startsWith(policyURL + "?limit="));
    await p
      .getByRole("row")
      .filter({
        has: p.locator("strong").filter({ hasText: /^准入演示工作台$/ }),
      })
      .getByRole("button", { name: "成员访问", exact: true })
      .click();
    await h.arrived;
    await close(p);
    h.release();
    hold = null;
    d = await open(p, app.name);
    assert.equal(
      await d.getByLabel("应用访问成员 ID", { exact: true }).inputValue(),
      "",
    );
    await close(p);
    const secondary = (await s.bff(p, "/v1/tenants")).body.items.find(
        (v) => v.id !== tenant,
      ).id,
      otherPage = await owner.context.newPage();
    await s.login(owner, otherPage, secondary);
    h = arm((url, m) => m === "GET" && url.startsWith(policyURL + "?limit="));
    await p
      .getByRole("row")
      .filter({
        has: p.locator("strong").filter({ hasText: /^准入演示工作台$/ }),
      })
      .getByRole("button", { name: "成员访问", exact: true })
      .click();
    await h.arrived;
    await p.goto(otherPage.url());
    await dialog(p).waitFor({ state: "hidden" });
    h.release();
    hold = null;
    assert.equal(await p.getByText(app.name, { exact: true }).count(), 0);
    phase = "owner-without-directory-can-check-opaque-member-id";
    await s.login(owner, p, tenant);
    let av = (await call(p, "GET", base + "/permissions")).etag;
    const deny = (
      await call(
        p,
        "POST",
        base + "/roles",
        {
          code: "admission_privacy",
          policies: [{ permission: "members.read", effect: "deny" }],
        },
        av,
        201,
      )
    ).body.id;
    await s.login(owner, p, tenant);
    av = (await call(p, "GET", base + "/permissions")).etag;
    await call(
      p,
      "PUT",
      base + "/members/" + me.membership_id + "/roles/" + deny,
      {},
      av,
      204,
    );
    await s.login(owner, p, tenant);
    await s.nav(p, "应用接入");
    d = await open(p, app.name);
    assert.equal(
      await d.getByLabel("选择应用访问成员", { exact: true }).count(),
      0,
    );
    assert(!(await d.innerText()).includes(guest.email));
    await d
      .getByLabel("应用访问成员 ID", { exact: true })
      .fill(joined.membership_id);
    await d.getByRole("button", { name: "检查成员访问", exact: true }).click();
    await d.getByRole("status").filter({ hasText: "当前允许访问" }).waitFor();
    phase = "live-owner-authority-change-requires-reauthorization";
    av = (await call(p, "GET", base + "/permissions")).etag;
    await call(
      p,
      "POST",
      base + "/roles",
      { code: "admission_changed", policies: [] },
      av,
      201,
    );
    await d.getByRole("button", { name: "检查成员访问", exact: true }).click();
    await p
      .getByRole("link", { name: "重新确认空间授权", exact: true })
      .waitFor();
    assert.equal(await dialog(p).count(), 0);
    assert.equal(errors.length, 0);
    assert.equal(s.pageErrors.length, 0);
    const privacy = require("../support/private-material.cjs")(
      s.root,
      [
        ...secrets,
        ...s.fixtures.flatMap((f) => [f.password, f.totpSecret, ...f.codes]),
      ],
      started,
    );
    console.log(
      "Application User access UI passed: " +
        checks +
        " unique real BFF contract responses; explicit mode/member writes, independent OAuth PKCE app, real login refusal/grant/revoke/regrant, no browser Bearer, desktop/mobile, stale namespace/version, strict failure clearing, cancellation/context isolation and owner directory-deny/live authority; private exclusion in " +
        privacy.files +
        " files/runtime logs",
    );
  } finally {
    if (appPage)
      appDiagnostic = await appPage
        .evaluate(() => ({
          path: location.pathname.replace(/[a-f0-9]{32}/g, "{id}"),
          appOrigin: location.origin === "http://127.0.0.1:9876",
          title: [
            "登录你的账户",
            "确认本次授权",
            "应用接入演示",
            "验证你的身份",
          ].includes(document.querySelector("h1")?.textContent)
            ? document.querySelector("h1").textContent
            : "other",
          callbackScope: new URL(location.href).searchParams.has("scope"),
          callbackIssuer: new URL(location.href).searchParams.has("iss"),
        }))
        .catch(() => ({ unavailable: true }));
    if (hold) hold.release();
    clearInterval(progress);
    if (child && child.exitCode === null) {
      const stopped = new Promise((r) => child.once("exit", r));
      child.kill("SIGTERM");
      await stopped;
    }
    await s.close();
  }
})().catch((e) => {
  console.error(
    "Application User access UI failed: phase=" +
      phase +
      ", error=" +
      e.name +
      " (private details withheld)",
  );
  console.error(
    JSON.stringify({
      checks,
      writes,
      appDiagnostic,
      appResponses,
      cspBlocked: errors.includes("example CSP blocked navigation"),
      forwardingErrors: errors.length,
      frames: [
        ...String(e.stack).matchAll(/user-access\/browser\.cjs:(\d+):/g),
      ].map((v) => Number(v[1])),
    }),
  );
  process.exitCode = 1;
});
