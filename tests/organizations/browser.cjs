// Real Hydra login -> browser BFF -> public API -> business MySQL.
// Proofs/cookies/tokens and raw browser exceptions never enter output.
const s = require("../support/console-fixtures.cjs"),
  assert = require("node:assert/strict"),
  { execFileSync } = require("node:child_process"),
  path = require("node:path");
let phase = "startup",
  checks = 0;
const progress = setInterval(
  () => console.log("Organization acceptance phase: " + phase),
  20000,
);
function normalized(url) {
  return url
    .split("?")[0]
    .replace(/\/tenants\/[a-f0-9]{32}/, "/tenants/{tenant}")
    .replace(/\/organizations\/[a-f0-9]{32}/, "/organizations/{org}")
    .replace(/\/members\/[a-f0-9]{32}/, "/members/{member}");
}
async function call(page, method, url, body, status = 200, etag) {
  const r = await s.bff(page, url, {
    method,
    body,
    headers: etag ? { "If-Match": etag } : {},
  });
  if (r.status !== status)
    console.error(
      "Unexpected status: " +
        method +
        " " +
        normalized(url) +
        " expected=" +
        status +
        " received=" +
        r.status,
    );
  assert.equal(r.status, status);
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
  return r;
}
async function confirm(page) {
  const d = page.getByRole("dialog");
  await d.getByLabel("我确认对当前组织执行此操作").check();
  await d.getByRole("button", { name: "确认操作", exact: true }).click();
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
    const invitation = await s.bff(p, base + "/invitations", {
      method: "POST",
      body: { email: guest.email },
    });
    assert.equal(invitation.status, 201);
    const joined = await s.bff(guest.page, "/v1/invitations/accept", {
      method: "POST",
      body: { token: invitation.body.token },
    });
    assert.equal(joined.status, 201);
    const member = joined.body.membership_id;
    await s.login(owner, p, tenant);
    await s.login(guest, guest.page, tenant);
    const otherTenant = (await s.bff(p, "/v1/tenants")).body.items.find(
      (t) => t.id !== tenant,
    ).id;
    const otherPage = await owner.context.newPage();
    await s.login(owner, otherPage, otherTenant);
    const otherRoot = (await s.bff(otherPage, "/v1/tenants/" + otherTenant))
      .body.root_id;
    const root = (await s.bff(p, base)).body.root_id;
    async function directory() {
      return call(p, "GET", base + "/organizations");
    }
    async function create(name, parent = root) {
      const list = await directory();
      return (
        await call(
          p,
          "POST",
          base + "/organizations",
          { parent_id: parent, name },
          201,
          list.etag,
        )
      ).body.id;
    }
    phase = "root-nonempty-cross-space-validation-and-etags";
    const parent = await create("工程中心"),
      child = await create("平台小组", parent),
      empty = await create("临时工作组");
    let list = await directory();
    const target = base + "/organizations/" + child;
    await call(p, "POST", target + "/rename", { name: "Name" }, 428);
    await call(
      p,
      "POST",
      target + "/rename",
      { name: "Name" },
      400,
      '"authz-1"',
    );
    for (const body of [
      { name: "   " },
      { name: "中".repeat(43) },
      { name: "Name", owner: true },
    ])
      await call(p, "POST", target + "/rename", body, 400, list.etag);
    await call(
      p,
      "POST",
      base + "/organizations/" + root + "/rename",
      { name: "Space overwrite" },
      409,
      list.etag,
    );
    await call(
      p,
      "DELETE",
      base + "/organizations/" + root,
      {},
      409,
      list.etag,
    );
    await call(
      p,
      "DELETE",
      base + "/organizations/" + parent,
      {},
      409,
      list.etag,
    );
    await call(
      p,
      "DELETE",
      base + "/organizations/" + otherRoot,
      {},
      404,
      list.etag,
    );
    await call(
      p,
      "GET",
      base + "/organizations/" + otherRoot + "/members",
      undefined,
      404,
    );
    await call(
      p,
      "POST",
      base + "/organizations/" + otherRoot + "/rename",
      { name: "Foreign" },
      404,
      list.etag,
    );
    let renamed = await call(
      p,
      "POST",
      target + "/rename",
      { name: "  平台工程组  " },
      200,
      list.etag,
    );
    await call(
      p,
      "POST",
      target + "/rename",
      { name: "Stale" },
      412,
      list.etag,
    );
    const noop = await call(
      p,
      "POST",
      target + "/rename",
      { name: "平台工程组" },
      200,
      renamed.etag,
    );
    assert.equal(noop.etag, renamed.etag);
    assert.equal((await s.bff(p, "/v1/me")).status, 200);
    phase = "ordinary-member-denied";
    await call(guest.page, "GET", target + "/members", undefined, 403);
    await call(
      guest.page,
      "POST",
      target + "/rename",
      { name: "No privilege" },
      403,
      renamed.etag,
    );
    await call(guest.page, "DELETE", target, {}, 403, renamed.etag);
    let detail = (await s.bff(p, base)).body;
    await call(
      guest.page,
      "DELETE",
      target + "/members/" + member,
      {},
      403,
      '"authz-' + detail.authz_version + '"',
    );
    phase = "direct-members-pagination-and-empty-protection";
    for (const id of [me.membership_id, member]) {
      detail = (await s.bff(p, base)).body;
      await call(
        p,
        "PUT",
        target + "/members/" + id,
        {},
        204,
        '"authz-' + detail.authz_version + '"',
      );
      await s.login(owner, p, tenant);
    }
    const members = await call(p, "GET", target + "/members?limit=1");
    assert.equal(members.body.items.length, 1);
    assert(members.body.next_cursor);
    const next = await call(
      p,
      "GET",
      target + "/members?after=" + members.body.next_cursor,
    );
    assert.equal(next.body.items.length, 1);
    assert.notEqual(next.body.items[0].id, members.body.items[0].id);
    await call(p, "GET", target + "/members?limit=101", undefined, 400);
    await call(p, "DELETE", target, {}, 409, renamed.etag);
    phase = "recent-mfa-on-all-new-writes";
    execFileSync(
      "python3",
      ["tests/api/fixtures.py", "age-session", me.user_id, me.session_id],
      { cwd: s.root, stdio: "pipe" },
    );
    for (const [method, url, body, etag] of [
      ["POST", target + "/rename", { name: "Aged MFA" }, renamed.etag],
      ["DELETE", base + "/organizations/" + empty, {}, renamed.etag],
      ["DELETE", target + "/members/" + member, {}, members.etag],
    ]) {
      const denied = await call(p, method, url, body, 403, etag);
      assert.equal(denied.body.code, "recent_mfa_required");
    }
    await call(p, "GET", target + "/members");
    me = await s.login(owner, p, tenant, true);
    phase = "console-rename-and-direct-members-desktop-mobile";
    phase = "ui-organizations-navigation";
    await s.nav(p, "组织");
    phase = "ui-open-rename";
    let row = p
      .getByRole("row")
      .filter({ has: p.getByText("平台工程组", { exact: true }) });
    await row.getByRole("button", { name: "重命名", exact: true }).click();
    phase = "ui-rename-form";
    await p.getByLabel("新组织名称").fill("基础平台组");
    assert(
      await p
        .getByRole("dialog")
        .getByRole("button", { name: "确认操作", exact: true })
        .isDisabled(),
    );
    await confirm(p);
    await p.getByRole("status").filter({ hasText: "组织名称已更新" }).waitFor();
    row = p
      .getByRole("row")
      .filter({ has: p.getByText("基础平台组", { exact: true }) });
    await row
      .getByRole("button", { name: "查看直属成员", exact: true })
      .click();
    phase = "ui-direct-members-load";
    await p
      .getByRole("dialog")
      .getByRole("row")
      .filter({ hasText: guest.email })
      .waitFor();
    await p.screenshot({
      path: path.join(s.root, "docs/ui/organization-members-desktop.png"),
      fullPage: true,
    });
    await p.setViewportSize({ width: 390, height: 844 });
    assert(
      await p.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth,
      ),
    );
    const mobileAction = await p
      .getByRole("dialog")
      .getByRole("button", { name: "移除归属", exact: true })
      .first()
      .boundingBox();
    assert(mobileAction && mobileAction.height < 50);
    await p.screenshot({
      path: path.join(s.root, "docs/ui/organization-members-mobile.png"),
      fullPage: true,
    });
    await p.setViewportSize({ width: 1280, height: 720 });
    await p
      .getByRole("button", { name: "关闭组织对话框", exact: true })
      .click();
    phase = "late-real-response-after-closing-dialog";
    let release, seen, finished;
    const gate = new Promise((r) => (release = r)),
      arrived = new Promise((r) => (seen = r)),
      done = new Promise((r) => (finished = r));
    let routeFailed = false;
    const pattern = "**/bff" + target + "/members?*";
    await p.route(pattern, async (route) => {
      try {
        const original = new URL(route.request().url());
        const response = await route.fetch({
          url: "http://127.0.0.1:3003" + original.pathname + original.search,
          headers: {
            ...(await route.request().allHeaders()),
            host: "console.localhost:3003",
          },
          maxRetries: 0,
          maxRedirects: 0,
        });
        if (response.status() !== 200) routeFailed = true;
        seen();
        await gate;
        await route.fulfill({ response }).catch(() => {});
      } catch {
        routeFailed = true;
        seen();
      } finally {
        finished();
      }
    });
    try {
      await row
        .getByRole("button", { name: "查看直属成员", exact: true })
        .click();
      await arrived;
      await p
        .getByRole("button", { name: "关闭组织对话框", exact: true })
        .click();
      release();
      await done;
      assert(!routeFailed);
      assert.equal(await p.getByRole("dialog").count(), 0);
    } finally {
      release();
      await p.unroute(pattern);
    }
    phase = "ui-nonempty-refusal-and-member-removal";
    const parentRow = p
      .getByRole("row")
      .filter({ has: p.locator("strong").filter({ hasText: /^工程中心$/ }) });
    await parentRow
      .getByRole("button", { name: "删除空组织", exact: true })
      .click();
    await confirm(p);
    await p
      .getByRole("dialog")
      .getByRole("alert")
      .filter({ hasText: "请先移动子组织" })
      .waitFor();
    assert(
      await p
        .getByRole("dialog")
        .getByRole("button", { name: "确认操作", exact: true })
        .isDisabled(),
    );
    await p
      .getByRole("button", { name: "关闭组织对话框", exact: true })
      .click();
    await row
      .getByRole("button", { name: "查看直属成员", exact: true })
      .click();
    const guestRow = p
      .getByRole("dialog")
      .getByRole("row")
      .filter({ hasText: guest.email });
    await guestRow
      .getByRole("button", { name: "移除归属", exact: true })
      .click();
    await confirm(p);
    await p
      .getByRole("link", { name: "重新确认空间授权", exact: true })
      .waitFor();
    assert.equal((await s.bff(p, "/v1/me")).status, 401);
    assert.equal((await s.bff(otherPage, "/v1/me")).status, 200);
    await s.login(owner, p, tenant);
    const remaining = await call(p, "GET", target + "/members");
    assert.deepEqual(
      remaining.body.items.map((x) => x.id),
      [me.membership_id],
    );
    detail = (await s.bff(p, base)).body;
    await call(
      p,
      "DELETE",
      target + "/members/" + member,
      {},
      204,
      remaining.etag,
    ); // no-op
    assert.equal(
      (await s.bff(p, base)).body.authz_version,
      detail.authz_version,
    );
    await call(
      p,
      "DELETE",
      target + "/members/" + me.membership_id,
      {},
      204,
      remaining.etag,
    );
    await s.login(owner, p, tenant);
    phase = "ui-empty-deletion-and-closed-graph";
    await s.nav(p, "组织");
    row = p
      .getByRole("row")
      .filter({ has: p.getByText("基础平台组", { exact: true }) });
    await row.getByRole("button", { name: "删除空组织", exact: true }).click();
    await confirm(p);
    await p
      .getByRole("link", { name: "重新确认空间授权", exact: true })
      .waitFor();
    await s.login(owner, p, tenant);
    list = await directory();
    assert(!list.body.items.some((x) => x.id === child));
    await call(p, "DELETE", target, {}, 404, list.etag);
    const stillMember = (await s.bff(p, base + "/members")).body.items.find(
      (x) => x.id === member,
    );
    assert(stillMember && stillMember.status === "active");
    assert.equal((await s.bff(p, base)).body.owner, true);
    const audit = (await s.bff(p, base + "/audit-events?limit=100")).body.items;
    assert.equal(audit.filter((x) => x.action === "org.renamed").length, 2);
    assert.equal(
      audit.filter((x) => x.action === "org.member_unassigned").length,
      2,
    );
    assert.equal(audit.filter((x) => x.action === "org.deleted").length, 1);
    phase = "delegated-ui-member-privacy-and-deny";
    const roleVersion = (await s.bff(p, base)).body.authz_version;
    const createdRole = await s.bff(p, base + "/roles", {
      method: "POST",
      body: {
        code: "directory_reader",
        policies: [{ permission: "organizations.read", effect: "allow" }],
      },
      headers: { "If-Match": '"authz-' + roleVersion + '"' },
    });
    assert.equal(createdRole.status, 201);
    await s.login(owner, p, tenant);
    let av = (await s.bff(p, base)).body.authz_version;
    assert.equal(
      (
        await s.bff(
          p,
          base + "/members/" + member + "/roles/" + createdRole.body.id,
          {
            method: "PUT",
            body: {},
            headers: { "If-Match": '"authz-' + av + '"' },
          },
        )
      ).status,
      204,
    );
    await s.login(owner, p, tenant);
    await s.login(guest, guest.page, tenant);
    await s.nav(guest.page, "组织");
    assert.equal(
      await guest.page
        .getByRole("button", { name: "查看直属成员", exact: true })
        .count(),
      0,
    );
    await call(
      guest.page,
      "GET",
      base + "/organizations/" + parent + "/members",
      undefined,
      403,
    );
    for (const deny of [false, true]) {
      av = (await s.bff(p, base)).body.authz_version;
      const policies = [
        { permission: "organizations.read", effect: "allow" },
        { permission: "members.read", effect: "allow" },
      ];
      if (deny) policies.push({ permission: "members.read", effect: "deny" });
      assert.equal(
        (
          await s.bff(p, base + "/roles/" + createdRole.body.id, {
            method: "PUT",
            body: { policies },
            headers: { "If-Match": '"authz-' + av + '"' },
          })
        ).status,
        204,
      );
      await s.login(owner, p, tenant);
      await s.login(guest, guest.page, tenant);
      await call(
        guest.page,
        "GET",
        base + "/organizations/" + parent + "/members",
        undefined,
        deny ? 403 : 200,
      );
      await s.nav(guest.page, "组织");
      if (deny)
        assert.equal(
          await guest.page
            .getByRole("button", { name: "查看直属成员", exact: true })
            .count(),
          0,
        );
      else {
        await guest.page
          .getByRole("row")
          .filter({
            has: guest.page.locator("strong").filter({ hasText: /^工程中心$/ }),
          })
          .getByRole("button", { name: "查看直属成员", exact: true })
          .click();
        await guest.page
          .getByRole("dialog")
          .getByText("没有直接关联的成员。", { exact: true })
          .waitFor();
        assert.equal(
          await guest.page
            .getByRole("dialog")
            .getByRole("button", { name: "移除归属", exact: true })
            .count(),
          0,
        );
        await guest.page
          .getByRole("button", { name: "关闭组织对话框", exact: true })
          .click();
      }
    }
    phase = "playground-new-operation";
    await s.nav(p, "API Playground");
    await p
      .getByLabel("选择 API", { exact: true })
      .selectOption("listOrganizationMembers");
    await p.getByLabel("org", { exact: true }).fill(parent);
    await p.getByRole("button", { name: "发送读取请求", exact: true }).click();
    await p
      .getByRole("region", { name: "API 响应", exact: true })
      .getByRole("heading", { name: "HTTP 200", exact: true })
      .waitFor();
    assert.deepEqual(s.pageErrors, []);
    assert.deepEqual(s.leaks, []);
    console.log(
      "Organization lifecycle acceptance passed: " +
        checks +
        " contract responses; root/nonempty/isolation/ETag/MFA/permissions, direct pagination, no-op, rename, removal, delete, revocation, other-space retention, audit, UI/mobile, late response and Playground",
    );
  } finally {
    clearInterval(progress);
    await s.close();
  }
})().catch((e) => {
  console.error(
    "Organization acceptance failed: phase=" +
      phase +
      ", error=" +
      e.name +
      " (private details withheld)",
  );
  console.error(JSON.stringify(s.diagnostics()));
  process.exitCode = 1;
});
