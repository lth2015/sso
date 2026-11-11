const s = require("../support/console-fixtures.cjs"),
  assert = require("node:assert/strict"),
  { execFileSync } = require("node:child_process"),
  path = require("node:path");
let phase = "startup",
  checks = 0,
  writes = 0,
  owner,
  user,
  tenant,
  base,
  members,
  organizations,
  hold = null,
  fault = null;
const errors = [],
  secrets = [],
  started = new Date().toISOString(),
  progress = setInterval(
    () => console.log("Organization assignment phase: " + phase),
    20000,
  );
function population(kind, action, body) {
  return execFileSync(
    "python3",
    [
      kind === "members"
        ? "tests/role-choices/population.py"
        : "tests/organization-assignment/population.py",
      action,
      user,
      tenant,
    ],
    {
      cwd: s.root,
      input: body ? JSON.stringify(body) : undefined,
      stdio: ["pipe", "pipe", "pipe"],
    },
  ).toString();
}
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
  if (!url.startsWith(base + "/") || ![200, 201, 204].includes(r.status))
    validate(url, method, r, body);
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
        method === "PUT" &&
        /\/organizations\/[a-f0-9]{32}\/members\/[a-f0-9]{32}$/.test(u.pathname)
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
      errors.push("assignment forwarding failed");
      if (hold && !hold.used) {
        hold.used = true;
        hold.resolve();
      }
      await route.abort().catch(() => {});
    }
  });
}
const dialog = (p) =>
    p.getByRole("dialog", { name: "关联组织成员", exact: true }),
  confirm = "我确认关联所选成员与组织，归属不授予角色";
async function ready(d) {
  await d.getByRole("status").waitFor({ state: "hidden" });
}
async function open(p) {
  await p.getByRole("button", { name: "关联成员", exact: true }).click();
  const d = dialog(p);
  await d.getByText("已加载 25 个组织", { exact: true }).waitFor();
  await ready(d);
  return d;
}
async function close(p) {
  await dialog(p).getByRole("button", { name: "取消", exact: true }).click();
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
async function choose(d, member, org) {
  await d.getByLabel("选择成员", { exact: true }).selectOption(member);
  await d.getByLabel("选择组织", { exact: true }).selectOption(org);
  await d.getByLabel(confirm, { exact: true }).check();
}
(async () => {
  try {
    await s.start();
    owner = await s.register(true);
    const guest = await s.register();
    let me = await s.login(owner);
    user = me.user_id;
    tenant = me.tenant_id;
    base = "/v1/tenants/" + tenant;
    members = JSON.parse(population("members", "create"));
    organizations = JSON.parse(population("orgs", "create"));
    const p = owner.page,
      secondary = (await s.bff(p, "/v1/tenants")).body.items.find(
        (t) => t.id !== tenant,
      ).id;
    await install(p);
    await install(guest.page);
    phase = "independent-directory-pages-beyond-old-limits";
    await s.nav(p, "组织");
    let d = await open(p);
    assert(
      await d
        .getByRole("button", { name: "保存并继续", exact: true })
        .isDisabled(),
    );
    for (const kind of ["成员", "组织"])
      while (
        await d
          .getByRole("button", { name: "加载更多" + kind, exact: true })
          .count()
      ) {
        await d
          .getByRole("button", { name: "加载更多" + kind, exact: true })
          .click();
        await ready(d);
      }
    assert.equal(
      await d.getByLabel("选择成员", { exact: true }).locator("option").count(),
      107,
    );
    assert.equal(
      await d.getByLabel("选择组织", { exact: true }).locator("option").count(),
      57,
    );
    const target = members.members.at(-1).id,
      org = organizations.orgs.at(-1).id;
    assert.equal(
      (await call(p, "GET", base + "/members/" + target + "/roles")).body.items
        .length,
      0,
    );
    await d.getByLabel("选择成员", { exact: true }).selectOption(target);
    await d.getByLabel("选择组织", { exact: true }).selectOption(org);
    assert(
      await d
        .getByRole("button", { name: "保存并继续", exact: true })
        .isDisabled(),
    );
    await d.getByLabel(confirm, { exact: true }).check();
    await d
      .getByLabel("选择组织", { exact: true })
      .selectOption(organizations.orgs[0].id);
    assert.equal(
      await d.getByLabel(confirm, { exact: true }).isChecked(),
      false,
    );
    await d.getByLabel("选择组织", { exact: true }).selectOption(org);
    await d.getByLabel(confirm, { exact: true }).check();
    await p.setViewportSize({ width: 1365, height: 1000 });
    await p.screenshot({
      path: path.join(s.root, "docs/ui/organization-assignment-desktop.png"),
    });
    await p.setViewportSize({ width: 390, height: 844 });
    assert.equal(
      await p.evaluate(
        () => document.documentElement.scrollWidth > innerWidth + 1,
      ),
      false,
    );
    await p.screenshot({
      path: path.join(s.root, "docs/ui/organization-assignment-mobile.png"),
    });
    await p.setViewportSize({ width: 1365, height: 1000 });
    phase = "confirmed-single-association-and-no-role-grant";
    let h = arm(
      (url, m) =>
        m === "PUT" &&
        url === base + "/organizations/" + org + "/members/" + target,
    );
    await d.getByRole("button", { name: "保存并继续", exact: true }).click();
    await h.arrived;
    assert.equal(writes, 1);
    assert(
      await d.getByRole("button", { name: "取消", exact: true }).isDisabled(),
    );
    await p.keyboard.press("Escape");
    assert(await d.isVisible());
    await d.locator("form").evaluate((f) => f.requestSubmit());
    assert.equal(writes, 1);
    h.release();
    hold = null;
    await d.waitFor({ state: "hidden" });
    await s.login(owner, p, tenant);
    assert.equal(
      (
        await call(p, "GET", base + "/organizations/" + org + "/members")
      ).body.items.filter((v) => v.id === target).length,
      1,
    );
    assert.equal(
      (await call(p, "GET", base + "/members/" + target + "/roles")).body.items
        .length,
      0,
    );
    phase = "close-initial-read-and-reopen-late-page";
    await s.nav(p, "组织");
    h = arm((url, m) => m === "GET" && url === base + "/members?limit=25");
    await p.getByRole("button", { name: "关联成员", exact: true }).click();
    await h.arrived;
    await close(p);
    h.release();
    hold = null;
    assert.equal(await dialog(p).count(), 0);
    d = await open(p);
    h = arm(
      (url, m) => m === "GET" && url.includes("/organizations?limit=25&after="),
    );
    await d.getByRole("button", { name: "加载更多组织", exact: true }).click();
    await h.arrived;
    await close(p);
    d = await open(p);
    h.release();
    hold = null;
    await d.getByText("已加载 25 个组织", { exact: true }).waitFor();
    await close(p);
    phase = "real-structure-version-change-clears-selection";
    d = await open(p);
    await choose(d, members.members[0].id, organizations.orgs[0].id);
    let version = (await call(p, "GET", base + "/organizations")).etag;
    const renamed = organizations.orgs[5];
    await call(
      p,
      "POST",
      base + "/organizations/" + renamed.id + "/rename",
      { name: renamed.name + "_changed" },
      version,
    );
    renamed.name += "_changed";
    await d.getByRole("button", { name: "加载更多组织", exact: true }).click();
    await d.getByText(/本次选择已清除/).waitFor();
    assert(
      await d
        .getByRole("button", { name: "保存并继续", exact: true })
        .isDisabled(),
    );
    assert.equal(writes, 1);
    await close(p);
    phase = "invalid-page-and-dependency-failure";
    d = await open(p);
    fault = {
      kind: "invalid",
      match: (url, m) =>
        m === "GET" && url.includes("/organizations?limit=25&after="),
    };
    await d.getByRole("button", { name: "加载更多组织", exact: true }).click();
    await d.getByText(/本次选择已清除/).waitFor();
    assert(!(await d.innerText()).includes("private-directory-marker"));
    await close(p);
    d = await open(p);
    fault = {
      kind: "unavailable",
      match: (url, m) =>
        m === "GET" && url.includes("/members?limit=25&after="),
    };
    await d.getByRole("button", { name: "加载更多成员", exact: true }).click();
    await d.getByText(/本次选择已清除/).waitFor();
    assert(
      await d
        .getByRole("button", { name: "保存并继续", exact: true })
        .isDisabled(),
    );
    await close(p);
    phase = "workspace-switch-drops-held-primary-directory";
    d = await open(p);
    h = arm(
      (url, m) => m === "GET" && url.includes("/members?limit=25&after="),
    );
    await d.getByRole("button", { name: "加载更多成员", exact: true }).click();
    await h.arrived;
    await s.login(owner, p, secondary);
    h.release();
    hold = null;
    await s.nav(p, "组织");
    await p.getByRole("button", { name: "关联成员", exact: true }).click();
    d = dialog(p);
    await d.getByText("已加载 1 个组织", { exact: true }).waitFor();
    assert.equal(
      await d.getByLabel("选择成员", { exact: true }).locator("option").count(),
      2,
    );
    assert(!(await d.innerText()).includes(organizations.orgs[0].name));
    await close(p);
    phase = "authority-change-requires-reauthorization";
    await s.login(owner, p, tenant);
    await s.nav(p, "组织");
    d = await open(p);
    version = (await call(p, "GET", base + "/permissions")).etag;
    await call(
      p,
      "POST",
      base + "/roles",
      { code: "assignment_stale", policies: [] },
      version,
      201,
    );
    await d.getByRole("button", { name: "加载更多成员", exact: true }).click();
    await p
      .getByRole("heading", { name: "请重新确认访问权限", exact: true })
      .waitFor();
    assert.equal(await dialog(p).count(), 0);
    phase = "committed-lost-reply-never-retries";
    await s.login(owner, p, tenant);
    await s.nav(p, "组织");
    d = await open(p);
    const lostTarget = members.members[0].id,
      lostOrg = organizations.orgs[0].id;
    await choose(d, lostTarget, lostOrg);
    fault = {
      kind: "lost",
      match: (url, m) =>
        m === "PUT" &&
        url === base + "/organizations/" + lostOrg + "/members/" + lostTarget,
    };
    await d.getByRole("button", { name: "保存并继续", exact: true }).click();
    await d.getByText(/本次选择已清除/).waitFor();
    assert.equal(writes, 2);
    assert(
      await d
        .getByRole("button", { name: "保存并继续", exact: true })
        .isDisabled(),
    );
    await close(p);
    await s.login(owner, p, tenant);
    assert.equal(
      (
        await call(p, "GET", base + "/organizations/" + lostOrg + "/members")
      ).body.items.filter((v) => v.id === lostTarget).length,
      1,
    );
    phase = "real-delegated-organization-operator";
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
    const accepted = await call(
      guest.page,
      "POST",
      "/v1/invitations/accept",
      { token: invitation.token },
      undefined,
      201,
    );
    await s.login(owner, p, tenant);
    version = (await call(p, "GET", base + "/permissions")).etag;
    const role = (
      await call(
        p,
        "POST",
        base + "/roles",
        {
          code: "directory_org_operator",
          policies: [
            "organizations.read",
            "organizations.write",
            "members.read",
          ].map((permission) => ({ permission, effect: "allow" })),
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
      base + "/members/" + accepted.body.membership_id + "/roles/" + role,
      {},
      version,
      204,
    );
    await s.login(guest, guest.page, tenant);
    await s.nav(guest.page, "组织");
    d = await open(guest.page);
    await choose(d, members.members[1].id, organizations.orgs[1].id);
    await d.getByRole("button", { name: "保存并继续", exact: true }).click();
    await d.waitFor({ state: "hidden" });
    phase = "delegated-result-and-audit-actor";
    assert.equal(writes, 3);
    await s.login(owner, p, tenant);
    assert.equal(
      (
        await call(
          p,
          "GET",
          base + "/members/" + members.members[1].id + "/roles",
        )
      ).body.items.length,
      0,
    );
    const events = (
      await call(p, "GET", base + "/audit-events?limit=100")
    ).body.items.filter((v) => v.action === "org.member_assigned");
    assert.equal(events.length, 3);
    assert(
      events.some((v) => v.actor_membership_id === accepted.body.membership_id),
    );
    assert.equal(errors.length, 0);
    assert.equal(s.pageErrors.length, 0);
    assert.equal(s.leaks.length, 0);
    const absence = require("../support/private-material.cjs")(
      s.root,
      [
        ...secrets,
        ...s.fixtures.flatMap((f) => [f.password, f.totpSecret, ...f.codes]),
      ],
      started,
    );
    console.log(
      "Organization assignment passed: " +
        checks +
        " real BFF contract responses; 106 members/56 organizations, last pages, once-only confirmed writes, no role grant, ordinary delegated operator, structure/authority change, cancel/reopen/workspace isolation, malformed/dependency failures and committed lost reply; private exclusion in " +
        absence.files +
        " files/runtime logs",
    );
  } finally {
    if (hold) hold.release();
    clearInterval(progress);
    if (organizations) {
      population("orgs", "cleanup", {
        population: organizations,
        members: members.members.map((m) => m.id),
      });
      organizations = null;
    }
    if (members) {
      population("members", "cleanup", members);
      members = null;
    }
    await s.close();
  }
})().catch((e) => {
  console.error(
    "Organization assignment failed: phase=" +
      phase +
      ", error=" +
      e.name +
      " (private details withheld)",
  );
  process.exitCode = 1;
});
