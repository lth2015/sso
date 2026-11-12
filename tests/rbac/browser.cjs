// Real console sessions and public-contract calls; no fabricated identities/JWTs.
const support = require("../support/console-fixtures.cjs");
const { execFileSync } = require("node:child_process");
const assert = require("node:assert/strict"),
  fs = require("node:fs");
let phase = "startup",
  checks = 0,
  success = false;
const coverage = new Set();
const progress = setInterval(
  () => console.log("RBAC acceptance phase: " + phase),
  20000,
);
function normalized(url) {
  return url
    .split("?")[0]
    .replace(/\/tenants\/[a-f0-9]{32}/, "/tenants/{tenant}")
    .replace(/\/members\/[a-f0-9]{32}/, "/members/{member}")
    .replace(/\/roles\/[a-f0-9]{32}/, "/roles/{role}");
}
async function call(page, method, path, body, status = 200, etag) {
  const result = await support.bff(page, path, {
    method,
    body,
    headers: etag ? { "If-Match": etag } : {},
  });
  assert.equal(
    result.status,
    status,
    method + " " + normalized(path) + " status",
  );
  const sample = {
    path: "/bff" + normalized(path),
    contract: "console",
    method,
    status,
    body: result.body,
    request_body: body,
    content_type: result.contentType,
    request_id: result.requestID,
  };
  execFileSync("python3", ["tests/api/validate_contract.py"], {
    cwd: support.root,
    input: JSON.stringify(sample),
    stdio: ["pipe", "pipe", "pipe"],
  });
  checks++;
  coverage.add(method.toLowerCase() + " " + normalized(path));
  return result;
}
async function authorize(f, tenant) {
  return support.login(f, f.page, tenant);
}
async function version(page, base) {
  return (await call(page, "GET", base + "/permissions")).etag;
}
async function reauthOwner(owner, tenant) {
  await authorize(owner, tenant);
}
(async () => {
  try {
    await support.start();
    phase = "verified-accounts";
    const owner = await support.register(true),
      guest = await support.register(),
      other = await support.register();
    const me = await authorize(owner),
      tenant = me.tenant_id,
      base = "/v1/tenants/" + tenant;
    const p = owner.page;
    // Invite both verified users through actual console/API flows.
    for (const f of [guest, other]) {
      const own = await support.bff(p, base + "/invitations", {
        method: "POST",
        body: { email: f.email },
      });
      assert.equal(own.status, 201);
      await authorize(f);
      assert.equal(
        (
          await support.bff(f.page, "/v1/invitations/accept", {
            method: "POST",
            body: { token: own.body.token },
          })
        ).status,
        201,
      );
      await reauthOwner(owner, tenant);
    }
    await authorize(guest, tenant);
    await authorize(other, tenant);
    let members = (await call(p, "GET", base + "/members")).body.items;
    const guestMember = members.find((x) => x.email === guest.email).id,
      otherMember = members.find((x) => x.email === other.email).id;
    phase = "default-deny-and-protected-role-management";
    let effective = await call(guest.page, "GET", base + "/permissions");
    assert.deepEqual(effective.body.permissions, []);
    await call(guest.page, "GET", base + "/roles", undefined, 403);
    await call(
      guest.page,
      "POST",
      base + "/roles",
      {
        code: "escalate",
        policies: [{ permission: "audit.read", effect: "allow" }],
      },
      403,
      await version(guest.page, base),
    );
    const roles = (await call(p, "GET", base + "/roles")).body.items;
    const ownerRole = roles.find((x) => x.built_in).id;
    await call(
      p,
      "PUT",
      base + "/roles/" + ownerRole,
      { policies: [] },
      409,
      await version(p, base),
    );
    await call(
      p,
      "DELETE",
      base + "/roles/" + ownerRole,
      {},
      409,
      await version(p, base),
    );
    await call(
      p,
      "DELETE",
      base + "/members/" + me.membership_id + "/roles/" + ownerRole,
      {},
      409,
      await version(p, base),
    );
    await call(
      p,
      "POST",
      base + "/roles",
      { code: "owner", policies: [] },
      400,
      await version(p, base),
    );
    await call(
      p,
      "POST",
      base + "/roles",
      {
        code: "reserved",
        policies: [{ permission: "roles.manage", effect: "allow" }],
      },
      400,
      await version(p, base),
    );
    await call(
      p,
      "POST",
      base + "/roles",
      { code: "unknown", policies: [{ permission: "*", effect: "allow" }] },
      400,
      await version(p, base),
    );
    const create = async (code, policies) => {
      const r = await call(
        p,
        "POST",
        base + "/roles",
        { code, policies },
        201,
        await version(p, base),
      );
      await call(p, "GET", base, undefined, 401);
      await reauthOwner(owner, tenant);
      return r.body.id;
    };
    const binding = async (member, role, remove = false) => {
      await call(
        p,
        remove ? "DELETE" : "PUT",
        base + "/members/" + member + "/roles/" + role,
        {},
        204,
        await version(p, base),
      );
      await reauthOwner(owner, tenant);
    };
    phase = "granular-read-delegation";
    const readRole = await create("directory_reader", [
      { permission: "members.read", effect: "allow" },
    ]);
    await binding(guestMember, readRole);
    await call(guest.page, "GET", base + "/members", undefined, 401);
    await authorize(guest, tenant);
    assert(
      (
        await call(guest.page, "GET", base + "/permissions")
      ).body.permissions.includes("members.read"),
    );
    await call(guest.page, "GET", base + "/members");
    await call(guest.page, "GET", base + "/audit-events", undefined, 403);
    await call(
      guest.page,
      "POST",
      base + "/members/" + otherMember + "/suspend",
      {},
      403,
      await version(guest.page, base),
    );
    await support.nav(guest.page, "成员");
    assert.equal(
      await guest.page
        .getByRole("button", { name: "停用成员", exact: true })
        .count(),
      0,
    );
    assert.equal(
      await guest.page
        .getByRole("navigation", { name: "主导航" })
        .getByRole("button", { name: "角色与权限", exact: true })
        .count(),
      0,
    );
    phase = "delegate-write-without-owner-authority";
    const operator = await create("member_operator", [
      { permission: "members.write", effect: "allow" },
    ]);
    await binding(guestMember, operator);
    await authorize(guest, tenant);
    await call(
      guest.page,
      "POST",
      base + "/members/" + me.membership_id + "/suspend",
      {},
      403,
      await version(guest.page, base),
    );
    await call(
      guest.page,
      "PUT",
      base + "/members/" + guestMember + "/roles/" + ownerRole,
      {},
      403,
      await version(guest.page, base),
    );
    await call(
      guest.page,
      "POST",
      base + "/members/" + otherMember + "/suspend",
      {},
      204,
      await version(guest.page, base),
    );
    await call(other.page, "GET", "/v1/me", undefined, 401);
    await reauthOwner(owner, tenant);
    await authorize(guest, tenant);
    await call(
      guest.page,
      "POST",
      base + "/members/" + otherMember + "/restore",
      {},
      204,
      await version(guest.page, base),
    );
    await reauthOwner(owner, tenant);
    await authorize(other, tenant);
    await authorize(guest, tenant);
    phase = "explicit-deny-and-policy-update";
    const denied = await create("deny_directory", [
      { permission: "members.read", effect: "deny" },
    ]);
    await binding(guestMember, denied);
    await authorize(guest, tenant);
    const blocked = await call(guest.page, "GET", base + "/permissions");
    assert(blocked.body.denied.includes("members.read"));
    await call(guest.page, "GET", base + "/members", undefined, 403);
    await call(
      p,
      "DELETE",
      base + "/roles/" + denied,
      {},
      409,
      await version(p, base),
    );
    await call(
      p,
      "PUT",
      base + "/roles/" + denied,
      { policies: [{ permission: "audit.read", effect: "allow" }] },
      204,
      await version(p, base),
    );
    await reauthOwner(owner, tenant);
    await authorize(guest, tenant);
    await call(guest.page, "GET", base + "/members");
    await call(guest.page, "GET", base + "/audit-events");
    phase = "no-op-binding-and-stale-preconditions";
    const current = await version(p, base);
    await call(
      p,
      "PUT",
      base + "/members/" + guestMember + "/roles/" + denied,
      {},
      204,
      current,
    );
    assert.equal(await version(p, base), current);
    await call(
      p,
      "PUT",
      base + "/roles/" + denied,
      { policies: [{ permission: "audit.read", effect: "allow" }] },
      204,
      current,
    );
    assert.equal(await version(p, base), current);
    await call(
      p,
      "PUT",
      base + "/roles/" + denied,
      { policies: [] },
      412,
      '"authz-1"',
    );
    phase = "revocation-and-historical-binding-audit";
    await binding(guestMember, denied, true);
    await call(guest.page, "GET", base + "/audit-events", undefined, 401);
    await authorize(guest, tenant);
    await call(guest.page, "GET", base + "/audit-events", undefined, 403);
    await call(
      p,
      "DELETE",
      base + "/roles/" + denied,
      {},
      204,
      await version(p, base),
    );
    await reauthOwner(owner, tenant);
    const events = (await call(p, "GET", base + "/audit-events?limit=100")).body
      .items;
    assert(
      events.some(
        (x) =>
          x.action === "role.unbound" &&
          x.role_code === "deny_directory" &&
          x.membership_id === guestMember,
      ),
    );
    await call(p, "GET", base + "/members/" + guestMember + "/roles");
    phase = "reserved-owner-deny-recovery";
    const ownerDeny = await create("owner_audit_denied", [
      { permission: "audit.read", effect: "deny" },
    ]);
    await binding(me.membership_id, ownerDeny);
    await call(p, "GET", base + "/audit-events", undefined, 403);
    assert(
      (await call(p, "GET", base + "/permissions")).body.permissions.includes(
        "roles.manage",
      ),
    );
    await call(p, "GET", base + "/roles");
    await binding(me.membership_id, ownerDeny, true);
    await call(p, "GET", base + "/audit-events");
    phase = "owner-binding-transfer-and-last-owner";
    await binding(otherMember, ownerRole);
    await authorize(other, tenant);
    await call(
      other.page,
      "DELETE",
      base + "/members/" + me.membership_id + "/roles/" + ownerRole,
      {},
      204,
      await version(other.page, base),
    );
    await authorize(other, tenant);
    await authorize(owner, tenant);
    await call(p, "GET", base + "/roles", undefined, 403);
    await call(
      other.page,
      "DELETE",
      base + "/members/" + otherMember + "/roles/" + ownerRole,
      {},
      409,
      await version(other.page, base),
    );
    await call(
      other.page,
      "PUT",
      base + "/members/" + me.membership_id + "/roles/" + ownerRole,
      {},
      204,
      await version(other.page, base),
    );
    await reauthOwner(owner, tenant);
    await authorize(other, tenant);
    phase = "cross-tenant-object-guards";
    const spaces = (await support.bff(p, "/v1/tenants")).body.items;
    const second = spaces.find((x) => x.id !== tenant).id;
    const tab = await owner.context.newPage();
    await support.login(owner, tab, second);
    const foreignRoles = (
      await call(tab, "GET", "/v1/tenants/" + second + "/roles")
    ).body.items;
    await call(
      p,
      "PUT",
      base + "/members/" + guestMember + "/roles/" + foreignRoles[0].id,
      {},
      404,
      await version(p, base),
    );
    await call(
      p,
      "PUT",
      base + "/roles/" + foreignRoles[0].id,
      { policies: [] },
      404,
      await version(p, base),
    );
    await tab.close();
    phase = "react-role-create-and-binding";
    await support.nav(p, "角色与权限");
    phase = "react-create-role-form";
    await p.getByRole("button", { name: "创建角色", exact: true }).click();
    await p.getByLabel("角色标识").fill("console_auditor");
    await p.getByLabel("查看审计记录", { exact: true }).selectOption("allow");
    await p.getByRole("button", { name: "保存角色变更", exact: true }).click();
    phase = "react-create-role-reauthorization";
    await p
      .getByRole("link", { name: "重新确认空间授权", exact: true })
      .waitFor();
    await reauthOwner(owner, tenant);
    phase = "react-open-member-binding";
    await support.nav(p, "成员");
    await p
      .getByRole("row")
      .filter({ hasText: guest.email })
      .getByRole("button", { name: "管理角色", exact: true })
      .click();
    await p
      .getByRole("heading", { name: "管理成员角色", exact: true })
      .waitFor();
    const choice = (
      await call(p, "GET", base + "/roles?limit=100")
    ).body.items.find((x) => x.code === "console_auditor");
    phase = "react-submit-member-binding";
    await p.getByLabel("选择角色", { exact: true }).selectOption(choice.id);
    await p.getByRole("button", { name: "保存角色变更", exact: true }).click();
    await p
      .getByRole("link", { name: "重新确认空间授权", exact: true })
      .waitFor();
    await reauthOwner(owner, tenant);
    phase = "react-delegated-audit-navigation";
    await authorize(guest, tenant);
    await support.nav(guest.page, "审计");
    await guest.page
      .getByRole("heading", { name: "空间审计", exact: true })
      .waitFor();
    await guest.page.screenshot({
      path: require("node:path").join(support.root, "docs/ui/rbac-auditor.png"),
      fullPage: true,
    });
    await support.nav(p, "角色与权限");
    await p
      .getByRole("table")
      .getByText("console_auditor", { exact: true })
      .waitFor();
    await p.screenshot({
      path: require("node:path").join(support.root, "docs/ui/rbac-roles.png"),
      fullPage: true,
    });
    phase = "owner-policy-history-boundary";
    const policyHistory = await call(
      p,
      "GET",
      base + "/roles/" + choice.id + "/policy-history",
    );
    assert.equal(policyHistory.body.format, "sso.role.policy-history.v1");
    assert.equal(policyHistory.body.role_id, choice.id);
    assert.equal(policyHistory.body.items.length, 1);
    await call(
      guest.page,
      "GET",
      base + "/roles/" + choice.id + "/policy-history",
      undefined,
      403,
    );
    // Verify all nine role/permission operations produced actual valid responses.
    const spec = JSON.parse(
      fs.readFileSync(
        require("node:path").join(support.root, "api/openapi.json"),
        "utf8",
      ),
    );
    for (const [path, operations] of Object.entries(spec.paths))
      if (/\/(roles|permissions)(\/|$)/.test(path))
        for (const method of Object.keys(operations))
          assert(coverage.has(method + " " + path));
    assert.deepEqual(support.pageErrors, []);
    assert.deepEqual(support.leaks, []);
    success = true;
  } finally {
    clearInterval(progress);
    await support.close();
    if (success)
      console.log(
        "RBAC acceptance passed: " +
          checks +
          " contract-validated real responses; nine role/permission operations, delegated read/write, deny precedence, revocation, role recovery, owner transfer, last-owner protection and React role management",
      );
  }
})().catch((error) => {
  console.error(JSON.stringify(support.diagnostics()));
  console.error(
    "RBAC acceptance failed: phase=" +
      phase +
      ", error=" +
      error.name +
      " (private diagnostics withheld)",
  );
  process.exitCode = 1;
});
