// Real Hydra GET observations; drift is injected only into the owned pending public fixture.
const n = require("../support/native-fixtures.cjs"),
  assert = require("node:assert/strict"),
  crypto = require("node:crypto"),
  { execFileSync } = require("node:child_process");
let phase = "startup",
  checks = 0;
const secrets = [],
  started = new Date().toISOString();
const progress = setInterval(
  () => console.log("Protocol observation native phase: " + phase),
  20000,
);
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
  return r;
}
async function auth(f, tenant, scope) {
  const t = await n.authorize(f, tenant, scope);
  secrets.push(
    ...[t.access_token, t.id_token, t.refresh_token].filter(
      (v) => typeof v === "string",
    ),
  );
  return t.access_token;
}
function absent(body) {
  const text = JSON.stringify(body);
  for (const v of secrets) assert(!text.includes(v));
  assert(!text.includes("client_secret"));
  assert(!text.includes("registration_access_token"));
}
function fixture(script, args) {
  execFileSync("python3", [script, ...args], { cwd: n.root, stdio: "pipe" });
}
function contract(kind, path, method, r, body) {
  execFileSync("python3", ["tests/api/validate_contract.py"], {
    cwd: n.root,
    input: JSON.stringify({
      contract: kind,
      path,
      method,
      status: r.status,
      body: r.body,
      request_body: body,
      content_type: r.headers["content-type"],
      request_id: r.headers["x-request-id"],
    }),
    stdio: ["pipe", "pipe", "pipe"],
  });
  checks++;
}
async function exchange(key, app) {
  const form = {
    grant_type: "client_credentials",
    scope: "machine:invoke",
    audience: "urn:sso:application:" + app,
  };
  const r = await n.request(
    4464,
    "auth.localhost:4464",
    "POST",
    "/oauth2/token",
    new URLSearchParams(form).toString(),
    {
      "Content-Type": "application/x-www-form-urlencoded",
      Authorization:
        "Basic " +
        Buffer.from(key.credential.access_key + ":" + key.secret_key).toString(
          "base64",
        ),
    },
  );
  assert.equal(r.status, 200);
  contract("machine", "/oauth2/token", "POST", r, form);
  secrets.push(r.body.access_token);
  return r.body.access_token;
}
async function identity(token, app, status) {
  const r = await n.request(
    8083,
    "api.localhost:8083",
    "GET",
    "/v1/machine/applications/" + app + "/identity",
    undefined,
    { Authorization: "Bearer " + token },
  );
  if (r.status !== status)
    console.error(
      "Machine identity status expected=" + status + " received=" + r.status,
    );
  assert.equal(r.status, status);
  contract(
    "machine",
    "/v1/machine/applications/{application}/identity",
    "GET",
    r,
  );
  return r;
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
    const app = (
        await call(
          "POST",
          base + "/applications",
          token,
          { name: "协议核对示范" },
          201,
        )
      ).body.id,
      apps = base + "/applications/" + app;
    const client = (
      await call(
        "POST",
        apps + "/clients",
        token,
        {
          name: "受控公开客户端",
          redirect_uri: "http://127.0.0.1:9876/callback",
        },
        201,
      )
    ).body;
    const cp = apps + "/clients/" + client.client_id,
      cs = cp + "/protocol-status";
    phase = "public-absent-drift-matching-without-activation";
    const before = (await call("GET", base + "/audit-exports?limit=100", token))
      .body.items;
    let observed = await call("GET", cs, token);
    assert.equal(observed.headers.etag, undefined);
    assert.equal(
      observed.body.resource_etag,
      `"client-${tenant}-${client.client_id}-1"`,
    );
    assert.equal(observed.body.profile_state, "absent");
    assert.equal(observed.body.local_state, "pending");
    assert.equal(observed.body.service_principal_id, null);
    absent(observed.body);
    await call("GET", cs, undefined, undefined, 401);
    await call("GET", cs + "?repair=true", token, undefined, 400);
    await call("GET", cs, token, {}, 400);
    fixture("tests/applications/managed_fixture.py", [
      "create-drift",
      client.client_id,
    ]);
    observed = await call("GET", cs, token);
    assert.equal(observed.body.profile_state, "conflict");
    assert.deepEqual(observed.body.differences, ["scope"]);
    assert(!JSON.stringify(observed.body).includes("platform:write"));
    await call(
      "POST",
      cp + "/reconcile",
      token,
      {},
      409,
      observed.body.resource_etag,
    );
    fixture("tests/applications/managed_fixture.py", [
      "restore",
      client.client_id,
    ]);
    observed = await call("GET", cs, token);
    assert.equal(observed.body.profile_state, "matching");
    assert.equal(observed.body.local_state, "pending");
    assert.equal(observed.body.revision, 1);
    assert.equal((await call("GET", cp, token)).body.active, false);
    assert.deepEqual(
      (await call("GET", base + "/audit-exports?limit=100", token)).body.items,
      before,
    );
    let current = await call(
      "POST",
      cp + "/reconcile",
      token,
      {},
      200,
      observed.body.resource_etag,
    );
    observed = await call("GET", cs, token);
    assert.equal(observed.body.local_state, "active");
    assert.equal(observed.body.revision, 2);
    await call(
      "POST",
      cp + "/revoke",
      token,
      {},
      200,
      observed.body.resource_etag,
    );
    observed = await call("GET", cs, token);
    assert.equal(observed.body.local_state, "revoked");
    assert.equal(observed.body.profile_state, "matching");
    await call(
      "POST",
      cp + "/reconcile",
      token,
      {},
      200,
      observed.body.resource_etag,
    );
    observed = await call("GET", cs, token);
    assert.equal(observed.body.profile_state, "absent");
    assert.equal(observed.body.local_state, "revoked");
    phase =
      "machine-real-secret-token-and-observations-never-restore-eligibility";
    const sp = (
        await call(
          "POST",
          apps + "/service-principals",
          token,
          { name: "只读核对任务" },
          201,
        )
      ).body,
      spPath = apps + "/service-principals/" + sp.id;
    let keyReply = await call(
        "POST",
        spPath + "/credentials",
        token,
        { name: "待核对机器凭证", expires_in_days: 30 },
        201,
        `"sp-${tenant}-${sp.id}-${sp.version}"`,
      ),
      key = keyReply.body;
    secrets.push(key.secret_key);
    const kp = spPath + "/credentials/" + key.credential.id,
      ks = kp + "/protocol-status",
      machineToken = await exchange(key, app);
    await identity(machineToken, app, 200);
    observed = await call("GET", ks, token);
    assert.equal(observed.body.profile_state, "matching");
    assert.equal(observed.body.service_principal_id, sp.id);
    assert(!JSON.stringify(observed.body).includes(key.credential.access_key));
    absent(observed.body);
    await call("GET", ks, machineToken, undefined, 401);
    await call("GET", cs, machineToken, undefined, 401);
    const audits = (await call("GET", base + "/audit-exports?limit=100", token))
      .body.items;
    const machineAudits = (
      await call("GET", base + "/audit-exports?kind=machine&limit=100", token)
    ).body.items;
    await call("GET", ks, token);
    await call("GET", cs, token);
    assert.deepEqual(
      (await call("GET", base + "/audit-exports?limit=100", token)).body.items,
      audits,
    );
    assert.deepEqual(
      (await call("GET", base + "/audit-exports?kind=machine&limit=100", token))
        .body.items,
      machineAudits,
    );
    phase = "machine-pending-observation-and-invalidated-jwt";
    fixture("tests/machine/fixture.py", [
      "pending",
      app,
      sp.id,
      key.credential.id,
    ]);
    observed = await call("GET", ks, token);
    assert.equal(observed.body.local_state, "pending");
    assert.equal(observed.body.profile_state, "matching");
    await identity(machineToken, app, 401);
    await call(
      "POST",
      kp + "/revoke",
      token,
      {},
      200,
      observed.body.resource_etag,
    );
    observed = await call("GET", ks, token);
    assert.equal(observed.body.local_state, "revoked");
    assert.equal(observed.body.profile_state, "matching");
    await call(
      "POST",
      kp + "/cleanup",
      token,
      {},
      200,
      observed.body.resource_etag,
    );
    observed = await call("GET", ks, token);
    assert.equal(observed.body.profile_state, "absent");
    await identity(machineToken, app, 401);
    current = await call("GET", spPath, token);
    key = (
      await call(
        "POST",
        spPath + "/credentials",
        token,
        { name: "到期核对凭证", expires_in_days: 30 },
        201,
        current.headers.etag,
      )
    ).body;
    secrets.push(key.secret_key);
    phase = "machine-expired-observation-and-invalidated-jwt";
    const expiredPath = spPath + "/credentials/" + key.credential.id,
      expiredStatus = expiredPath + "/protocol-status",
      expiringToken = await exchange(key, app);
    fixture("tests/machine/fixture.py", [
      "expire",
      app,
      sp.id,
      key.credential.id,
    ]);
    observed = await call("GET", expiredStatus, token);
    assert.equal(observed.body.local_state, "expired");
    assert.equal(observed.body.profile_state, "matching");
    await identity(expiringToken, app, 401);
    phase = "current-owner-tenant-scope-and-recent-mfa-read-boundaries";
    const invite = (
      await call(
        "POST",
        base + "/invitations",
        token,
        { email: guest.email },
        201,
      )
    ).body;
    secrets.push(invite.token);
    await call(
      "POST",
      "/v1/invitations/accept",
      guestToken,
      { token: invite.token },
      201,
    );
    token = await auth(owner, tenant);
    guestToken = await auth(guest, tenant);
    await call("GET", cs, guestToken, undefined, 403);
    await call("GET", expiredStatus, guestToken, undefined, 403);
    const other = (
        await call(
          "POST",
          "/v1/tenants",
          token,
          {
            slug: "protocol-other-" + crypto.randomBytes(4).toString("hex"),
            name: "Other protocol fixture",
          },
          201,
        )
      ).body.id,
      otherToken = await auth(owner, other);
    await call("GET", cs, otherToken, undefined, 404);
    await call("GET", cs.replace(tenant, other), otherToken, undefined, 404);
    current = await call("GET", apps, token);
    await call("POST", apps + "/disable", token, {}, 200, current.headers.etag);
    await call("GET", cs, token);
    await call("GET", expiredStatus, token);
    const readOnly = await auth(owner, tenant, "openid platform:read"),
      writeOnly = await auth(owner, tenant, "openid platform:write");
    await call("GET", cs, readOnly);
    await call("GET", expiredStatus, readOnly);
    await call("GET", cs, writeOnly, undefined, 403);
    me = (await call("GET", "/v1/me", token)).body;
    fixture("tests/api/fixtures.py", [
      "age-session",
      me.user_id,
      me.session_id,
    ]);
    absent((await call("GET", cs, token)).body);
    absent((await call("GET", expiredStatus, token)).body);
    const privacy = require("../support/private-material.cjs")(
      n.root,
      secrets,
      started,
    );
    console.log(
      "Protocol observation native passed: " +
        checks +
        " real contract responses; owned public/machine missing/matching/drift states, pending/revoked/expired isolation, actual M2M/old-JWT refusal, no audit mutation, Owner/User realm/tenant/scope/MFA bounds and private exclusion in " +
        privacy.files +
        " files/runtime logs",
    );
  } finally {
    clearInterval(progress);
    await n.close();
  }
})().catch((e) => {
  console.error(
    "Protocol observation native failed: phase=" +
      phase +
      ", error=" +
      e.name +
      " (private details withheld)",
  );
  console.error(JSON.stringify(n.diagnostics()));
  process.exitCode = 1;
});
