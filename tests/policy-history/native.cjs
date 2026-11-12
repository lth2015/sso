// Real User PKCE/Bearer -> primary-owned immutable policy projection.
const n = require("../support/native-fixtures.cjs"),
  assert = require("node:assert/strict"),
  { execFileSync } = require("node:child_process");
let phase = "startup",
  checks = 0;
const secrets = [],
  started = new Date().toISOString(),
  progress = setInterval(
    () => console.log("Policy history native phase: " + phase),
    20000,
  );
async function auth(f, tenant, scopes, force = false) {
  const t = await n.authorize(f, tenant, scopes, force);
  secrets.push(
    ...[t.access_token, t.id_token, t.refresh_token].filter(
      (v) => typeof v === "string",
    ),
  );
  return t.access_token;
}
async function call(method, url, token, body, status = 200, etag) {
  const r = await n.api(
    method,
    url,
    token,
    body,
    etag ? { "If-Match": etag } : {},
    status,
  );
  checks++;
  if (typeof r.body?.token === "string") secrets.push(r.body.token);
  if (typeof r.body?.secret_key === "string") secrets.push(r.body.secret_key);
  return r;
}
function absent(v) {
  const text = JSON.stringify(v);
  for (const secret of secrets) assert(!text.includes(secret));
  assert(!/eyJ[\w-]+\.[\w-]+\.[\w-]+/.test(text));
}
(async () => {
  try {
    await n.start();
    const owner = await n.register(),
      guest = await n.register(),
      other = await n.register();
    let token = await auth(owner),
      guestToken = await auth(guest),
      otherToken = await auth(other);
    let me = (await call("GET", "/v1/me", token)).body;
    const tenant = me.tenant_id,
      base = "/v1/tenants/" + tenant,
      roles = base + "/roles";
    async function version(t = token) {
      return (await call("GET", base + "/permissions", t)).headers.etag;
    }
    async function create(code, policies) {
      const result = await call(
        "POST",
        roles,
        token,
        { code, policies },
        201,
        await version(),
      );
      token = await auth(owner, tenant);
      return result.body.id;
    }
    const originalOwner = (await call("GET", roles, token)).body.items.find(
      (r) => r.built_in,
    ).id;
    phase = "legacy-empty-and-captured-create";
    const legacy = await call(
      "GET",
      roles + "/" + originalOwner + "/policy-history",
      token,
    );
    assert.deepEqual(legacy.body.items, []);
    assert.equal(legacy.headers.etag, undefined);
    const initial = [
        { permission: "members.read", effect: "allow" },
        { permission: "audit.read", effect: "allow" },
      ],
      role = await create("native_policy_history", initial),
      url = roles + "/" + role + "/policy-history";
    let history = await call("GET", url, token);
    assert.equal(history.body.format, "sso.role.policy-history.v1");
    assert.equal(history.body.tenant_id, tenant);
    assert.equal(history.body.role_id, role);
    assert.equal(history.body.items.length, 1);
    assert.deepEqual(history.body.items[0].before, []);
    assert.equal(history.body.items[0].role_code, "native_policy_history");
    absent(history.body);
    phase = "canonical-noop-and-atomic-update";
    const oldVersion = await version(),
      oldEvents = (await call("GET", base + "/audit-events?limit=100", token))
        .body.items;
    await call(
      "PUT",
      roles + "/" + role,
      token,
      { policies: [initial[1], initial[0]] },
      204,
      oldVersion,
    );
    assert.equal(await version(), oldVersion);
    assert.equal((await call("GET", url, token)).body.items.length, 1);
    assert.deepEqual(
      (await call("GET", base + "/audit-events?limit=100", token)).body.items,
      oldEvents,
    );
    const replacement = [{ permission: "members.read", effect: "deny" }];
    await call(
      "PUT",
      roles + "/" + role,
      token,
      { policies: replacement },
      204,
      oldVersion,
    );
    await call("GET", url, token, undefined, 401);
    token = await auth(owner, tenant);
    history = await call("GET", url, token);
    assert.equal(history.body.items.length, 2);
    const changed = history.body.items.find((v) => v.action === "role.updated");
    assert.deepEqual(changed.after, replacement);
    assert.equal(changed.before.length, 2);
    await call(
      "PUT",
      roles + "/" + role,
      token,
      { policies: [] },
      412,
      oldVersion,
    );
    await call("PUT", roles + "/" + role, token, { policies: [] }, 428);
    assert.equal((await call("GET", url, token)).body.items.length, 2);
    phase = "real-concurrent-authorization-and-single-snapshot";
    const concurrentVersion = await version();
    const result = await Promise.all(
      ["allow", "deny"].map((effect) =>
        call(
          "PUT",
          roles + "/" + role,
          token,
          { policies: [{ permission: "audit.read", effect }] },
          [204, 401, 403, 412],
          concurrentVersion,
        ),
      ),
    );
    assert.equal(result.filter((r) => r.status === 204).length, 1);
    token = await auth(owner, tenant);
    assert.equal((await call("GET", url, token)).body.items.length, 3);
    phase = "bounded-pages-delete-and-code-reuse";
    let first = await call("GET", url + "?limit=1", token);
    assert(first.body.next_cursor);
    const second = await call(
      "GET",
      url + "?limit=1&after=" + first.body.next_cursor,
      token,
    );
    assert(second.body.items[0].id > first.body.items[0].id);
    await call("DELETE", roles + "/" + role, token, {}, 204, await version());
    token = await auth(owner, tenant);
    history = await call("GET", url, token);
    assert.equal(history.body.items.length, 4);
    assert(
      history.body.items.some(
        (v) => v.action === "role.deleted" && v.after.length === 0,
      ),
    );
    const reused = await create("native_policy_history", []),
      reusedURL = roles + "/" + reused + "/policy-history";
    assert.notEqual(reused, role);
    assert.equal((await call("GET", reusedURL, token)).body.items.length, 1);
    assert.equal((await call("GET", url, token)).body.items.length, 4);
    phase = "actual-role-tenant-cursor-and-input-boundaries";
    const foreignMe = (await call("GET", "/v1/me", otherToken)).body,
      foreignBase = "/v1/tenants/" + foreignMe.tenant_id;
    const foreignRole = (
      await call(
        "POST",
        foreignBase + "/roles",
        otherToken,
        { code: "foreign_policy", policies: [] },
        201,
        (await call("GET", foreignBase + "/permissions", otherToken)).headers
          .etag,
      )
    ).body.id;
    otherToken = await auth(other, foreignMe.tenant_id);
    const foreignHistory = (
      await call(
        "GET",
        foreignBase + "/roles/" + foreignRole + "/policy-history",
        otherToken,
      )
    ).body;
    for (const cursor of [
      "0".repeat(32),
      foreignHistory.items[0].id,
      (await call("GET", reusedURL, token)).body.items[0].id,
    ])
      await call("GET", url + "?after=" + cursor, token, undefined, 404);
    await call(
      "GET",
      roles + "/" + foreignRole + "/policy-history",
      token,
      undefined,
      404,
    );
    await call(
      "GET",
      foreignBase + "/roles/" + foreignRole + "/policy-history",
      token,
      undefined,
      404,
    );
    await call(
      "GET",
      roles + "/" + "f".repeat(32) + "/policy-history",
      token,
      undefined,
      404,
    );
    for (const query of [
      "?limit=0",
      "?limit=101",
      "?limit=",
      "?after=",
      "?after=a",
      "?after=" + first.body.next_cursor + "&after=" + first.body.next_cursor,
      "?kind=management",
    ])
      await call("GET", url + query, token, undefined, 400);
    await call("GET", url, token, {}, 400);
    phase = "audit-delegation-keeps-rules-private-and-owner-recovery";
    const invite = (
      await call(
        "POST",
        base + "/invitations",
        token,
        { email: guest.email },
        201,
      )
    ).body;
    const accepted = (
      await call(
        "POST",
        "/v1/invitations/accept",
        guestToken,
        { token: invite.token },
        201,
      )
    ).body;
    token = await auth(owner, tenant);
    guestToken = await auth(guest, tenant);
    await call("GET", url, guestToken, undefined, 403);
    const auditRole = await create("policy_history_auditor", [
      { permission: "audit.read", effect: "allow" },
    ]);
    await call(
      "PUT",
      base + "/members/" + accepted.membership_id + "/roles/" + auditRole,
      token,
      {},
      204,
      await version(),
    );
    token = await auth(owner, tenant);
    guestToken = await auth(guest, tenant);
    await call("GET", url, guestToken, undefined, 403);
    const batch = await call(
      "GET",
      base + "/audit-exports?kind=management&limit=100",
      guestToken,
    );
    assert(batch.body.items.some((v) => v.action === "role.updated"));
    assert(
      batch.body.items.every(
        (v) => !Object.hasOwn(v, "before") && !Object.hasOwn(v, "after"),
      ),
    );
    const deny = await create("owner_history_privacy", [
      { permission: "members.read", effect: "deny" },
      { permission: "audit.read", effect: "deny" },
    ]);
    await call(
      "PUT",
      base + "/members/" + me.membership_id + "/roles/" + deny,
      token,
      {},
      204,
      await version(),
    );
    token = await auth(owner, tenant);
    await call("GET", base + "/members", token, undefined, 403);
    await call("GET", base + "/audit-events", token, undefined, 403);
    history = await call("GET", url, token);
    assert.equal(history.body.items.length, 4);
    assert(history.body.items.every((v) => !Object.hasOwn(v, "email")));
    absent(history.body);
    phase = "read-write-scope-and-machine-realm";
    const readOnly = await auth(owner, tenant, "openid platform:read"),
      writeOnly = await auth(owner, tenant, "openid platform:write");
    await call("GET", url, readOnly);
    await call("GET", url, writeOnly, undefined, 403);
    const app = (
        await call(
          "POST",
          base + "/applications",
          token,
          { name: "Policy history realm fixture" },
          201,
        )
      ).body.id,
      sp = (
        await call(
          "POST",
          base + "/applications/" + app + "/service-principals",
          token,
          { name: "History machine" },
          201,
        )
      ).body;
    const key = (
      await call(
        "POST",
        base +
          "/applications/" +
          app +
          "/service-principals/" +
          sp.id +
          "/credentials",
        token,
        { name: "History realm key", expires_in_days: 1 },
        201,
        `"sp-${tenant}-${sp.id}-${sp.version}"`,
      )
    ).body;
    const form = {
      grant_type: "client_credentials",
      scope: "machine:invoke",
      audience: "urn:sso:application:" + app,
    };
    const minted = await n.request(
      4464,
      "auth.localhost:4464",
      "POST",
      "/oauth2/token",
      new URLSearchParams(form).toString(),
      {
        "Content-Type": "application/x-www-form-urlencoded",
        Authorization:
          "Basic " +
          Buffer.from(
            key.credential.access_key + ":" + key.secret_key,
          ).toString("base64"),
      },
    );
    assert.equal(minted.status, 200);
    secrets.push(minted.body.access_token);
    await call("GET", url, minted.body.access_token, undefined, 401);
    phase = "aged-mfa-read-and-current-owner-revocation";
    me = (await call("GET", "/v1/me", token)).body;
    execFileSync(
      "python3",
      ["tests/api/fixtures.py", "age-session", me.user_id, me.session_id],
      { cwd: n.root, stdio: "pipe" },
    );
    await call("GET", url, token);
    const agedWrite = await call(
      "PUT",
      roles + "/" + reused,
      token,
      { policies: [{ permission: "audit.read", effect: "allow" }] },
      403,
      await version(),
    );
    assert.equal(agedWrite.body.code, "recent_mfa_required");
    token = await auth(owner, tenant, undefined, true);
    await call(
      "PUT",
      base + "/members/" + accepted.membership_id + "/roles/" + originalOwner,
      token,
      {},
      204,
      await version(),
    );
    guestToken = await auth(guest, tenant);
    const guestVersion = (await call("GET", base + "/permissions", guestToken))
      .headers.etag;
    await call(
      "DELETE",
      base + "/members/" + me.membership_id + "/roles/" + originalOwner,
      guestToken,
      {},
      204,
      guestVersion,
    );
    await call("GET", url, token, undefined, 401);
    token = await auth(owner, tenant);
    await call("GET", url, token, undefined, 403);
    guestToken = await auth(guest, tenant);
    await call("GET", url, guestToken);
    const privacy = require("../support/private-material.cjs")(
      n.root,
      secrets,
      started,
    );
    console.log(
      "Policy history native passed: " +
        checks +
        " real contract responses; captured canonical policies, no-op/refusal/concurrency atomicity, deleted/code reuse, owner-only/current revocation, cursor/tenant/scope/MFA/machine realm and frozen audit projection; private exclusion in " +
        privacy.files +
        " files/runtime logs",
    );
  } finally {
    clearInterval(progress);
    await n.close();
  }
})().catch((e) => {
  console.error(
    "Policy history native failed: phase=" +
      phase +
      ", error=" +
      e.name +
      " (private details withheld)",
  );
  process.exitCode = 1;
});
