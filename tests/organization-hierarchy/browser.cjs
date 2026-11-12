const s = require("../support/console-fixtures.cjs"),
  assert = require("node:assert/strict"),
  { execFileSync } = require("node:child_process"),
  path = require("node:path");
let phase = "startup",
  checks = 0,
  writes = 0,
  previews = 0,
  owner,
  user,
  tenant,
  base,
  organizations,
  hold = null,
  fault = null;
const errors = [],
  secrets = [],
  started = new Date().toISOString(),
  progress = setInterval(
    () => console.log("Organization hierarchy phase: " + phase),
    20000,
  );
function normalized(url) {
  return url
    .split("?")[0]
    .replace(/\/tenants\/[a-f0-9]{32}/, "/tenants/{tenant}")
    .replace(/\/organizations\/[a-f0-9]{32}/, "/organizations/{org}")
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
        method === "POST" &&
        (u.pathname.endsWith("/organizations") || u.pathname.endsWith("/move"))
      ) {
        if (input?.dry_run === true) previews++;
        else writes++;
      }
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
      errors.push("hierarchy forwarding failed");
      if (hold && !hold.used) {
        hold.used = true;
        hold.resolve();
      }
      await route.abort().catch(() => {});
    }
  });
}
const dialog = (p) => p.getByRole("dialog"),
  confirm = "我确认按当前预览移动组织及其下级";
async function ready(d) {
  await d
    .getByRole("status")
    .filter({ hasText: /正在/ })
    .waitFor({ state: "hidden" });
}
async function open(p, row) {
  if (row)
    await p
      .locator('tr[data-record-id="' + row + '"]')
      .getByRole("button", { name: "移动组织", exact: true })
      .click();
  else await p.getByRole("button", { name: "创建组织", exact: true }).click();
  const d = dialog(p);
  await d.getByText("已加载 25 个组织", { exact: true }).waitFor();
  await ready(d);
  return d;
}
async function close(p) {
  await dialog(p).getByRole("button", { name: "取消", exact: true }).click();
  await dialog(p).waitFor({ state: "hidden" });
}
async function all(d) {
  while (
    await d.getByRole("button", { name: "加载更多组织", exact: true }).count()
  ) {
    await d.getByRole("button", { name: "加载更多组织", exact: true }).click();
    await ready(d);
  }
}
function arm(match) {
  let resolve, release;
  const arrived = new Promise((r) => (resolve = r)),
    gate = new Promise((r) => (release = r));
  hold = { match, resolve, gate, release, used: false };
  return { ...hold, arrived };
}
async function preview(p) {
  const d = dialog(p);
  await d.getByRole("button", { name: "预览移动", exact: true }).click();
  await d.getByRole("status").filter({ hasText: "将移动" }).waitFor();
}
(async () => {
  try {
    await s.start();
    owner = await s.register(true);
    const guest = await s.register();
    const me = await s.login(owner);
    user = me.user_id;
    tenant = me.tenant_id;
    base = "/v1/tenants/" + tenant;
    // Only 55 synthetic metadata leaves in this newly verified non-demo space.
    organizations = JSON.parse(
      execFileSync(
        "python3",
        ["tests/organization-assignment/population.py", "create", user, tenant],
        { cwd: s.root, stdio: ["pipe", "pipe", "pipe"] },
      ),
    );
    const p = owner.page,
      root = organizations.root,
      last = organizations.orgs.at(-1).id,
      other = organizations.orgs.at(-2).id,
      secondary = (await s.bff(p, "/v1/tenants")).body.items.find(
        (v) => v.id !== tenant,
      ).id;
    await install(p);
    await install(guest.page);
    phase = "real-last-page-create-and-explicit-selection";
    await s.nav(p, "组织");
    assert.equal(
      await p.locator('tr[data-record-id="' + last + '"]').count(),
      0,
    );
    let d = await open(p);
    assert.equal(
      await d.getByLabel("上级组织", { exact: true }).inputValue(),
      "",
    );
    await d.getByLabel("组织名称").fill("末页创建的子组织");
    assert(
      await d
        .getByRole("button", { name: "保存并继续", exact: true })
        .isDisabled(),
    );
    await all(d);
    await d.getByText("已加载 56 个组织", { exact: true }).waitFor();
    await d.getByLabel("上级组织", { exact: true }).selectOption(last);
    const beforeCreate = writes;
    let h = arm(
      (url, m, b) =>
        m === "POST" &&
        url === base + "/organizations" &&
        b.name === "末页创建的子组织",
    );
    await d.getByRole("button", { name: "保存并继续", exact: true }).click();
    await h.arrived;
    assert.equal(writes, beforeCreate + 1);
    assert(
      await d.getByRole("button", { name: "取消", exact: true }).isDisabled(),
    );
    await p.keyboard.press("Escape");
    assert(await d.isVisible());
    await d.locator("form").evaluate((f) => f.requestSubmit());
    assert.equal(writes, beforeCreate + 1);
    h.release();
    hold = null;
    await d.waitFor({ state: "hidden" });
    const list = await call(p, "GET", base + "/organizations?limit=100"),
      source = list.body.items.find((v) => v.name === "末页创建的子组织");
    assert.equal(source.parent_id, last);
    const child = (
      await call(
        p,
        "POST",
        base + "/organizations",
        { parent_id: source.id, name: "实际子节点" },
        list.etag,
        201,
      )
    ).body.id;
    await s.nav(p, "空间概览");
    await s.nav(p, "组织");
    phase = "real-subtree-preview-target-binding-and-page-invalidation";
    d = await open(p, source.id);
    await d.getByLabel("上级组织", { exact: true }).selectOption(root);
    assert(
      await d
        .getByRole("button", { name: "确认移动", exact: true })
        .isDisabled(),
    );
    await preview(p);
    await d.getByLabel(confirm, { exact: true }).check();
    await d.getByRole("button", { name: "加载更多组织", exact: true }).click();
    await ready(d);
    assert.equal(
      await d.getByLabel(confirm, { exact: true }).isChecked(),
      false,
    );
    assert.equal(
      await d.getByRole("status").filter({ hasText: "将移动" }).count(),
      0,
    );
    assert(
      await d
        .getByRole("button", { name: "确认移动", exact: true })
        .isDisabled(),
    );
    await all(d);
    assert.equal(
      await d
        .getByLabel("上级组织", { exact: true })
        .locator('option[value="' + source.id + '"]')
        .count(),
      0,
    );
    assert.equal(
      await d
        .getByLabel("上级组织", { exact: true })
        .locator('option[value="' + last + '"]')
        .count(),
      0,
    );
    await d.getByLabel("上级组织", { exact: true }).selectOption(other);
    await preview(p);
    await d
      .getByRole("status")
      .filter({ hasText: "将移动 2 个组织" })
      .waitFor();
    await d.getByLabel(confirm, { exact: true }).check();
    await d.getByLabel("上级组织", { exact: true }).selectOption(root);
    assert.equal(
      await d.getByLabel(confirm, { exact: true }).isChecked(),
      false,
    );
    assert(
      await d
        .getByRole("button", { name: "确认移动", exact: true })
        .isDisabled(),
    );
    await d.getByLabel("上级组织", { exact: true }).selectOption(other);
    await preview(p);
    await d.getByLabel(confirm, { exact: true }).check();
    await p.screenshot({
      path: path.join(s.root, "docs/ui/organization-hierarchy-desktop.png"),
    });
    await p.setViewportSize({ width: 390, height: 844 });
    assert.equal(
      await p.evaluate(
        () => document.documentElement.scrollWidth > innerWidth + 1,
      ),
      false,
    );
    await p.screenshot({
      path: path.join(s.root, "docs/ui/organization-hierarchy-mobile.png"),
    });
    await p.setViewportSize({ width: 1365, height: 1000 });
    const beforeMove = writes;
    h = arm(
      (url, m, b) =>
        m === "POST" && url.endsWith("/move") && b.dry_run === false,
    );
    await d.getByRole("button", { name: "确认移动", exact: true }).click();
    await h.arrived;
    await d.locator("form").evaluate((f) => f.requestSubmit());
    assert.equal(writes, beforeMove + 1);
    assert(
      await d.getByRole("button", { name: "取消", exact: true }).isDisabled(),
    );
    h.release();
    hold = null;
    await d.waitFor({ state: "hidden" });
    await s.login(owner, p, tenant);
    await s.nav(p, "组织");
    const moved = (await call(p, "GET", base + "/organizations?limit=100")).body
      .items;
    assert.equal(moved.find((v) => v.id === source.id).parent_id, other);
    assert.equal(moved.find((v) => v.id === child).parent_id, source.id);
    phase = "cycle-rejection-and-structure-change-after-preview";
    d = await open(p, source.id);
    await d.getByLabel("上级组织", { exact: true }).selectOption(child);
    await d.getByRole("button", { name: "预览移动", exact: true }).click();
    await d.getByRole("alert").filter({ hasText: "不能把组织移动" }).waitFor();
    assert.equal(
      await d.getByLabel("上级组织", { exact: true }).inputValue(),
      "",
    );
    assert(
      await d
        .getByRole("button", { name: "确认移动", exact: true })
        .isDisabled(),
    );
    await close(p);
    d = await open(p, source.id);
    await d.getByLabel("上级组织", { exact: true }).selectOption(root);
    await preview(p);
    await d.getByLabel(confirm, { exact: true }).check();
    let directory = await call(p, "GET", base + "/organizations?limit=100");
    await call(
      p,
      "POST",
      base + "/organizations/" + child + "/rename",
      { name: "实际子节点已更新" },
      directory.etag,
    );
    await d.getByRole("button", { name: "确认移动", exact: true }).click();
    await d
      .getByRole("alert")
      .filter({ hasText: "资料已被其他操作更新" })
      .waitFor();
    assert.equal(
      await d.getByLabel(confirm, { exact: true }).isChecked(),
      false,
    );
    await close(p);
    // Source row still has the pre-rename version: fresh directory must refuse it.
    await p
      .locator('tr[data-record-id="' + source.id + '"]')
      .getByRole("button", { name: "移动组织", exact: true })
      .click();
    d = dialog(p);
    await d
      .getByRole("alert")
      .filter({ hasText: "上级目录或移动预览已变化" })
      .waitFor();
    assert.equal(
      await d.getByLabel("上级组织", { exact: true }).inputValue(),
      "",
    );
    await close(p);
    await s.nav(p, "空间概览");
    await s.nav(p, "组织");
    phase = "cancel-preview-and-context-late-responses";
    d = await open(p, source.id);
    await d.getByLabel("上级组织", { exact: true }).selectOption(root);
    h = arm(
      (url, m, b) =>
        m === "POST" && url.endsWith("/move") && b.dry_run === true,
    );
    await d.getByRole("button", { name: "预览移动", exact: true }).click();
    await h.arrived;
    await close(p);
    h.release();
    hold = null;
    d = await open(p, source.id);
    assert.equal(
      await d.getByRole("status").filter({ hasText: "将移动" }).count(),
      0,
    );
    await close(p);
    h = arm((url, m) => m === "GET" && url.includes("/organizations?limit=25"));
    await p.getByRole("button", { name: "创建组织", exact: true }).click();
    await h.arrived;
    await p.evaluate(() => {
      location.hash = "#workspace=" + "0".repeat(32);
    });
    await dialog(p).waitFor({ state: "hidden" });
    h.release();
    hold = null;
    await s.login(owner, p, secondary);
    await s.nav(p, "组织");
    assert.equal(await dialog(p).count(), 0);
    assert.equal(
      await p.getByText("末页创建的子组织", { exact: true }).count(),
      0,
    );
    await s.login(owner, p, tenant);
    await s.nav(p, "组织");
    phase = "malformed-and-unavailable-pages-and-preview-clear-selection";
    for (const kind of ["invalid", "unavailable"]) {
      d = await open(p);
      await d.getByLabel("上级组织", { exact: true }).selectOption(root);
      fault = {
        kind,
        match: (url, m) => m === "GET" && url.includes("after="),
      };
      await d
        .getByRole("button", { name: "加载更多组织", exact: true })
        .click();
      await d.getByRole("alert").waitFor();
      assert.equal(
        await d.getByLabel("上级组织", { exact: true }).inputValue(),
        "",
      );
      assert.equal(
        await d
          .getByLabel("上级组织", { exact: true })
          .locator("option")
          .count(),
        1,
      );
      assert(!(await d.innerText()).includes("private-directory-marker"));
      await close(p);
    }
    d = await open(p, source.id);
    await d.getByLabel("上级组织", { exact: true }).selectOption(root);
    fault = {
      kind: "invalid",
      match: (url, m, b) => m === "POST" && b.dry_run === true,
    };
    await d.getByRole("button", { name: "预览移动", exact: true }).click();
    await d.getByRole("alert").waitFor();
    assert.equal(
      await d.getByRole("status").filter({ hasText: "将移动" }).count(),
      0,
    );
    assert(
      await d
        .getByRole("button", { name: "确认移动", exact: true })
        .isDisabled(),
    );
    await close(p);
    phase = "committed-lost-create-is-not-retried";
    d = await open(p);
    await d.getByLabel("组织名称").fill("已提交丢包的组织");
    await d.getByLabel("上级组织", { exact: true }).selectOption(root);
    fault = {
      kind: "lost",
      match: (url, m, b) => m === "POST" && b.name === "已提交丢包的组织",
    };
    const beforeLost = writes;
    await d.getByRole("button", { name: "保存并继续", exact: true }).click();
    await d.getByRole("alert").waitFor();
    await d.locator("form").evaluate((f) => f.requestSubmit());
    assert.equal(writes, beforeLost + 1);
    directory = await call(p, "GET", base + "/organizations?limit=100");
    assert.equal(
      directory.body.items.filter((v) => v.name === "已提交丢包的组织").length,
      1,
    );
    await close(p);
    phase = "committed-lost-move-requires-authority-refresh-no-replay";
    await s.nav(p, "空间概览");
    await s.nav(p, "组织");
    d = await open(p, source.id);
    await d.getByLabel("上级组织", { exact: true }).selectOption(root);
    await preview(p);
    await d.getByLabel(confirm, { exact: true }).check();
    fault = {
      kind: "lost",
      match: (url, m, b) =>
        m === "POST" && url.endsWith("/move") && b.dry_run === false,
    };
    const beforeLostMove = writes;
    await d.getByRole("button", { name: "确认移动", exact: true }).click();
    await d.getByRole("alert").waitFor();
    await d.locator("form").evaluate((f) => f.requestSubmit());
    assert.equal(writes, beforeLostMove + 1);
    assert.equal(
      await d.getByLabel(confirm, { exact: true }).isChecked(),
      false,
    );
    await close(p);
    await s.login(owner, p, tenant);
    directory = await call(p, "GET", base + "/organizations?limit=100");
    assert.equal(
      directory.body.items.find((v) => v.id === source.id).parent_id,
      root,
    );
    assert.equal(
      directory.body.items.find((v) => v.id === child).parent_id,
      source.id,
    );
    const audit = (await call(p, "GET", base + "/audit-events?limit=100")).body
      .items;
    assert.equal(
      audit.filter((v) => v.action === "org.moved" && v.target_id === source.id)
        .length,
      2,
    );
    phase = "ordinary-organization-delegate-and-live-revocation";
    await s.login(guest);
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
    const accepted = (
      await call(
        guest.page,
        "POST",
        "/v1/invitations/accept",
        { token: invitation.token },
        undefined,
        201,
      )
    ).body;
    await s.login(owner, p, tenant);
    let version = (await call(p, "GET", base + "/permissions")).etag;
    const role = (
      await call(
        p,
        "POST",
        base + "/roles",
        {
          code: "hierarchy_operator",
          policies: [
            { permission: "organizations.read", effect: "allow" },
            { permission: "organizations.write", effect: "allow" },
          ],
        },
        version,
        201,
      )
    ).body.id;
    await s.login(owner, p, tenant);
    version = (await call(p, "GET", base + "/permissions")).etag;
    await call(
      p,
      "PUT",
      base + "/members/" + accepted.membership_id + "/roles/" + role,
      {},
      version,
      204,
    );
    await s.login(owner, p, tenant);
    await s.login(guest, guest.page, tenant);
    await s.nav(guest.page, "组织");
    d = await open(guest.page);
    await d.getByLabel("组织名称").fill("委托创建的组织");
    await all(d);
    await d.getByLabel("上级组织", { exact: true }).selectOption(last);
    await d.getByRole("button", { name: "保存并继续", exact: true }).click();
    await d.waitFor({ state: "hidden" });
    assert.equal(
      await guest.page
        .getByRole("button", { name: "关联成员", exact: true })
        .count(),
      0,
    );
    await s.nav(p, "组织");
    d = await open(guest.page);
    version = (await call(p, "GET", base + "/permissions")).etag;
    await call(
      p,
      "DELETE",
      base + "/members/" + accepted.membership_id + "/roles/" + role,
      {},
      version,
      204,
    );
    await d.getByRole("button", { name: "加载更多组织", exact: true }).click();
    await guest.page
      .getByRole("link", { name: "重新确认空间授权", exact: true })
      .waitFor();
    assert.equal(await dialog(guest.page).count(), 0);
    assert.equal(errors.length, 0);
    const privacy = require("../support/private-material.cjs")(
      s.root,
      [
        ...secrets,
        ...s.fixtures.flatMap((f) => [f.password, f.totpSecret, ...f.codes]),
      ],
      started,
    );
    console.log(
      "Organization hierarchy passed: " +
        checks +
        " unique real BFF contract responses; 56 initial organizations, real last-page create/subtree move, preview binding/page invalidation, cycles/stale version, cancel/context guards, malformed/dependency failures, committed lost reply, delegated operator/live revoke; " +
        writes +
        " write attempts and " +
        previews +
        " previews; private exclusion in " +
        privacy.files +
        " files/runtime logs",
    );
  } finally {
    if (hold) hold.release();
    clearInterval(progress);
    // Shared cleanup verifies the exact registered account set and demo exclusion.
    await s.close();
  }
})().catch((e) => {
  console.error(
    "Organization hierarchy failed: phase=" +
      phase +
      ", error=" +
      e.name +
      " (private details withheld)",
  );
  console.error(
    JSON.stringify({
      checks,
      writes,
      previews,
      forwardingErrors: errors.length,
      frames: [
        ...String(e.stack).matchAll(
          /organization-hierarchy\/browser\.cjs:(\d+):/g,
        ),
      ].map((v) => Number(v[1])),
    }),
  );
  process.exitCode = 1;
});
