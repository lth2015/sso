// Real User PKCE -> independent Go policy-history SDK/CLI. Tokens only on stdin.
const n = require("../support/native-fixtures.cjs"),
  assert = require("node:assert/strict"),
  { spawnSync, execFileSync } = require("node:child_process"),
  crypto = require("node:crypto");
let phase = "startup",
  sdkChecks = 0,
  contractChecks = 0;
const secrets = [],
  childOutputs = [],
  started = new Date().toISOString();
const progress = setInterval(
  () => console.log("Policy history client phase: " + phase),
  20000,
);
async function auth(f, tenant, scope, force = false) {
  const t = await n.authorize(f, tenant, scope, force);
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
  contractChecks++;
  if (typeof r.body?.token === "string") secrets.push(r.body.token);
  if (typeof r.body?.secret_key === "string") secrets.push(r.body.secret_key);
  return r;
}
function cli(token, tenant, kind = "management", page = {}, failure) {
  const result = spawnSync(n.root + "/.local/audit-batch-example", [], {
    cwd: n.root,
    input: JSON.stringify({
      access_token: token,
      tenant_id: tenant,
      kind,
      ...page,
    }),
    stdio: ["pipe", "pipe", "pipe"],
    timeout: 8000,
  });
  assert(!result.error);
  childOutputs.push(result.stdout, result.stderr);
  sdkChecks++;
  if (failure) {
    assert.equal(result.status, 1);
    assert.equal(result.stdout.length, 0);
    assert.equal(
      result.stderr.toString(),
      "audit batch failed: " + failure + "\n",
    );
    return;
  }
  assert.equal(result.status, 0);
  assert.equal(result.stderr.length, 0);
  const batch = JSON.parse(result.stdout);
  assert.equal(batch.tenant_id, tenant);
  if (kind === "policy_history") {
    assert.equal(batch.format, "sso.role.policy-history.v1");
    assert.equal(batch.role_id, page.role_id);
  } else assert.equal(batch.kind, kind);
  execFileSync("python3", ["tests/api/validate_contract.py"], {
    cwd: n.root,
    input: JSON.stringify({
      path:
        kind === "policy_history"
          ? "/v1/tenants/{tenant}/roles/{role}/policy-history"
          : "/v1/tenants/{tenant}/audit-exports",
      method: "GET",
      status: 200,
      body: batch,
      content_type: "application/json",
    }),
    stdio: ["pipe", "pipe", "pipe"],
  });
  return batch;
}
(async () => {
  try {
    await n.start();
    const owner = await n.register(),
      guest = await n.register();
    let token = await auth(owner),
      guestToken = await auth(guest);
    const me = (await call("GET", "/v1/me", token)).body,
      tenant = me.tenant_id,
      base = "/v1/tenants/" + tenant;
    const originalOwner = (
      await call("GET", base + "/roles", token)
    ).body.items.find((v) => v.built_in).id;
    async function version(t = token) {
      return (await call("GET", base + "/permissions", t)).headers.etag;
    }
    async function create(code, policies) {
      const r = await call(
        "POST",
        base + "/roles",
        token,
        { code, policies },
        201,
        await version(),
      );
      token = await auth(owner, tenant);
      return r.body.id;
    }
    const history = (t, role, page = {}, failure) =>
      cli(t, tenant, "policy_history", { role_id: role, ...page }, failure);
    phase = "create-replace-delete-and-real-keyset-pages";
    assert.deepEqual(history(token, originalOwner).items, []);
    const role = await create("sdk_policy_history", [
      { permission: "members.read", effect: "allow" },
    ]);
    assert.equal(history(token, role).items[0].action, "role.created");
    const oldToken = token;
    await call(
      "PUT",
      base + "/roles/" + role,
      token,
      { policies: [{ permission: "members.read", effect: "deny" }] },
      204,
      await version(),
    );
    history(oldToken, role, {}, "unauthenticated");
    token = await auth(owner, tenant);
    const first = history(token, role, { limit: 1 });
    assert(first.next_cursor);
    const next = history(token, role, { limit: 1, after: first.next_cursor });
    assert(next.items[0].id > first.items[0].id);
    assert.deepEqual(next.items[0].before, [
      { permission: "members.read", effect: "allow" },
    ]);
    assert.deepEqual(next.items[0].after, [
      { permission: "members.read", effect: "deny" },
    ]);
    await call(
      "DELETE",
      base + "/roles/" + role,
      token,
      {},
      204,
      await version(),
    );
    token = await auth(owner, tenant);
    const deleted = history(token, role);
    assert.equal(deleted.items.length, 3);
    assert(
      deleted.items.some(
        (v) => v.action === "role.deleted" && v.after.length === 0,
      ),
    );
    const reused = await create("sdk_policy_history", []);
    assert.notEqual(reused, role);
    const fresh = history(token, reused);
    assert.equal(fresh.items.length, 1);
    assert.equal(history(token, role).items.length, 3);
    phase = "namespace-scope-and-stdin-boundaries";
    history(token, role, { after: fresh.items[0].id }, "not_found");
    history(token, "f".repeat(32), {}, "not_found");
    cli(
      token,
      "f".repeat(32),
      "policy_history",
      { role_id: role },
      "not_found",
    );
    history(token, role, { limit: 101 }, "invalid_input");
    history(token, "../roles", {}, "invalid_input");
    cli(token, tenant, "policy_history", {}, "invalid_input");
    cli(token, tenant, "management", { role_id: role }, "invalid_input");
    cli(token, tenant, "machine", { role_id: role }, "invalid_input");
    history(await auth(owner, tenant, "openid platform:read"), role);
    history(
      await auth(owner, tenant, "openid platform:write"),
      role,
      {},
      "forbidden",
    );
    phase = "audit-only-delegate-is-not-policy-owner";
    const invitation = (
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
        { token: invitation.token },
        201,
      )
    ).body;
    token = await auth(owner, tenant);
    const auditRole = await create("sdk_history_audit_reader", [
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
    history(guestToken, role, {}, "forbidden");
    const delegated = cli(guestToken, tenant);
    assert(
      delegated.items.every(
        (v) => !Object.hasOwn(v, "before") && !Object.hasOwn(v, "after"),
      ),
    );
    phase = "real-machine-bearer-cannot-read-user-policies";
    const app = (
      await call(
        "POST",
        base + "/applications",
        token,
        { name: "Policy SDK realm" },
        201,
      )
    ).body.id;
    const sp = (
      await call(
        "POST",
        base + "/applications/" + app + "/service-principals",
        token,
        { name: "Policy SDK machine" },
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
        { name: "Policy SDK key", expires_in_days: 1 },
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
    execFileSync("python3", ["tests/api/validate_contract.py"], {
      cwd: n.root,
      input: JSON.stringify({
        contract: "machine",
        path: "/oauth2/token",
        method: "POST",
        status: 200,
        body: minted.body,
        request_body: form,
        content_type: minted.headers["content-type"],
      }),
      stdio: ["pipe", "pipe", "pipe"],
    });
    contractChecks++;
    history(minted.body.access_token, role, {}, "unauthenticated");
    phase = "aged-mfa-read-and-current-owner-loss";
    const current = (await call("GET", "/v1/me", token)).body;
    execFileSync(
      "python3",
      [
        "tests/api/fixtures.py",
        "age-session",
        current.user_id,
        current.session_id,
      ],
      { cwd: n.root, stdio: "pipe" },
    );
    history(token, role);
    // Reauthenticate this synthetic owner with MFA; no recovery codes used.
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
    await call(
      "DELETE",
      base + "/members/" + me.membership_id + "/roles/" + originalOwner,
      guestToken,
      {},
      204,
      await version(guestToken),
    );
    history(token, role, {}, "unauthenticated");
    token = await auth(owner, tenant);
    history(token, role, {}, "forbidden");
    guestToken = await auth(guest, tenant);
    assert.equal(history(guestToken, role).items.length, 3);
    for (const output of childOutputs) {
      for (const secret of secrets)
        assert(!output.includes(Buffer.from(secret)));
      assert(!/eyJ[\w-]+\.[\w-]+\.[\w-]+/.test(output.toString()));
    }
    const privacy = require("../support/private-material.cjs")(
      n.root,
      secrets,
      started,
    );
    console.log(
      "Policy history SDK/CLI passed: " +
        sdkChecks +
        " independent CLI checks and " +
        contractChecks +
        " real setup/API responses; owner-only/deleted/code reuse/pagination/scope/tenant/delegate/machine/aged-MFA/live owner loss, private exclusion in " +
        privacy.files +
        " files/runtime logs",
    );
  } finally {
    clearInterval(progress);
    await n.close();
  }
})().catch((e) => {
  console.error(
    "Policy history SDK/CLI failed: phase=" +
      phase +
      ", error=" +
      e.name +
      " (private details withheld)",
  );
  console.error(JSON.stringify(n.diagnostics()));
  process.exitCode = 1;
});
