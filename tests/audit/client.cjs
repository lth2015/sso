// Real User OAuth/PKCE -> independent Go SDK/CLI -> audit primary. Tokens only on stdin.
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
  () => console.log("User audit client phase: " + phase),
  20000,
);
async function auth(f, tenant, scope) {
  const t = await n.authorize(f, tenant, scope);
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
  assert.equal(batch.kind, kind);
  execFileSync("python3", ["tests/api/validate_contract.py"], {
    cwd: n.root,
    input: JSON.stringify({
      path: "/v1/tenants/{tenant}/audit-exports",
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
    let me = (await call("GET", "/v1/me", token)).body;
    const tenant = me.tenant_id,
      base = "/v1/tenants/" + tenant;
    phase = "real-bounded-management-pagination-and-no-read-side-effects";
    const invite = (
      await call(
        "POST",
        base + "/invitations",
        token,
        { email: guest.email },
        201,
      )
    ).body;
    const before = (await call("GET", base + "/audit-exports?limit=100", token))
      .body.items;
    const first = cli(token, tenant, "management", { limit: 1 });
    assert.equal(first.items.length, 1);
    assert(first.next_cursor);
    const next = cli(token, tenant, "management", {
      limit: 1,
      after: first.next_cursor,
    });
    assert(next.items.every((v) => v.id > first.next_cursor));
    const machineEmpty = cli(token, tenant, "machine");
    assert.deepEqual(machineEmpty.items, []);
    assert.deepEqual(
      (await call("GET", base + "/audit-exports?limit=100", token)).body.items,
      before,
    );
    cli(token, tenant, "management", { limit: 101 }, "invalid_input");
    cli(token, "f".repeat(32), "management", {}, "not_found");
    const readOnly = await auth(owner, tenant, "openid platform:read"),
      writeOnly = await auth(owner, tenant, "openid platform:write");
    cli(readOnly, tenant);
    cli(writeOnly, tenant, "management", {}, "forbidden");
    phase = "delegate-only-audit-and-current-deny-no-credential-privilege";
    await call(
      "POST",
      "/v1/invitations/accept",
      guestToken,
      { token: invite.token },
      201,
    );
    token = await auth(owner, tenant);
    guestToken = await auth(guest, tenant);
    cli(guestToken, tenant, "management", {}, "forbidden");
    const member = (
      await call("GET", base + "/members", token)
    ).body.items.find((v) => v.email === guest.email).id;
    async function policy(code, effect) {
      let current = await call("GET", base, token);
      const role = (
        await call(
          "POST",
          base + "/roles",
          token,
          { code, policies: [{ permission: "audit.read", effect }] },
          201,
          current.headers.etag,
        )
      ).body.id;
      token = await auth(owner, tenant);
      current = await call("GET", base, token);
      await call(
        "PUT",
        base + "/members/" + member + "/roles/" + role,
        token,
        {},
        204,
        current.headers.etag,
      );
      token = await auth(owner, tenant);
    }
    await policy("audit_sdk_reader", "allow");
    guestToken = await auth(guest, tenant);
    const delegated = cli(guestToken, tenant);
    assert(delegated.items.some((v) => v.role_id !== null));
    cli(guestToken, tenant, "machine");
    await call("GET", base + "/members", guestToken, undefined, 403);
    const oldGuestToken = guestToken;
    await policy("audit_sdk_blocked", "deny");
    cli(oldGuestToken, tenant, "management", {}, "unauthenticated");
    guestToken = await auth(guest, tenant);
    cli(guestToken, tenant, "management", {}, "forbidden");
    phase = "real-machine-audit-record-and-machine-bearer-cannot-manage";
    const app = (
      await call(
        "POST",
        base + "/applications",
        token,
        { name: "Audit SDK consumer" },
        201,
      )
    ).body.id;
    const sp = (
      await call(
        "POST",
        base + "/applications/" + app + "/service-principals",
        token,
        { name: "SDK fixture" },
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
        { name: "SDK protocol fixture", expires_in_days: 30 },
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
    const machine = cli(token, tenant, "machine");
    assert.equal(machine.items.length, 1);
    assert.equal(machine.items[0].service_principal_id, sp.id);
    assert(!Object.hasOwn(machine.items[0], "actor_membership_id"));
    cli(minted.body.access_token, tenant, "management", {}, "unauthenticated");
    phase = "aged-mfa-read-remains-valid-and-independent-scope";
    me = (await call("GET", "/v1/me", token)).body;
    execFileSync(
      "python3",
      ["tests/api/fixtures.py", "age-session", me.user_id, me.session_id],
      { cwd: n.root, stdio: "pipe" },
    );
    cli(token, tenant);
    cli(token, tenant, "machine");
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
      "User audit SDK/CLI passed: " +
        sdkChecks +
        " independent CLI checks and " +
        contractChecks +
        " real setup/API responses; primary-authorized management/machine batches, pagination, audit-only delegation/live deny, realm/scope/tenant/aged-MFA bounds, no mutation and private exclusion in " +
        privacy.files +
        " files/runtime logs",
    );
  } finally {
    clearInterval(progress);
    await n.close();
  }
})().catch((e) => {
  console.error(
    "User audit SDK/CLI failed: phase=" +
      phase +
      ", error=" +
      e.name +
      " (private details withheld)",
  );
  console.error(JSON.stringify(n.diagnostics()));
  process.exitCode = 1;
});
