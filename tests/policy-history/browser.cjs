// Real BFF role mutations create the timeline; no invented history rows or auth.
const s = require("../support/console-fixtures.cjs"),
  assert = require("node:assert/strict"),
  { execFileSync } = require("node:child_process"),
  path = require("node:path");
let debugPage;
let phase = "startup",
  checks = 0,
  hold = null,
  fault = null,
  base;
const errors = [],
  started = new Date().toISOString(),
  progress = setInterval(
    () => console.log("Policy history UI phase: " + phase),
    20000,
  );
function normalized(url) {
  return url
    .split("?")[0]
    .replace(/\/tenants\/[a-f0-9]{32}/, "/tenants/{tenant}")
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
}
async function call(p, method, url, body, etag, status = 200) {
  const r = await s.bff(p, url, {
    method,
    body,
    headers: etag ? { "If-Match": etag } : {},
  });
  assert.equal(r.status, status);
  if (!url.startsWith(base + "/roles/")) validate(url, method, r, body);
  return r;
}
const dialog = (p) =>
  p.getByRole("dialog", { name: "角色策略历史", exact: true });
async function ready(d) {
  await d
    .getByRole("status")
    .filter({ hasText: "正在读取策略历史…" })
    .waitFor({ state: "hidden" });
}
async function open(p, code) {
  await s.nav(p, "角色与权限");
  await p
    .getByRole("row")
    .filter({ hasText: code })
    .getByRole("button", { name: "查看策略历史", exact: true })
    .click();
  const d = dialog(p);
  await d.waitFor();
  await d.getByRole("article").first().waitFor();
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
  const h = { match, resolve, release, arrived, gate, used: false };
  hold = h;
  return h;
}
(async () => {
  try {
    await s.start();
    const owner = await s.register(true),
      me = await s.login(owner),
      p = owner.page,
      tenant = me.tenant_id;
    base = "/v1/tenants/" + tenant;
    debugPage = p;
    await p.route("**/bff" + base + "/roles/**", async (route) => {
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
        if (hold && !hold.used && hold.match(url, method)) {
          const h = hold;
          h.used = true;
          h.resolve();
          await h.gate;
        }
        if (fault && fault.match(url, method)) {
          const f = fault;
          fault = null;
          console.log("History UI controlled injection: " + f.kind);
          if (f.kind === "invalid")
            await route.fulfill({
              response,
              json: { ...body, secret_key: "private-history-marker" },
            });
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
        errors.push("history UI forwarding failed");
        if (hold && !hold.used) {
          hold.used = true;
          hold.resolve();
        }
        await route.abort().catch(() => {});
      }
    });
    async function version() {
      return (await call(p, "GET", base + "/permissions")).etag;
    }
    const code = "ui_policy_history";
    const role = (
        await call(
          p,
          "POST",
          base + "/roles",
          { code, policies: [{ permission: "audit.read", effect: "allow" }] },
          await version(),
          201,
        )
      ).body.id,
      url = base + "/roles/" + role + "/policy-history";
    await s.login(owner, p, tenant);
    phase = "captured-timeline-pagination";
    for (let i = 0; i < 20; i++) {
      await call(
        p,
        "PUT",
        base + "/roles/" + role,
        {
          policies: [
            {
              permission: "audit.read",
              effect: i % 2 === 0 ? "deny" : "allow",
            },
          ],
        },
        await version(),
        204,
      );
      await s.login(owner, p, tenant);
    }
    let d = await open(p, code);
    assert.equal(await d.getByRole("article").count(), 20);
    assert(
      await d
        .getByRole("button", { name: "下一批策略变化", exact: true })
        .isEnabled(),
    );
    assert.equal(
      await d.getByRole("button", { name: "保存并继续", exact: true }).count(),
      0,
    );
    assert((await d.innerText()).includes("仅包含启用采集后实际捕获"));
    await p.setViewportSize({ width: 1365, height: 1000 });
    await p.screenshot({
      path: path.join(s.root, "docs/ui/role-policy-history-desktop.png"),
    });
    await p.setViewportSize({ width: 390, height: 844 });
    assert.equal(
      await p.evaluate(
        () => document.documentElement.scrollWidth > innerWidth + 1,
      ),
      false,
    );
    await p.screenshot({
      path: path.join(s.root, "docs/ui/role-policy-history-mobile.png"),
    });
    await p.setViewportSize({ width: 1365, height: 1000 });
    await d
      .getByRole("button", { name: "下一批策略变化", exact: true })
      .click();
    await ready(d);
    assert.equal(await d.getByRole("article").count(), 1);
    assert.equal(
      await d
        .getByRole("button", { name: "下一批策略变化", exact: true })
        .count(),
      0,
    );
    await d.getByRole("button", { name: "返回首批", exact: true }).click();
    await ready(d);
    assert.equal(await d.getByRole("article").count(), 20);
    await close(p);
    phase = "close-during-read-and-reopen-late-result";
    let h = arm((u, m) => m === "GET" && u === url + "?limit=20");
    await s.nav(p, "角色与权限");
    await p
      .getByRole("row")
      .filter({ hasText: code })
      .getByRole("button", { name: "查看策略历史", exact: true })
      .click();
    await h.arrived;
    await close(p);
    d = await open(p, code);
    h.release();
    hold = null;
    await ready(d);
    assert.equal(await d.getByRole("article").count(), 20);
    await close(p);
    phase = "unknown-private-fields-and-failure-clear-old-data";
    d = await open(p, code);
    fault = {
      kind: "invalid",
      match: (u, m) => m === "GET" && u === url + "?limit=20",
    };
    await d.getByRole("button", { name: "重新读取", exact: true }).click();
    phase = "private-history-await-alert";
    await d.getByRole("alert").waitFor();
    assert.equal(await d.getByRole("article").count(), 0);
    assert(!(await d.innerText()).includes("private-history-marker"));
    fault = {
      kind: "unavailable",
      match: (u, m) => m === "GET" && u === url + "?limit=20",
    };
    await d.getByRole("button", { name: "重新读取", exact: true }).click();
    phase = "dependency-history-await-alert";
    await d.getByRole("alert").filter({ hasText: "服务暂时不可用" }).waitFor();
    assert.equal(await d.getByRole("article").count(), 0);
    await close(p);
    phase = "context-switch-drops-old-role-data";
    d = await open(p, code);
    h = arm((u, m) => m === "GET" && u === url + "?limit=20");
    await d.getByRole("button", { name: "重新读取", exact: true }).click();
    await h.arrived;
    const secondary = (await s.bff(p, "/v1/tenants")).body.items.find(
      (t) => t.id !== tenant,
    ).id;
    await s.login(owner, p, secondary);
    h.release();
    hold = null;
    assert.equal(await dialog(p).count(), 0);
    await s.nav(p, "角色与权限");
    assert.equal(
      await p
        .getByRole("button", { name: "查看策略历史", exact: true })
        .count(),
      0,
    );
    assert(!(await p.locator("main").innerText()).includes(code));
    phase = "deleted-role-history-from-real-audit-event";
    await s.login(owner, p, tenant);
    await call(p, "DELETE", base + "/roles/" + role, {}, await version(), 204);
    await s.login(owner, p, tenant);
    await s.nav(p, "审计");
    const deleted = () =>
      p
        .getByRole("row")
        .filter({ has: p.locator('code[title="' + role + '"]') })
        .filter({ hasText: "删除角色" });
    while (!(await deleted().count())) {
      const count = await p.getByRole("row").count();
      await p.getByRole("button", { name: "加载更多", exact: true }).click();
      await p.waitForFunction(
        (n) => document.querySelectorAll("tr").length > n,
        count,
      );
    }
    await deleted()
      .getByRole("button", { name: "查看角色策略历史", exact: true })
      .click();
    d = dialog(p);
    await ready(d);
    await d
      .getByRole("button", { name: "下一批策略变化", exact: true })
      .click();
    await ready(d);
    assert.equal(await d.getByRole("article").count(), 2);
    await d
      .getByRole("heading", { name: "删除角色 · " + code, exact: true })
      .waitFor();
    await p.screenshot({
      path: path.join(s.root, "docs/ui/role-policy-history-deleted.png"),
    });
    await close(p);
    phase = "live-authorization-failure-clears-current-history";
    await s.nav(p, "审计");
    await deleted()
      .getByRole("button", { name: "查看角色策略历史", exact: true })
      .click();
    d = dialog(p);
    await ready(d);
    await call(
      p,
      "POST",
      base + "/roles",
      { code: "history_context_change", policies: [] },
      await version(),
      201,
    );
    await d.getByRole("button", { name: "重新读取", exact: true }).click();
    await p
      .getByRole("heading", { name: "请重新确认访问权限", exact: true })
      .waitFor();
    assert.equal(await dialog(p).count(), 0);
    assert.equal(errors.length, 0);
    assert.equal(s.pageErrors.length, 0);
    assert.equal(s.leaks.length, 0);
    const privacy = require("../support/private-material.cjs")(
      s.root,
      s.fixtures.flatMap((f) => [f.password, f.totpSecret, ...f.codes]),
      started,
    );
    console.log(
      "Policy history UI passed: " +
        checks +
        " unique real BFF contract responses; 21 genuine captured policy changes, pagination, desktop/mobile, deleted audit entry, cancel/reopen/context isolation, invalid/private/dependency rejection and live revocation; private exclusion in " +
        privacy.files +
        " files/runtime logs",
    );
  } catch (e) {
    if (debugPage && !debugPage.isClosed()) {
      const d = dialog(debugPage),
        text = await d.innerText().catch(() => "");
      console.error(
        "History UI safe state: " +
          JSON.stringify({
            phase,
            dialog: await d.count(),
            articles: await d.getByRole("article").count(),
            alerts: await d.getByRole("alert").count(),
            loading: text.includes("正在读取策略历史"),
            invalid: text.includes("策略历史数据不正确"),
            unavailable: text.includes("服务暂时不可用"),
            network: text.includes("连接中断"),
            generic: text.includes("操作未完成"),
            reloadDisabled: await d
              .getByRole("button", { name: "重新读取", exact: true })
              .isDisabled()
              .catch(() => true),
          }),
      );
    }
    throw e;
  } finally {
    if (hold) hold.release();
    clearInterval(progress);
    await s.close();
  }
})().catch((e) => {
  console.error(
    "Policy history UI failed: phase=" +
      phase +
      ", error=" +
      e.name +
      " (private details withheld)",
  );
  process.exitCode = 1;
});
