// Real owner/BFF/primary reads; large directory is synthetic metadata, no credentials.
const s = require("../support/console-fixtures.cjs"),
  assert = require("node:assert/strict"),
  { execFileSync } = require("node:child_process"),
  path = require("node:path");
let phase = "startup",
  checks = 0,
  writes = 0,
  population,
  owner,
  tenant,
  base,
  hold = null,
  fault = null;
const errors = [],
  started = new Date().toISOString(),
  progress = setInterval(
    () => console.log("Role chooser phase: " + phase),
    20000,
  );
function fixture(action, record) {
  return execFileSync(
    "python3",
    ["tests/role-choices/population.py", action, ownerID, tenant],
    {
      cwd: s.root,
      input: record ? JSON.stringify(record) : undefined,
      stdio: ["pipe", "pipe", "pipe"],
      maxBuffer: 1024 * 1024,
    },
  ).toString();
}
let ownerID;
function normalized(url) {
  return url
    .split("?")[0]
    .replace(/\/tenants\/[a-f0-9]{32}/, "/tenants/{tenant}")
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
}
async function call(p, method, url, body, etag) {
  const r = await s.bff(p, url, {
    method,
    body,
    headers: etag ? { "If-Match": etag } : {},
  });
  validate(url, method, r, body);
  assert([200, 201, 204].includes(r.status));
  return r;
}
const dialog = (p) =>
  p.getByRole("dialog", { name: "管理成员角色", exact: true });
async function open(p) {
  await p.getByRole("button", { name: "管理成员角色", exact: true }).click();
  const d = dialog(p);
  await d.waitFor();
  await d.getByText("已加载 25 位成员", { exact: true }).waitFor();
  return d;
}
async function closed(p) {
  await dialog(p).getByRole("button", { name: "取消", exact: true }).click();
  await dialog(p).waitFor({ state: "hidden" });
}
function arm(match) {
  let resolve, release;
  const arrived = new Promise((r) => (resolve = r)),
    gate = new Promise((r) => (release = r));
  const h = { match, arrived, resolve, gate, release, used: false };
  hold = h;
  return h;
}
async function ready(d) {
  await d.getByRole("status").waitFor({ state: "hidden" });
}
(async () => {
  try {
    await s.start();
    owner = await s.register(true);
    let me = await s.login(owner);
    ownerID = me.user_id;
    tenant = me.tenant_id;
    base = "/v1/tenants/" + tenant;
    population = JSON.parse(fixture("create"));
    const p = owner.page,
      spaces = (await call(p, "GET", "/v1/tenants")).body.items,
      secondary = spaces.find((v) => v.id !== tenant).id;
    let memberReads = 0;
    await p.route("**/bff" + base + "/**", async (route) => {
      const req = route.request(),
        u = new URL(req.url()),
        url = u.pathname.slice(4) + u.search,
        method = req.method();
      try {
        if (method === "GET" && u.pathname === "/bff" + base + "/members")
          memberReads++;
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
        if ([200, 201, 204].includes(response.status()))
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
        if (
          ["PUT", "DELETE"].includes(method) &&
          /\/members\/[a-f0-9]{32}\/roles\/[a-f0-9]{32}$/.test(u.pathname)
        )
          writes++;
        if (hold && !hold.used && hold.match(url, method)) {
          const h = hold;
          h.used = true;
          h.resolve();
          await h.gate;
        }
        if (fault && fault.match(url, method)) {
          const f = fault;
          fault = null;
          if (f.kind === "bad-page")
            await route.fulfill({
              response,
              json: { ...body, secret_key: "choice-private-marker" },
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
        errors.push("route forwarding failed");
        if (hold && !hold.used) {
          hold.used = true;
          hold.resolve();
        }
        await route.abort().catch(() => {});
      }
    });
    phase = "real-more-than-one-hundred-members-and-two-role-pages";
    await s.nav(p, "角色与权限");
    let d = await open(p);
    assert.equal(
      await d.getByLabel("目标成员", { exact: true }).locator("option").count(),
      26,
    );
    while (
      await d.getByRole("button", { name: "加载更多成员", exact: true }).count()
    ) {
      await d
        .getByRole("button", { name: "加载更多成员", exact: true })
        .click();
      await ready(d);
    }
    assert.equal(
      await d.getByLabel("目标成员", { exact: true }).locator("option").count(),
      107,
    );
    await d.getByRole("button", { name: "加载更多角色", exact: true }).click();
    await ready(d);
    assert.equal(
      await d.getByLabel("选择角色", { exact: true }).locator("option").count(),
      32,
    );
    const target = population.members.at(-1).id,
      role = population.roles.at(-1);
    await d.getByLabel("目标成员", { exact: true }).selectOption(target);
    await ready(d);
    await d.getByLabel("选择角色", { exact: true }).selectOption(role.id);
    assert(
      await d
        .getByRole("button", { name: "保存角色变更", exact: true })
        .isEnabled(),
    );
    await p.setViewportSize({ width: 1365, height: 1000 });
    await p.screenshot({
      path: path.join(s.root, "docs/ui/role-choices-desktop.png"),
    });
    await p.setViewportSize({ width: 390, height: 844 });
    assert.equal(
      await p.evaluate(
        () => document.documentElement.scrollWidth > innerWidth + 1,
      ),
      false,
    );
    await p.screenshot({
      path: path.join(s.root, "docs/ui/role-choices-mobile.png"),
    });
    await p.setViewportSize({ width: 1365, height: 1000 });
    phase = "one-real-binding-to-the-last-member";
    await d.getByRole("button", { name: "保存角色变更", exact: true }).click();
    await d.waitFor({ state: "hidden" });
    assert.equal(writes, 1);
    await s.login(owner, p, tenant);
    assert(
      (
        await call(p, "GET", base + "/members/" + target + "/roles")
      ).body.items.some((v) => v.id === role.id),
    );
    phase = "pinned-row-from-later-main-directory-page";
    await s.nav(p, "成员");
    const targetRow = () =>
      p.getByRole("row").filter({
        has: p.locator('code[title="' + population.members.at(-1).user + '"]'),
      });
    while (!(await targetRow().count())) {
      const count = await p.getByRole("row").count();
      await p.getByRole("button", { name: "加载更多", exact: true }).click();
      await p.waitForFunction(
        (count) => document.querySelectorAll("tr").length > count,
        count,
      );
    }
    const row = targetRow();
    await row.getByRole("button", { name: "管理角色", exact: true }).click();
    d = dialog(p);
    await d.getByText("已加载 26 位成员", { exact: true }).waitFor();
    assert.equal(
      await d.getByLabel("目标成员", { exact: true }).inputValue(),
      target,
    );
    await d.getByText("当前角色：" + role.code, { exact: false }).waitFor();
    while (
      await d.getByRole("button", { name: "加载更多成员", exact: true }).count()
    ) {
      await d
        .getByRole("button", { name: "加载更多成员", exact: true })
        .click();
      await ready(d);
    }
    assert.equal(
      await d.getByLabel("目标成员", { exact: true }).locator("option").count(),
      107,
    );
    assert.equal(
      await d.getByLabel("目标成员", { exact: true }).inputValue(),
      target,
    );
    phase = "remove-a-binding-outside-the-first-role-choice-page";
    await d.getByLabel("绑定操作", { exact: true }).selectOption("unbind");
    await d.getByLabel("选择角色", { exact: true }).selectOption(role.id);
    await d.getByRole("button", { name: "保存角色变更", exact: true }).click();
    await d.waitFor({ state: "hidden" });
    assert.equal(writes, 2);
    await s.login(owner, p, tenant);
    assert.equal(
      (await call(p, "GET", base + "/members/" + target + "/roles")).body.items
        .length,
      0,
    );
    phase = "close-during-initial-read";
    await s.nav(p, "角色与权限");
    let h = arm((url, m) => m === "GET" && url === base + "/roles?limit=25");
    await p.getByRole("button", { name: "管理成员角色", exact: true }).click();
    await h.arrived;
    await closed(p);
    h.release();
    hold = null;
    await new Promise((r) => setTimeout(r, 100));
    assert.equal(await dialog(p).count(), 0);
    phase = "close-and-reopen-during-more";
    d = await open(p);
    h = arm(
      (url, m) => m === "GET" && url.includes("/members?limit=25&after="),
    );
    await d.getByRole("button", { name: "加载更多成员", exact: true }).click();
    await h.arrived;
    await closed(p);
    d = await open(p);
    h.release();
    hold = null;
    await ready(d);
    await d.getByText("已加载 25 位成员", { exact: true }).waitFor();
    await closed(p);
    phase = "invalid-page-clears-selection-and-does-not-write";
    d = await open(p);
    await d
      .getByLabel("目标成员", { exact: true })
      .selectOption(me.membership_id);
    await ready(d);
    await d.getByLabel("选择角色", { exact: true }).selectOption({ index: 1 });
    fault = {
      kind: "bad-page",
      match: (url, m) =>
        m === "GET" && url.includes("/members?limit=25&after="),
    };
    await d.getByRole("button", { name: "加载更多成员", exact: true }).click();
    await d
      .getByText("本次选择已清除，请关闭后重新读取。", { exact: true })
      .waitFor();
    assert(
      await d
        .getByRole("button", { name: "保存角色变更", exact: true })
        .isDisabled(),
    );
    assert(!(await d.innerText()).includes("choice-private-marker"));
    assert.equal(writes, 2);
    await closed(p);
    phase = "binding-read-failure-clears-old-selection";
    d = await open(p);
    fault = {
      kind: "unavailable",
      match: (url, m) =>
        m === "GET" && url === base + "/members/" + me.membership_id + "/roles",
    };
    await d
      .getByLabel("目标成员", { exact: true })
      .selectOption(me.membership_id);
    await d
      .getByText("本次选择已清除，请关闭后重新读取。", { exact: true })
      .waitFor();
    assert(
      await d
        .getByRole("button", { name: "保存角色变更", exact: true })
        .isDisabled(),
    );
    await closed(p);
    phase = "workspace-switch-drops-pending-page";
    d = await open(p);
    h = arm(
      (url, m) => m === "GET" && url.includes("/members?limit=25&after="),
    );
    await d.getByRole("button", { name: "加载更多成员", exact: true }).click();
    await h.arrived;
    await s.login(owner, p, secondary);
    h.release();
    hold = null;
    await s.nav(p, "角色与权限");
    await p.getByRole("button", { name: "管理成员角色", exact: true }).click();
    d = dialog(p);
    await d.getByText("已加载 1 位成员", { exact: true }).waitFor();
    assert.equal(
      await d.getByLabel("目标成员", { exact: true }).locator("option").count(),
      2,
    );
    assert(!(await d.innerText()).includes(target));
    await closed(p);
    phase = "real-authority-change-during-pagination";
    await s.login(owner, p, tenant);
    await s.nav(p, "角色与权限");
    d = await open(p);
    const staleETag = (await call(p, "GET", base + "/permissions")).etag;
    await call(
      p,
      "POST",
      base + "/roles",
      { code: "stale_choice_read", policies: [] },
      staleETag,
    );
    await d.getByRole("button", { name: "加载更多成员", exact: true }).click();
    await p
      .getByRole("heading", { name: "请重新确认访问权限", exact: true })
      .waitFor();
    assert.equal(await dialog(p).count(), 0);
    assert.equal(writes, 2);
    phase = "deny-create-role";
    await s.login(owner, p, tenant);
    let version = (await call(p, "GET", base + "/permissions")).etag;
    const deny = (
      await call(
        p,
        "POST",
        base + "/roles",
        {
          code: "deny_choice_directory",
          policies: [{ permission: "members.read", effect: "deny" }],
        },
        version,
      )
    ).body.id;
    phase = "deny-bind-current-owner";
    await s.login(owner, p, tenant);
    version = (await call(p, "GET", base + "/permissions")).etag;
    await call(
      p,
      "PUT",
      base + "/members/" + me.membership_id + "/roles/" + deny,
      {},
      version,
    );
    await s.login(owner, p, tenant);
    await s.nav(p, "角色与权限");
    phase = "deny-self-chooser";
    const before = memberReads;
    await p.getByRole("button", { name: "管理成员角色", exact: true }).click();
    d = dialog(p);
    await d
      .getByRole("option", { name: "当前账户", exact: true })
      .waitFor({ state: "attached" });
    assert.equal(
      await d.getByLabel("目标成员", { exact: true }).locator("option").count(),
      2,
    );
    await d
      .getByLabel("目标成员", { exact: true })
      .selectOption(me.membership_id);
    await ready(d);
    assert.equal(memberReads, before);
    assert.equal(
      await d
        .getByRole("button", { name: "加载更多成员", exact: true })
        .count(),
      0,
    );
    await closed(p);
    assert.equal(writes, 3);
    assert.equal(errors.length, 0);
    assert.equal(s.pageErrors.length, 0);
    assert.equal(s.leaks.length, 0);
    const values = s.fixtures.flatMap((f) => [
      f.password,
      f.totpSecret,
      ...f.codes,
    ]);
    const absent = require("../support/private-material.cjs")(
      s.root,
      values,
      started,
    );
    console.log(
      "Role chooser passed: " +
        checks +
        " real BFF contract responses; 106 members, 31 roles, last-page binding, pinned row, close/reopen/workspace stale responses, invalid/failure clearing and directory-denied self recovery; private exclusion in " +
        absent.files +
        " files/runtime logs",
    );
  } finally {
    if (hold) hold.release();
    clearInterval(progress);
    if (population) {
      fixture("cleanup", population);
      population = null;
    }
    await s.close();
  }
})().catch((e) => {
  console.error(
    "Role chooser failed: phase=" +
      phase +
      ", error=" +
      e.name +
      " (private details withheld)",
  );
  process.exitCode = 1;
});
